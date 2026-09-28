package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/traffickit/traffickit/internal/traffic"
)

// sink records every snapshot and signals when an exchange finishes.
type sink struct {
	mu     sync.Mutex
	id     uint64
	states map[uint64][]traffic.State
	done   chan traffic.Exchange
}

func newSink() *sink {
	return &sink{states: make(map[uint64][]traffic.State), done: make(chan traffic.Exchange, 64)}
}

func (s *sink) NextID() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id++
	return s.id
}

func (s *sink) Record(x traffic.Exchange) {
	s.mu.Lock()
	s.states[x.ID] = append(s.states[x.ID], x.State)
	s.mu.Unlock()
	if x.State == traffic.StateComplete || x.State == traffic.StateFailed {
		s.done <- x
	}
}

func (s *sink) wait(t *testing.T) traffic.Exchange {
	t.Helper()
	select {
	case x := <-s.done:
		return x
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for exchange to finish")
		return traffic.Exchange{}
	}
}

type harness struct {
	proxy  *Proxy
	sink   *sink
	addr   string
	client *http.Client
}

func startProxy(t *testing.T, maxBody int64) *harness {
	t.Helper()
	s := newSink()
	p := New(Options{Sink: s, MaxBodyBytes: maxBody, DialTimeout: 2 * time.Second})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p.SetSelfPort(ln.Addr().(*net.TCPAddr).Port)
	srv := &http.Server{Handler: p}
	go srv.Serve(ln)
	t.Cleanup(func() {
		srv.Close()
		p.Close()
	})
	proxyURL, _ := url.Parse("http://" + ln.Addr().String())
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableCompression: true}
	t.Cleanup(tr.CloseIdleConnections)
	return &harness{proxy: p, sink: s, addr: ln.Addr().String(), client: &http.Client{Transport: tr}}
}

func header(hs []traffic.Header, name string) (string, bool) {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}

func TestForwardGET(t *testing.T) {
	var seen http.Header
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Origin", "yes")
		w.WriteHeader(http.StatusTeapot)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer origin.Close()
	h := startProxy(t, 1<<20)

	req, _ := http.NewRequest("GET", origin.URL+"/things?id=7", nil)
	req.Header.Set("X-Custom", "abc")
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("Connection", "X-Drop-Me")
	req.Header.Set("X-Drop-Me", "secret")
	req.Header.Set("User-Agent", "") // Go's client omits the header entirely when empty
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot || string(body) != `{"ok":true}` || resp.Header.Get("X-Origin") != "yes" {
		t.Fatalf("client got %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if seen.Get("X-Custom") != "abc" {
		t.Error("custom header not forwarded")
	}
	for _, hop := range []string{"Proxy-Connection", "X-Drop-Me"} {
		if seen.Get(hop) != "" {
			t.Errorf("hop-by-hop header %s was forwarded", hop)
		}
	}
	if _, ok := seen["User-Agent"]; ok {
		t.Errorf("proxy added a User-Agent: %q", seen.Get("User-Agent"))
	}

	x := h.sink.wait(t)
	if x.State != traffic.StateComplete || x.Kind != traffic.KindHTTP {
		t.Fatalf("state=%s kind=%s err=%+v", x.State, x.Kind, x.Error)
	}
	if x.Request.Method != "GET" || x.Request.Path != "/things?id=7" || x.Request.Scheme != "http" {
		t.Errorf("request = %+v", x.Request)
	}
	if v, _ := header(x.Request.Headers, "X-Custom"); v != "abc" {
		t.Error("request header not recorded")
	}
	if x.Response.Status != 418 || x.Response.StatusText != "I'm a teapot" {
		t.Errorf("status = %d %q", x.Response.Status, x.Response.StatusText)
	}
	if string(x.Response.Body.Data()) != `{"ok":true}` || x.Response.Body.Size != 11 || x.Response.Body.Truncated {
		t.Errorf("response body = %+v %q", x.Response.Body, x.Response.Body.Data())
	}
	if x.Timings.Total < 0 || x.Timings.Wait < 0 {
		t.Errorf("timings not filled: %+v", x.Timings)
	}
	states := h.sink.states[x.ID]
	want := []traffic.State{traffic.StatePending, traffic.StateStreaming, traffic.StateComplete}
	if fmt.Sprint(states) != fmt.Sprint(want) {
		t.Errorf("state sequence = %v, want %v", states, want)
	}
}

func TestRequestBodyCaptureIsBounded(t *testing.T) {
	var got string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		io.WriteString(w, strings.Repeat("z", 100))
	}))
	defer origin.Close()
	h := startProxy(t, 8)

	payload := "0123456789abcdef"
	resp, err := h.client.Post(origin.URL+"/upload", "text/plain", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if got != payload {
		t.Fatalf("origin received %q; capture limit must not affect forwarding", got)
	}
	x := h.sink.wait(t)
	rb := x.Request.Body
	if string(rb.Data()) != "01234567" || rb.Size != 16 || !rb.Truncated || rb.Captured != 8 {
		t.Errorf("request body = %+v %q", rb, rb.Data())
	}
	if x.Response.Body.Size != 100 || !x.Response.Body.Truncated {
		t.Errorf("response body = %+v", x.Response.Body)
	}
}

func TestResponseStreamsBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, "data: second\n\n")
	}))
	defer origin.Close()
	defer close(release)
	h := startProxy(t, 1<<20)

	resp, err := h.client.Get(origin.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- l
	}()
	select {
	case l := <-line:
		if l != "data: first\n" {
			t.Fatalf("first line = %q", l)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first event was buffered by the proxy")
	}
}

func TestUpstreamRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	h := startProxy(t, 1024)

	resp, err := h.client.Get("http://" + dead + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || resp.Header.Get("X-TrafficKit-Error") != "refused" {
		t.Fatalf("got %d %q", resp.StatusCode, resp.Header.Get("X-TrafficKit-Error"))
	}
	x := h.sink.wait(t)
	if x.State != traffic.StateFailed || x.Error == nil || x.Error.Code != "refused" || x.Error.Hint == "" {
		t.Fatalf("exchange = %+v err=%+v", x.State, x.Error)
	}
}

func TestLoopIsRefused(t *testing.T) {
	h := startProxy(t, 1024)
	_, port, _ := net.SplitHostPort(h.addr)
	resp, err := h.client.Get("http://localhost:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusLoopDetected {
		t.Fatalf("status = %d, want 508", resp.StatusCode)
	}
	if x := h.sink.wait(t); x.Error == nil || x.Error.Code != "loop" {
		t.Fatalf("error = %+v", x.Error)
	}
}

func TestDirectRequestGetsHelpText(t *testing.T) {
	h := startProxy(t, 1024)
	resp, err := http.Get("http://" + h.addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "curl -x") {
		t.Fatalf("got %d %q", resp.StatusCode, b)
	}
}

func TestConnectTunnel(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "secure hello")
	}))
	defer origin.Close()
	h := startProxy(t, 1024)

	proxyURL, _ := url.Parse("http://" + h.addr)
	tr := &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
	}
	resp, err := (&http.Client{Transport: tr}).Get(origin.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "secure hello" {
		t.Fatalf("body = %q", b)
	}
	tr.CloseIdleConnections() // ends the tunnel

	x := h.sink.wait(t)
	if x.Kind != traffic.KindTunnel || x.State != traffic.StateComplete {
		t.Fatalf("kind=%s state=%s err=%+v", x.Kind, x.State, x.Error)
	}
	if x.Request.Method != "CONNECT" || x.Request.Host != strings.TrimPrefix(origin.URL, "https://") {
		t.Errorf("request = %+v", x.Request)
	}
	if x.Request.Body.Size == 0 || x.Response.Body.Size == 0 {
		t.Errorf("byte counts not recorded: up=%d down=%d", x.Request.Body.Size, x.Response.Body.Size)
	}
	if x.Timings.Connect < 0 {
		t.Error("connect time missing")
	}
}

func TestConnectDialFailure(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	h := startProxy(t, 1024)

	conn, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", dead, dead)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if x := h.sink.wait(t); x.Kind != traffic.KindTunnel || x.Error == nil {
		t.Fatalf("exchange = %+v", x)
	}
}

func TestProtocolUpgrade(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "echo" {
			http.Error(w, "upgrade header lost", http.StatusBadRequest)
			return
		}
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
		buf := make([]byte, 4)
		if _, err := io.ReadFull(brw, buf); err == nil {
			conn.Write(buf)
		}
	}))
	defer origin.Close()
	h := startProxy(t, 1024)

	conn, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	host := strings.TrimPrefix(origin.URL, "http://")
	fmt.Fprintf(conn, "GET %s/ws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n", origin.URL, host)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d %s", resp.StatusCode, b)
	}
	conn.Write([]byte("ping"))
	echo := make([]byte, 4)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(br, echo); err != nil || string(echo) != "ping" {
		t.Fatalf("echo = %q, %v", echo, err)
	}
	conn.Close()

	x := h.sink.wait(t)
	if !x.Upgraded || x.Response.Status != 101 || x.Request.Body.Size != 4 || x.Response.Body.Size != 4 {
		t.Fatalf("exchange = upgraded:%v resp:%+v up:%d", x.Upgraded, x.Response, x.Request.Body.Size)
	}
}

func TestCloseEndsTunnels(t *testing.T) {
	hold := make(chan struct{})
	upstream, _ := net.Listen("tcp", "127.0.0.1:0")
	defer upstream.Close()
	go func() {
		c, err := upstream.Accept()
		if err == nil {
			<-hold
			c.Close()
		}
	}()
	defer close(hold)
	h := startProxy(t, 1024)

	conn, _ := net.Dial("tcp", h.addr)
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %[1]s HTTP/1.1\r\nHost: %[1]s\r\n\r\n", upstream.Addr())
	if _, err := http.ReadResponse(bufio.NewReader(conn), nil); err != nil {
		t.Fatal(err)
	}
	h.proxy.Close()
	if x := h.sink.wait(t); x.State != traffic.StateComplete {
		t.Fatalf("state = %s", x.State)
	}
}

func TestRemoveHopHeaders(t *testing.T) {
	h := http.Header{
		"Connection":    {"close, X-Private"},
		"X-Private":     {"1"},
		"Keep-Alive":    {"timeout=5"},
		"Content-Type":  {"text/plain"},
		"Authorization": {"Bearer t"},
	}
	removeHopHeaders(h)
	if len(h) != 2 || h.Get("Content-Type") == "" || h.Get("Authorization") == "" {
		t.Fatalf("headers left: %v", h)
	}
}

func TestClassifyTLS(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer origin.Close()
	_, err := (&http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{}}}).Get(origin.URL)
	if err == nil {
		t.Fatal("expected certificate error")
	}
	if f := classify(err, "x"); f.Code != "tls" {
		t.Fatalf("code = %s for %v", f.Code, err)
	}
}
