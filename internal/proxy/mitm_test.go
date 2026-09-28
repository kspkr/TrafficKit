package proxy

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/traffickit/traffickit/internal/ca"
	"github.com/traffickit/traffickit/internal/traffic"
)

type mitmHarness struct {
	*harness
	ca *ca.Authority
}

// startMITM runs a proxy with a fresh CA. Upstream TLS trusts origin's test
// certificate; intercept decides which hosts get decrypted (nil: all).
func startMITM(t *testing.T, origin *httptest.Server, intercept func(string) bool) *mitmHarness {
	t.Helper()
	authority, err := ca.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	upstream := &http.Transport{}
	if origin != nil {
		upstream.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	}
	s := newSink()
	p := New(Options{Sink: s, MaxBodyBytes: 1 << 20, CA: authority, Intercept: intercept, Transport: upstream})
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
		upstream.CloseIdleConnections()
	})
	return &mitmHarness{harness: &harness{proxy: p, sink: s, addr: ln.Addr().String()}, ca: authority}
}

// client returns an HTTP client that goes through the proxy and trusts roots.
func (h *mitmHarness) client(t *testing.T, roots *x509.CertPool) *http.Client {
	proxyURL, _ := url.Parse("http://" + h.addr)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 5 * time.Second}
}

func TestInterceptHTTPS(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"secret":"visible"}`)
	}))
	defer origin.Close()
	h := startMITM(t, origin, nil)

	resp, err := h.client(t, h.ca.Pool()).Get(origin.URL + "/api?x=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != `{"secret":"visible"}` {
		t.Fatalf("client got %q", body)
	}
	if issuer := resp.TLS.PeerCertificates[0].Issuer.Organization; len(issuer) == 0 || issuer[0] != "TrafficKit" {
		t.Fatalf("client saw issuer %v, want the TrafficKit CA", issuer)
	}

	x := h.sink.wait(t)
	if x.Kind != traffic.KindHTTP || x.Request.Scheme != "https" || x.Request.Path != "/api?x=1" {
		t.Fatalf("exchange = kind %s scheme %s path %s", x.Kind, x.Request.Scheme, x.Request.Path)
	}
	if string(x.Response.Body.Data()) != `{"secret":"visible"}` {
		t.Fatalf("captured body = %q", x.Response.Body.Data())
	}
}

func TestUntrustedClientIsReported(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer origin.Close()
	h := startMITM(t, origin, nil)

	// The client trusts the system roots, not our CA.
	if _, err := h.client(t, nil).Get(origin.URL); err == nil {
		t.Fatal("client accepted a certificate it shouldn't trust")
	}
	x := h.sink.wait(t)
	if x.Kind != traffic.KindTunnel || x.Error == nil || x.Error.Code != "tls_untrusted" {
		t.Fatalf("exchange = %s %+v", x.Kind, x.Error)
	}
}

func TestPassthroughHostIsNotDecrypted(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "direct")
	}))
	defer origin.Close()
	h := startMITM(t, origin, func(string) bool { return false })

	originRoots := origin.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	c := h.client(t, originRoots)
	resp, err := c.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	c.Transport.(*http.Transport).CloseIdleConnections()
	if x := h.sink.wait(t); x.Kind != traffic.KindTunnel || x.State != traffic.StateComplete {
		t.Fatalf("exchange = %s %s", x.Kind, x.State)
	}
}

func TestStartPageOverHTTPS(t *testing.T) {
	h := startMITM(t, nil, func(string) bool { return false }) // LocalHost is intercepted regardless
	resp, err := h.client(t, h.ca.Pool()).Get("https://" + LocalHost + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Intercepted") {
		t.Fatalf("start page: %d %s", resp.StatusCode, body)
	}
	select {
	case x := <-h.sink.done:
		t.Fatalf("start page was recorded as traffic: %+v", x.Request)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCertificateDownload(t *testing.T) {
	h := startMITM(t, nil, nil)
	resp, err := http.Get("http://" + h.addr + "/certificate")
	if err != nil {
		t.Fatal(err)
	}
	pemBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Header.Get("Content-Type") != "application/x-x509-ca-cert" || string(pemBytes) != string(h.ca.CertPEM()) {
		t.Fatalf("got %s: %.40q", resp.Header.Get("Content-Type"), pemBytes)
	}
}

func TestAbandonedHandshakeIsNotReported(t *testing.T) {
	h := startMITM(t, nil, nil)
	conn, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	buf := make([]byte, 64)
	conn.Read(buf)
	conn.Write([]byte{0x16, 0x03, 0x01}) // start of a ClientHello, then hang up
	conn.Close()
	select {
	case x := <-h.sink.done:
		t.Fatalf("abandoned connection recorded: %+v", x.Error)
	case <-time.After(300 * time.Millisecond):
	}
}

// Browsers send plain ws:// and http:// through CONNECT as well; those
// requests should be captured, not just relayed.
func TestPlainHTTPInsideTunnelIsCaptured(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "plain inside tunnel")
	}))
	defer origin.Close()
	h := startMITM(t, origin, nil)
	target := strings.TrimPrefix(origin.URL, "http://")

	conn, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
	br := bufio.NewReader(conn)
	if resp, err := http.ReadResponse(br, nil); err != nil || resp.StatusCode != 200 {
		t.Fatalf("CONNECT: %v %v", resp, err)
	}
	io.WriteString(conn, "GET /inside HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "plain inside tunnel" {
		t.Fatalf("body = %q", body)
	}
	x := h.sink.wait(t)
	if x.Kind != traffic.KindHTTP || x.Request.Scheme != "http" || x.Request.Path != "/inside" {
		t.Fatalf("exchange = %s %s %s", x.Kind, x.Request.Scheme, x.Request.Path)
	}
}
