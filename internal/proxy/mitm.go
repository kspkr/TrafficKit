package proxy

import (
	"bufio"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/traffickit/traffickit/internal/traffic"
)

// Issuer hands out certificates for intercepted hosts.
type Issuer interface {
	CertFor(host string) (*tls.Certificate, error)
	CertPEM() []byte
}

const handshakeTimeout = 10 * time.Second

func (p *Proxy) shouldIntercept(host string) bool {
	if p.ca == nil {
		return false
	}
	if strings.EqualFold(host, LocalHost) {
		return true
	}
	return p.intercept == nil || p.intercept(host)
}

// interceptConnect answers the CONNECT, terminates the client's TLS with a
// certificate for the target, and serves the requests inside as ordinary
// proxied HTTPS requests. Upstream TLS is a separate connection made by the
// transport, verified against the system roots as usual.
func (p *Proxy) interceptConnect(w http.ResponseWriter, r *http.Request, target string, start time.Time) {
	client, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		x := p.tunnelExchange(r, target, start)
		x.State = traffic.StateFailed
		x.Error = &traffic.Failure{Code: "hijack", Message: "Could not take over the client connection.", Detail: err.Error()}
		p.sink.Record(x.Clone())
		writeFailure(w, http.StatusInternalServerError, x.Error)
		return
	}
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		client.Close()
		return
	}

	// Look at the first byte: 0x16 starts a TLS handshake. Anything else
	// (plain HTTP or a raw protocol tunneled over CONNECT) is relayed as is.
	client.SetReadDeadline(time.Now().Add(handshakeTimeout))
	first, err := brw.Reader.Peek(1)
	client.SetReadDeadline(time.Time{})
	if err != nil {
		// Browsers open speculative connections and drop them unused.
		client.Close()
		return
	}
	if first[0] != 0x16 {
		// Browsers tunnel plain ws:// through CONNECT too. If it looks like
		// HTTP, capture it like any other request; otherwise relay it.
		if first[0] >= 'A' && first[0] <= 'Z' {
			p.serveTunneled(&bufferedConn{Conn: client, r: brw.Reader}, r, target, "http")
			return
		}
		p.relayRaw(client, brw.Reader, r, target, start)
		return
	}

	host, _, _ := net.SplitHostPort(target)
	tlsConn := tls.Server(&bufferedConn{Conn: client, r: brw.Reader}, &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name = host
			}
			return p.ca.CertFor(name)
		},
	})
	if !p.track(tlsConn) {
		return
	}
	defer p.untrack(tlsConn)

	tlsConn.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := tlsConn.Handshake(); err != nil {
		tlsConn.Close()
		p.recordHandshakeFailure(r, target, start, err)
		return
	}
	tlsConn.SetDeadline(time.Time{})

	p.serveTunneled(tlsConn, r, target, "https")
}

// serveTunneled serves the requests a client sends inside a CONNECT tunnel
// (decrypted, or plain HTTP) as ordinary proxied requests to target.
func (p *Proxy) serveTunneled(conn net.Conn, r *http.Request, target, scheme string) {
	host, port, _ := net.SplitHostPort(target)
	authority := target
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		authority = host
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.URL.Scheme = scheme
			req.URL.Host = authority
			req.RemoteAddr = r.RemoteAddr
			p.forward(w, req)
		}),
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	srv.Serve(newSingleConnListener(conn))
}

// relayRaw splices a CONNECT whose payload isn't TLS straight through.
func (p *Proxy) relayRaw(client net.Conn, buf *bufio.Reader, r *http.Request, target string, start time.Time) {
	x := p.tunnelExchange(r, target, start)
	p.sink.Record(x.Clone())
	upstream, err := p.dialer.Dial("tcp", target)
	if err != nil {
		client.Close()
		x.State = traffic.StateFailed
		x.Error = classify(err, target)
		x.Timings.Total = ms(time.Since(start))
		p.sink.Record(x.Clone())
		return
	}
	if !p.track(client, upstream) {
		return
	}
	defer p.untrack(client, upstream)
	x.State = traffic.StateStreaming
	x.Timings.Connect = ms(time.Since(start))
	x.Response = &traffic.Response{Status: 200, StatusText: "Connection Established", Proto: "HTTP/1.1", Headers: []traffic.Header{}}
	p.sink.Record(x.Clone())
	up, down := pipe(client, buf, upstream, nil, nil)
	x.Request.Body.Size = up
	x.Response.Body.Size = down
	x.Timings.Total = ms(time.Since(start))
	x.State = traffic.StateComplete
	p.sink.Record(x.Clone())
}

// recordHandshakeFailure turns a failed client handshake into a visible
// exchange, because "the app silently stopped working" is the most common
// interception problem and the least obvious one.
func (p *Proxy) recordHandshakeFailure(r *http.Request, target string, start time.Time, err error) {
	host, _, _ := net.SplitHostPort(target)
	msg := err.Error()
	// A client that rejects our certificate says so with a TLS alert.
	// Anything else (EOF, reset) is almost always a browser abandoning a
	// spare connection it opened speculatively; reporting those would bury
	// the real problems in noise.
	if !strings.Contains(msg, "remote error: tls:") {
		p.log.Debug("client abandoned TLS handshake", "host", host, "err", msg)
		return
	}
	x := p.tunnelExchange(r, target, start)
	x.State = traffic.StateFailed
	x.Timings.Total = ms(time.Since(start))
	switch {
	case strings.Contains(msg, "unknown certificate authority"),
		strings.Contains(msg, "bad certificate"),
		strings.Contains(msg, "certificate unknown"),
		strings.Contains(msg, "access denied"):
		x.Error = &traffic.Failure{
			Code:    "tls_untrusted",
			Message: "The client rejected TrafficKit's certificate for " + host + ".",
			Detail:  msg,
			Hint:    "Launch the client from the Connect screen so it trusts TrafficKit, or trust the TrafficKit CA in it. Apps that pin certificates can't be intercepted; add the host to HTTPS passthrough in Settings instead.",
		}
	default:
		x.Error = &traffic.Failure{
			Code:    "tls_handshake",
			Message: "The client ended the TLS handshake for " + host + ".",
			Detail:  msg,
			Hint:    "It may not accept TrafficKit's certificate or the TLS settings offered. Launch it from the Connect screen, or add the host to HTTPS passthrough in Settings.",
		}
	}
	p.sink.Record(x.Clone())
	p.log.Info("client TLS handshake failed", "host", host, "code", x.Error.Code)
}

// bufferedConn reads through the bufio.Reader that already holds the start
// of the client's handshake.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// singleConnListener lets http.Server serve one existing connection. Accept
// returns it once, then blocks until it's closed.
type singleConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
}

func newSingleConnListener(c net.Conn) *singleConnListener {
	return &singleConnListener{conn: c, done: make(chan struct{})}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = &closeNotifyConn{Conn: l.conn, done: l.done} })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *singleConnListener) Close() error   { return nil }
func (l *singleConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

type closeNotifyConn struct {
	net.Conn
	done chan struct{}
	once sync.Once
}

func (c *closeNotifyConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}
