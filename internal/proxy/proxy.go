// Package proxy implements the capturing HTTP forward proxy.
//
// Plain HTTP requests are forwarded and recorded in full (up to the body
// capture limit). CONNECT requests are either intercepted (TLS terminated
// with a certificate from the local CA, requests inside recorded like plain
// HTTP) or relayed byte-for-byte and recorded as tunnels.
package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/traffickit/traffickit/internal/traffic"
)

// Sink receives snapshots of exchanges as they progress. Record is called
// several times per exchange with the same ID; each call replaces the last.
type Sink interface {
	NextID() uint64
	Record(traffic.Exchange)
	// Message adds a WebSocket message to exchange id.
	Message(id uint64, m traffic.WSMessage)
}

type Options struct {
	Sink         Sink
	Logger       *slog.Logger
	MaxBodyBytes int64
	// Transport overrides the upstream transport, mainly for tests.
	Transport   http.RoundTripper
	DialTimeout time.Duration

	// CA issues certificates for intercepted hosts. Without one, every
	// CONNECT is relayed untouched.
	CA Issuer
	// Intercept reports whether CONNECTs to host should be decrypted.
	// Nil means intercept everything (when CA is set).
	Intercept func(host string) bool
}

type Proxy struct {
	sink      Sink
	log       *slog.Logger
	maxBody   int64
	transport http.RoundTripper
	dialer    *net.Dialer
	selfPort  atomic.Int64
	ca        Issuer
	intercept func(string) bool

	mu     sync.Mutex
	conns  map[io.Closer]struct{} // hijacked connections, closed on shutdown
	closed bool
}

func New(opts Options) *Proxy {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 15 * time.Second
	}
	dialer := &net.Dialer{Timeout: opts.DialTimeout, KeepAlive: 30 * time.Second}
	p := &Proxy{
		sink:      opts.Sink,
		log:       opts.Logger,
		maxBody:   opts.MaxBodyBytes,
		transport: opts.Transport,
		dialer:    dialer,
		conns:     make(map[io.Closer]struct{}),
		ca:        opts.CA,
		intercept: opts.Intercept,
	}
	if p.transport == nil {
		p.transport = &http.Transport{
			Proxy:                 nil, // never chain through the system proxy, which may be us
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: time.Second,
			// Pass Content-Encoding through untouched; we decode for display only.
			DisableCompression: true,
		}
	}
	return p
}

// SetSelfPort tells the proxy which port it's listening on so it can refuse
// requests that would loop back into it.
func (p *Proxy) SetSelfPort(port int) { p.selfPort.Store(int64(port)) }

// Close shuts down tunnels and upgraded connections, which http.Server's
// Shutdown doesn't track once they're hijacked.
func (p *Proxy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for c := range p.conns {
		c.Close()
	}
	clear(p.conns)
}

func (p *Proxy) track(cs ...io.Closer) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		for _, c := range cs {
			c.Close()
		}
		return false
	}
	for _, c := range cs {
		p.conns[c] = struct{}{}
	}
	return true
}

func (p *Proxy) untrack(cs ...io.Closer) {
	p.mu.Lock()
	for _, c := range cs {
		delete(p.conns, c)
	}
	p.mu.Unlock()
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodConnect:
		p.tunnel(w, r)
	case r.URL.IsAbs():
		p.forward(w, r)
	default:
		p.serveLocal(w, r)
	}
}

func (p *Proxy) targetsSelf(hostport, defaultPort string) bool {
	self := p.selfPort.Load()
	if self == 0 {
		return false
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = hostport, defaultPort
	}
	if port != strconv.FormatInt(self, 10) {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func (p *Proxy) refuseLoop(w http.ResponseWriter, x *traffic.Exchange) {
	x.State = traffic.StateFailed
	x.Error = &traffic.Failure{
		Code:    "loop",
		Message: "The request was addressed to TrafficKit's own proxy port.",
		Detail:  "refusing to forward " + x.Request.Host + " to itself",
		Hint:    "Request the real server address; the proxy setting on the client already routes it through TrafficKit.",
	}
	x.Timings.Total = 0
	p.sink.Record(x.Clone())
	writeFailure(w, http.StatusLoopDetected, x.Error)
}

func describeRequest(r *http.Request) traffic.Request {
	hs := append([]traffic.Header{{Name: "Host", Value: r.Host}}, traffic.HeaderList(r.Header)...)
	return traffic.Request{
		Method:  r.Method,
		URL:     r.URL.String(),
		Scheme:  r.URL.Scheme,
		Host:    r.URL.Host,
		Path:    r.URL.RequestURI(),
		Proto:   r.Proto,
		Headers: hs,
		Body:    traffic.Body{Encoding: r.Header.Get("Content-Encoding")},
	}
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) {
	if isLocalHost(r.URL.Host) {
		p.serveLocal(w, r)
		return
	}
	start := time.Now()
	x := &traffic.Exchange{
		ID:      p.sink.NextID(),
		Kind:    traffic.KindHTTP,
		State:   traffic.StatePending,
		Started: start,
		Client:  r.RemoteAddr,
		Request: describeRequest(r),
		Timings: traffic.NoTimings(),
	}

	defaultPort := "80"
	if r.URL.Scheme == "https" {
		defaultPort = "443"
	}
	if p.targetsSelf(r.URL.Host, defaultPort) {
		p.refuseLoop(w, x)
		return
	}
	if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
		x.State = traffic.StateFailed
		x.Error = &traffic.Failure{
			Code:    "upstream",
			Message: "Only http and https URLs can be proxied.",
			Detail:  "unsupported scheme " + strconv.Quote(r.URL.Scheme),
		}
		p.sink.Record(x.Clone())
		writeFailure(w, http.StatusBadRequest, x.Error)
		return
	}

	reqBody := newCapture(p.maxBody)
	out := r.Clone(r.Context())
	out.RequestURI = ""
	upgrade := upgradeType(r.Header)
	removeHopHeaders(out.Header)
	if upgrade != "" {
		out.Header.Set("Connection", "Upgrade")
		out.Header.Set("Upgrade", upgrade)
	}
	if r.Body != nil && r.Body != http.NoBody {
		out.Body = &teeBody{rc: r.Body, c: reqBody}
	}
	// Go's client adds a User-Agent if there isn't one. An empty value stops
	// it, so upstream sees exactly what the client sent.
	if _, ok := out.Header["User-Agent"]; !ok {
		out.Header["User-Agent"] = []string{""}
	}
	tm := newTimer(start)
	out = out.WithContext(tm.attach(out.Context()))

	p.sink.Record(x.Clone())

	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		p.setRequestBody(x, reqBody)
		x.Timings = tm.result(time.Now())
		x.State = traffic.StateFailed
		x.Error = classify(err, r.URL.Host)
		p.sink.Record(x.Clone())
		p.log.Info("upstream request failed", "id", x.ID, "host", r.URL.Host, "code", x.Error.Code)
		if x.Error.Code != "client_closed" {
			status := http.StatusBadGateway
			if x.Error.Code == "timeout" {
				status = http.StatusGatewayTimeout
			}
			writeFailure(w, status, x.Error)
		}
		return
	}
	defer resp.Body.Close()

	x.State = traffic.StateStreaming
	x.Response = &traffic.Response{
		Status:     resp.StatusCode,
		StatusText: reasonPhrase(resp),
		Proto:      resp.Proto,
		Headers:    traffic.HeaderList(resp.Header),
		Body:       traffic.Body{Encoding: resp.Header.Get("Content-Encoding")},
	}
	p.setRequestBody(x, reqBody)
	p.sink.Record(x.Clone())

	if resp.StatusCode == http.StatusSwitchingProtocols {
		p.relayUpgrade(w, resp, x, start)
		return
	}

	h := w.Header()
	copyHeader(h, resp.Header)
	removeHopHeaders(h)
	w.WriteHeader(resp.StatusCode)

	respBody := newCapture(p.maxBody)
	readErr, writeErr := relay(w, resp.Body, respBody)
	for k, vv := range resp.Trailer {
		for _, v := range vv {
			h.Add(http.TrailerPrefix+k, v)
		}
	}
	end := time.Now()

	data, size, truncated := respBody.snapshot()
	x.Response.Body.SetData(data, size, truncated)
	p.setRequestBody(x, reqBody)
	x.Timings = tm.result(end)
	x.State = traffic.StateComplete
	switch {
	case writeErr != nil:
		x.State = traffic.StateFailed
		x.Error = &traffic.Failure{
			Code:    "client_closed",
			Message: "The client disconnected before the response finished.",
			Detail:  writeErr.Error(),
		}
	case readErr != nil:
		x.State = traffic.StateFailed
		x.Error = classify(readErr, r.URL.Host)
		x.Error.Message = "The response body was cut off. " + x.Error.Message
	}
	p.sink.Record(x.Clone())
	p.log.Debug("exchange done", "id", x.ID, "method", r.Method, "host", r.URL.Host, "status", resp.StatusCode)
}

func (p *Proxy) setRequestBody(x *traffic.Exchange, c *capture) {
	data, size, truncated := c.snapshot()
	x.Request.Body.SetData(data, size, truncated)
}

func reasonPhrase(resp *http.Response) string {
	code := strconv.Itoa(resp.StatusCode)
	if rest, ok := strings.CutPrefix(resp.Status, code); ok {
		if rest = strings.TrimSpace(rest); rest != "" {
			return rest
		}
	}
	return http.StatusText(resp.StatusCode)
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, 32<<10); return &b }}

// relay streams src to w, flushing after each read so event streams and long
// polls reach the client as they arrive.
func relay(w http.ResponseWriter, src io.Reader, c *capture) (readErr, writeErr error) {
	rc := http.NewResponseController(w)
	bp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bp)
	buf := *bp
	for {
		n, err := src.Read(buf)
		if n > 0 {
			c.Write(buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				return nil, werr
			}
			rc.Flush()
		}
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return err, nil
		}
	}
}

// relayUpgrade finishes a 101 Switching Protocols exchange by splicing the
// client and upstream connections together. Frames aren't decoded.
func (p *Proxy) relayUpgrade(w http.ResponseWriter, resp *http.Response, x *traffic.Exchange, start time.Time) {
	x.Upgraded = true
	fail := func(status int, f *traffic.Failure) {
		x.State = traffic.StateFailed
		x.Error = f
		x.Timings.Total = ms(time.Since(start))
		p.sink.Record(x.Clone())
		if status != 0 {
			writeFailure(w, status, f)
		}
	}

	backend, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		fail(http.StatusBadGateway, &traffic.Failure{
			Code:    "upstream",
			Message: "The server switched protocols but the connection can't be relayed.",
			Detail:  "101 response body is not writable",
		})
		return
	}
	client, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		backend.Close()
		fail(http.StatusInternalServerError, &traffic.Failure{
			Code:    "hijack",
			Message: "Could not take over the client connection for the protocol switch.",
			Detail:  err.Error(),
		})
		return
	}
	if !p.track(client, backend) {
		return
	}
	defer p.untrack(client, backend)

	resp.Body = nil
	err = resp.Write(brw)
	if err == nil {
		err = brw.Flush()
	}
	if err != nil {
		client.Close()
		backend.Close()
		fail(0, &traffic.Failure{Code: "client_closed", Message: "The client went away during the protocol switch.", Detail: err.Error()})
		return
	}

	var upObs, downObs io.Writer
	var rec *wsRecorder
	if strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		params := parseDeflate(resp.Header)
		x.WebSocket = &traffic.WSStats{Compressed: params.enabled}
		rec = &wsRecorder{p: p, id: x.ID, x: x}
		send, receive := rec.decoders(params, max(p.maxBody, 64<<10))
		upObs, downObs = send, receive
		p.sink.Record(x.Clone())
	}
	up, down := pipe(client, brw.Reader, backend, upObs, downObs)
	if rec != nil {
		rec.close()
	}
	x.Request.Body.Size = up
	x.Response.Body.Size = down
	x.Timings.Total = ms(time.Since(start))
	x.State = traffic.StateComplete
	p.sink.Record(x.Clone())
}

func connectTarget(r *http.Request) string {
	if _, _, err := net.SplitHostPort(r.Host); err != nil {
		return net.JoinHostPort(r.Host, "443")
	}
	return r.Host
}

func (p *Proxy) tunnelExchange(r *http.Request, target string, start time.Time) *traffic.Exchange {
	return &traffic.Exchange{
		ID:      p.sink.NextID(),
		Kind:    traffic.KindTunnel,
		State:   traffic.StatePending,
		Started: start,
		Client:  r.RemoteAddr,
		Request: traffic.Request{
			Method:  http.MethodConnect,
			URL:     target,
			Host:    target,
			Proto:   r.Proto,
			Headers: traffic.HeaderList(r.Header),
		},
		Timings: traffic.NoTimings(),
	}
}

func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	target := connectTarget(r)
	if p.targetsSelf(target, "443") {
		p.refuseLoop(w, p.tunnelExchange(r, target, start))
		return
	}
	host, _, _ := net.SplitHostPort(target)
	if p.shouldIntercept(host) {
		p.interceptConnect(w, r, target, start)
		return
	}
	p.passthrough(w, r, target, start)
}

// passthrough relays a CONNECT without decrypting it. The upstream is dialed
// before answering so a dead host gets a proper 502.
func (p *Proxy) passthrough(w http.ResponseWriter, r *http.Request, target string, start time.Time) {
	x := p.tunnelExchange(r, target, start)
	p.sink.Record(x.Clone())

	ctx, cancel := context.WithTimeout(r.Context(), p.dialer.Timeout)
	upstream, err := p.dialer.DialContext(ctx, "tcp", target)
	cancel()
	connected := time.Now()
	if err != nil {
		x.State = traffic.StateFailed
		x.Error = classify(err, target)
		x.Timings.Total = ms(connected.Sub(start))
		p.sink.Record(x.Clone())
		p.log.Info("tunnel dial failed", "id", x.ID, "host", target, "code", x.Error.Code)
		writeFailure(w, http.StatusBadGateway, x.Error)
		return
	}

	client, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		upstream.Close()
		x.State = traffic.StateFailed
		x.Error = &traffic.Failure{Code: "hijack", Message: "Could not take over the client connection.", Detail: err.Error()}
		p.sink.Record(x.Clone())
		writeFailure(w, http.StatusInternalServerError, x.Error)
		return
	}
	if !p.track(client, upstream) {
		return
	}
	defer p.untrack(client, upstream)

	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		client.Close()
		upstream.Close()
		return
	}
	x.State = traffic.StateStreaming
	x.Timings.Connect = ms(connected.Sub(start))
	x.Response = &traffic.Response{Status: 200, StatusText: "Connection Established", Proto: "HTTP/1.1", Headers: []traffic.Header{}}
	p.sink.Record(x.Clone())

	up, down := pipe(client, brw.Reader, upstream, nil, nil)
	x.Request.Body.Size = up
	x.Response.Body.Size = down
	x.Timings.Total = ms(time.Since(start))
	x.State = traffic.StateComplete
	p.sink.Record(x.Clone())
}

// halfCloseGrace is how long the second direction of a pipe may stay open
// after the first one finished. Without it, a peer that never closes its
// side would pin the goroutines forever.
const halfCloseGrace = 30 * time.Second

// pipe copies between client and upstream in both directions until both are
// done, then closes them. clientBuf holds anything the HTTP server read past
// the request headers and must be drained first. The observers, if set, see
// a copy of each direction's bytes.
func pipe(client net.Conn, clientBuf *bufio.Reader, upstream io.ReadWriteCloser, upObs, downObs io.Writer) (up, down int64) {
	var src io.Reader = client
	if clientBuf != nil {
		src = clientBuf
	}
	var fromUpstream io.Reader = upstream
	if upObs != nil {
		src = io.TeeReader(src, upObs)
	}
	if downObs != nil {
		fromUpstream = io.TeeReader(upstream, downObs)
	}
	done := make(chan struct{}, 2)
	go func() {
		up, _ = io.Copy(upstream, src)
		closeWrite(upstream)
		done <- struct{}{}
	}()
	go func() {
		down, _ = io.Copy(client, fromUpstream)
		closeWrite(client)
		done <- struct{}{}
	}()
	<-done
	t := time.AfterFunc(halfCloseGrace, func() {
		client.Close()
		upstream.Close()
	})
	<-done
	t.Stop()
	client.Close()
	upstream.Close()
	return up, down
}

func closeWrite(c any) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// IsClosedConn reports whether err is the expected result of closing a
// listener, for callers deciding whether to log it.
func IsClosedConn(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed)
}
