package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/traffickit/traffickit/internal/config"
	"github.com/traffickit/traffickit/internal/engine"
	"github.com/traffickit/traffickit/internal/traffic"
)

const token = "test-token-0123456789"

type fixture struct {
	eng *engine.Engine
	url string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	eng, err := engine.New(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(eng, Options{Token: token, AllowedOrigins: []string{"http://localhost:5173"}, Version: "test"})
	ts := httptest.NewServer(srv.Handler())
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	var p int
	json.Unmarshal([]byte(port), &p)
	srv.SetPort(p)
	t.Cleanup(func() {
		ts.Close()
		eng.Shutdown(context.Background())
	})
	return &fixture{eng: eng, url: ts.URL}
}

func (f *fixture) do(t *testing.T, method, path string, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, f.url+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v // net/http ignores a Host entry in req.Header
			continue
		}
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func errCode(t *testing.T, resp *http.Response) string {
	return decode[map[string]apiError](t, resp)["error"].Code
}

func TestAuthRequired(t *testing.T) {
	f := setup(t)
	for _, auth := range []string{"", "Bearer wrong", "Basic " + token, token} {
		resp := f.do(t, "GET", "/v1/status", "", map[string]string{"Authorization": auth})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("auth %q: status %d", auth, resp.StatusCode)
		}
	}
	if resp := f.do(t, "GET", "/v1/status", "", nil); resp.StatusCode != 200 {
		t.Fatalf("valid token rejected: %d", resp.StatusCode)
	}
}

func TestHostCheckBlocksRebinding(t *testing.T) {
	f := setup(t)
	resp := f.do(t, "GET", "/v1/status", "", map[string]string{"Host": "evil.example:1234"})
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestOriginCheck(t *testing.T) {
	f := setup(t)
	resp := f.do(t, "GET", "/v1/status", "", map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign origin: %d acao=%q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	resp = f.do(t, "GET", "/v1/status", "", map[string]string{"Origin": "http://localhost:5173"})
	if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("allowed origin: %d", resp.StatusCode)
	}
}

func TestPreflightNeedsNoToken(t *testing.T) {
	f := setup(t)
	resp := f.do(t, "OPTIONS", "/v1/status", "", map[string]string{
		"Authorization": "", "Origin": "http://localhost:5173", "Access-Control-Request-Method": "GET",
	})
	if resp.StatusCode != http.StatusNoContent || !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("preflight: %d %v", resp.StatusCode, resp.Header)
	}
}

func TestProxyStartValidation(t *testing.T) {
	f := setup(t)
	cases := map[string]string{
		`{"port":70000}`:           "invalid_port",
		`{"bind":"0.0.0.0"}`:       "remote_bind_disabled",
		`{"bind":"$(whoami)"}`:     "invalid_bind",
		`{"port":"8080"}`:          "bad_request",
		`{"prot":8080}`:            "bad_request",
		`{"port":1}{"port":2}`:     "bad_request",
		strings.Repeat(" ", 2<<20): "bad_request",
	}
	for body, want := range cases {
		resp := f.do(t, "POST", "/v1/proxy/start", body, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%.30q: status %d", body, resp.StatusCode)
			continue
		}
		if got := errCode(t, resp); got != want {
			t.Errorf("%.30q: code %q, want %q", body, got, want)
		}
	}
}

func TestProxyStartStop(t *testing.T) {
	f := setup(t)
	resp := f.do(t, "POST", "/v1/proxy/start", `{"bind":"127.0.0.1","port":0}`, nil)
	st := decode[engine.ProxyStatus](t, resp)
	if resp.StatusCode != 200 || !st.Running || st.Port == 0 {
		t.Fatalf("start: %d %+v", resp.StatusCode, st)
	}

	blocker, _ := net.Listen("tcp", "127.0.0.1:0")
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port
	resp = f.do(t, "POST", "/v1/proxy/start", `{"port":`+itoa(port)+`}`, nil)
	if resp.StatusCode != http.StatusConflict || errCode(t, resp) != "port_in_use" {
		t.Fatalf("busy port: %d", resp.StatusCode)
	}

	resp = f.do(t, "POST", "/v1/proxy/stop", "", nil)
	if st := decode[engine.ProxyStatus](t, resp); st.Running {
		t.Fatal("still running")
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func seed(f *fixture, n int) []uint64 {
	var ids []uint64
	for range n {
		x := traffic.Exchange{
			ID:      f.eng.NextID(),
			Kind:    traffic.KindHTTP,
			State:   traffic.StateComplete,
			Request: traffic.Request{Method: "GET", Host: "a.test", Path: "/"},
		}
		f.eng.Record(x)
		ids = append(ids, x.ID)
	}
	return ids
}

func TestListPaging(t *testing.T) {
	f := setup(t)
	seed(f, 5)
	type page struct {
		Items []traffic.Summary `json:"items"`
		More  bool              `json:"more"`
	}
	p := decode[page](t, f.do(t, "GET", "/v1/exchanges?limit=3", "", nil))
	if len(p.Items) != 3 || !p.More {
		t.Fatalf("page 1: %d more=%v", len(p.Items), p.More)
	}
	p = decode[page](t, f.do(t, "GET", "/v1/exchanges?limit=3&after=3", "", nil))
	if len(p.Items) != 2 || p.More || p.Items[0].ID != 4 {
		t.Fatalf("page 2: %+v", p)
	}
	for _, q := range []string{"limit=0", "limit=9999", "after=-1", "limit=x"} {
		if resp := f.do(t, "GET", "/v1/exchanges?"+q, "", nil); resp.StatusCode != 400 {
			t.Errorf("%s: status %d", q, resp.StatusCode)
		}
	}
}

func TestGetAndDelete(t *testing.T) {
	f := setup(t)
	ids := seed(f, 2)
	x := decode[traffic.Exchange](t, f.do(t, "GET", "/v1/exchanges/1", "", nil))
	if x.ID != ids[0] || x.Request.Host != "a.test" {
		t.Fatalf("got %+v", x)
	}
	if resp := f.do(t, "GET", "/v1/exchanges/abc", "", nil); resp.StatusCode != 400 {
		t.Errorf("bad id: %d", resp.StatusCode)
	}
	if resp := f.do(t, "DELETE", "/v1/exchanges/1", "", nil); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp := f.do(t, "GET", "/v1/exchanges/1", "", nil); resp.StatusCode != 404 {
		t.Fatalf("after delete: %d", resp.StatusCode)
	}
	f.do(t, "DELETE", "/v1/exchanges", "", nil)
	if f.eng.Store().Len() != 0 {
		t.Fatal("clear left exchanges behind")
	}
}

func TestBodyEndpoint(t *testing.T) {
	f := setup(t)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	io.WriteString(zw, "<script>alert(1)</script>")
	zw.Close()

	x := traffic.Exchange{ID: f.eng.NextID(), State: traffic.StateComplete, Request: traffic.Request{Method: "GET"}}
	x.Response = &traffic.Response{Status: 200, Body: traffic.Body{Encoding: "gzip"}}
	x.Response.Body.SetData(gz.Bytes(), int64(gz.Len()), false)
	f.eng.Record(x)

	resp := f.do(t, "GET", "/v1/exchanges/1/body/response?decode=1", "", nil)
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "<script>alert(1)</script>" || resp.Header.Get("X-TK-Decoded") != "gzip" {
		t.Fatalf("decoded body = %q", b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("content type = %q; captured bodies must not be served as their own type", ct)
	}
	if resp.Header.Get("Content-Security-Policy") != "sandbox" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing sandbox headers")
	}

	resp = f.do(t, "GET", "/v1/exchanges/1/body/response", "", nil)
	b, _ = io.ReadAll(resp.Body)
	if !bytes.Equal(b, gz.Bytes()) {
		t.Error("raw body altered")
	}
	if resp := f.do(t, "GET", "/v1/exchanges/1/body/sideways", "", nil); resp.StatusCode != 400 {
		t.Errorf("bad side: %d", resp.StatusCode)
	}
}

func TestEventStream(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.url+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := bufio.NewScanner(resp.Body)

	next := func() map[string]json.RawMessage {
		t.Helper()
		if !lines.Scan() {
			t.Fatalf("stream ended: %v", lines.Err())
		}
		var ev map[string]json.RawMessage
		if err := json.Unmarshal(lines.Bytes(), &ev); err != nil {
			t.Fatalf("bad line %q: %v", lines.Text(), err)
		}
		return ev
	}
	if typ := string(next()["type"]); typ != `"hello"` {
		t.Fatalf("first event = %s", typ)
	}
	seed(f, 1)
	ev := next()
	if string(ev["type"]) != `"exchange"` {
		t.Fatalf("event = %s", ev["type"])
	}
	var sum traffic.Summary
	json.Unmarshal(ev["data"], &sum)
	if sum.ID != 1 || sum.Rev != 1 || sum.Host != "a.test" {
		t.Fatalf("summary = %+v", sum)
	}
	f.eng.Delete(1)
	if typ := string(next()["type"]); typ != `"removed"` {
		t.Fatalf("event = %s", typ)
	}
}

func TestHTTPSSettings(t *testing.T) {
	f := setup(t)
	st := decode[engine.HTTPSStatus](t, f.do(t, "GET", "/v1/https", "", nil))
	if !st.Intercept || st.CA.SPKI == "" {
		t.Fatalf("status = %+v", st)
	}
	resp := f.do(t, "POST", "/v1/https", `{"intercept":false,"passthrough":["*.bank.test","api.example.com"]}`, nil)
	st = decode[engine.HTTPSStatus](t, resp)
	if st.Intercept || len(st.Passthrough) != 2 {
		t.Fatalf("after update: %+v", st)
	}
	resp = f.do(t, "POST", "/v1/https", `{"passthrough":["http://nope"]}`, nil)
	if resp.StatusCode != 400 || errCode(t, resp) != "invalid_host" {
		t.Fatalf("bad pattern: %d", resp.StatusCode)
	}

	old := st.CA.SHA256
	st = decode[engine.HTTPSStatus](t, f.do(t, "POST", "/v1/https/ca/regenerate", "", nil))
	if st.CA.SHA256 == old {
		t.Fatal("CA not regenerated")
	}
	pemResp := f.do(t, "GET", "/v1/https/ca.pem", "", nil)
	b, _ := io.ReadAll(pemResp.Body)
	if !strings.HasPrefix(string(b), "-----BEGIN CERTIFICATE-----") || strings.Contains(string(b), "PRIVATE") {
		t.Fatalf("ca.pem = %.60q", b)
	}
}

func TestLaunchValidation(t *testing.T) {
	f := setup(t)
	// Proxy isn't running in this fixture.
	resp := f.do(t, "POST", "/v1/sources", `{"target":"chrome"}`, nil)
	if resp.StatusCode != 400 || errCode(t, resp) != "proxy_stopped" {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, u := range []string{"file:///etc/passwd", "--disable-web-security", "javascript:alert(1)", "example.com"} {
		resp := f.do(t, "POST", "/v1/sources", `{"target":"chrome","url":"`+u+`"}`, nil)
		if resp.StatusCode != 400 || errCode(t, resp) != "invalid_url" {
			t.Errorf("url %q: status %d", u, resp.StatusCode)
		}
	}
	f.do(t, "POST", "/v1/proxy/start", `{"port":0}`, nil)
	resp = f.do(t, "POST", "/v1/sources", `{"target":"no-such-browser"}`, nil)
	if resp.StatusCode != 404 {
		t.Fatalf("unknown target: %d", resp.StatusCode)
	}
	if resp := f.do(t, "DELETE", "/v1/sources/abc", "", nil); resp.StatusCode != 404 {
		t.Fatalf("stop unknown: %d", resp.StatusCode)
	}
}
