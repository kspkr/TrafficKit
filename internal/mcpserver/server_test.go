package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/traffickit/traffickit/internal/api"
	"github.com/traffickit/traffickit/internal/config"
	"github.com/traffickit/traffickit/internal/discovery"
	"github.com/traffickit/traffickit/internal/engine"
	"github.com/traffickit/traffickit/internal/traffic"
)

type env struct {
	eng     *engine.Engine
	session *mcp.ClientSession
	proxy   *http.Client
	origin  *httptest.Server
}

// setup runs a real engine and proxy, an origin server that hands out
// secrets, and an MCP client connected to the server under test.
func setup(t *testing.T, redact bool) *env {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	eng, err := engine.New(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	st, err := eng.StartProxy("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	apiSrv := api.New(eng, api.Options{Token: "tok", Version: "test"})
	ts := httptest.NewServer(apiSrv.Handler())
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	var p int
	json.Unmarshal([]byte(port), &p)
	apiSrv.SetPort(p)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=s3cr3t-session; HttpOnly")
		body, _ := io.ReadAll(r.Body)
		io.WriteString(w, `{"user":"ada","access_token":"tok-very-secret","echo":`+strconvQuote(string(body))+`}`)
	}))

	server := New(Options{Fixed: &discovery.Info{API: ts.URL, Token: "tok"}, Redact: redact, Version: "test"})
	st1, st2 := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st1, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-assistant", Version: "1"}, nil).Connect(ctx, st2, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, _ := url.Parse("http://" + st.Address)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	t.Cleanup(func() {
		session.Close()
		tr.CloseIdleConnections()
		origin.Close()
		ts.Close()
		eng.Shutdown(context.Background())
	})
	return &env{eng: eng, session: session, proxy: &http.Client{Transport: tr}, origin: origin}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// call runs a tool and decodes its structured output.
func (e *env) call(t *testing.T, name string, args any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := e.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s returned an error: %s", name, text(res))
	}
	if out != nil {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s: decoding %s: %v", name, b, err)
		}
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func (e *env) send(t *testing.T, method, path, body string) {
	t.Helper()
	req, _ := http.NewRequest(method, e.origin.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer abcdefghijklmnop")
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.proxy.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func (e *env) waitCaptured(t *testing.T, n int) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		items, _ := e.eng.Store().List(0, 1000)
		done := 0
		for _, s := range items {
			if s.Duration >= 0 {
				done++
			}
		}
		if done >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d exchanges finished", done, n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestToolsAreListed(t *testing.T) {
	e := setup(t, true)
	res, err := e.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"get_status", "list_traffic", "get_exchange", "search_traffic", "get_websocket_messages", "wait_for_request", "launch_browser", "close_source", "proxy_settings", "clear_traffic"} {
		if !strings.Contains(strings.Join(names, " "), want) {
			t.Errorf("tool %s missing from %v", want, names)
		}
	}
}

func TestListAndGetRedactsSecrets(t *testing.T) {
	e := setup(t, true)
	e.send(t, "POST", "/login?api_key=k123&page=2", `{"username":"ada","password":"hunter2"}`)
	e.send(t, "GET", "/profile", "")
	e.waitCaptured(t, 2)

	var list listOut
	e.call(t, "list_traffic", map[string]any{"method": "POST"}, &list)
	if len(list.Exchanges) != 1 {
		t.Fatalf("list = %+v", list)
	}
	it := list.Exchanges[0]
	if strings.Contains(it.URL, "k123") || !strings.Contains(it.URL, "page=2") {
		t.Errorf("url = %s", it.URL)
	}

	var x exchangeOut
	res := e.call(t, "get_exchange", map[string]any{"id": it.ID}, &x)
	all := text(res)
	for _, secret := range []string{"hunter2", "abcdefghijklmnop", "s3cr3t-session", "tok-very-secret", "k123"} {
		if strings.Contains(all, secret) {
			t.Errorf("secret %q reached the assistant", secret)
		}
	}
	if x.Response == nil || x.Response.Status != 200 || x.Request.Body == nil || !strings.Contains(x.Request.Body.Text, `"username":"ada"`) {
		t.Fatalf("exchange lost non-secret data: %s", all)
	}
	if len(x.Redacted) == 0 {
		t.Error("redacted list is empty")
	}
}

func TestNoRedact(t *testing.T) {
	e := setup(t, false)
	e.send(t, "POST", "/login", `{"password":"hunter2"}`)
	e.waitCaptured(t, 1)
	var list listOut
	e.call(t, "list_traffic", nil, &list)
	res := e.call(t, "get_exchange", map[string]any{"id": list.Exchanges[0].ID}, nil)
	if !strings.Contains(text(res), "hunter2") {
		t.Error("--no-redact still redacted")
	}
}

func TestSearch(t *testing.T) {
	e := setup(t, true)
	e.send(t, "POST", "/items", `{"sku":"blue-widget-42"}`)
	e.send(t, "GET", "/other", "")
	e.waitCaptured(t, 2)
	var out searchOut
	e.call(t, "search_traffic", map[string]any{"query": "BLUE-WIDGET"}, &out)
	if len(out.Hits) != 1 || out.Hits[0].Where != "request body" || !strings.Contains(out.Hits[0].Snippet, "blue-widget-42") {
		t.Fatalf("hits = %+v", out.Hits)
	}
}

func TestWaitForRequest(t *testing.T) {
	e := setup(t, true)
	e.send(t, "GET", "/before", "") // must not count
	e.waitCaptured(t, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		e.send(t, "GET", "/target-page", "")
	}()
	var out waitOut
	e.call(t, "wait_for_request", map[string]any{"filter": "target-page", "timeout_seconds": 10}, &out)
	if !out.Found || out.Exchange == nil || !strings.Contains(out.Exchange.URL, "/target-page") || out.Exchange.Status != 200 {
		t.Fatalf("wait = %+v", out)
	}

	var none waitOut
	e.call(t, "wait_for_request", map[string]any{"filter": "never-happens", "timeout_seconds": 1}, &none)
	if none.Found {
		t.Fatal("found a request that never happened")
	}
}

func TestStatusIdentifiesAssistant(t *testing.T) {
	e := setup(t, true)
	var st statusOut
	e.call(t, "get_status", nil, &st)
	if !strings.HasPrefix(st.Proxy, "running on 127.0.0.1:") || !st.Redaction {
		t.Fatalf("status = %+v", st)
	}
	clients := e.eng.Clients()
	if len(clients) != 1 || clients[0].Name != "test-assistant" {
		t.Fatalf("engine saw clients %+v", clients)
	}
}

func TestClearAndErrors(t *testing.T) {
	e := setup(t, true)
	e.send(t, "GET", "/x", "")
	e.waitCaptured(t, 1)
	e.call(t, "clear_traffic", nil, nil)
	if e.eng.Store().Len() != 0 {
		t.Fatal("traffic not cleared")
	}
	res, err := e.session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_exchange", Arguments: map[string]any{"id": 999}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(text(res), "No exchange") {
		t.Fatalf("missing exchange: %s", text(res))
	}
	res, _ = e.session.CallTool(context.Background(), &mcp.CallToolParams{Name: "launch_browser", Arguments: map[string]any{"browser": "netscape"}})
	if !res.IsError {
		t.Fatal("unknown browser accepted")
	}
}

func TestNotRunning(t *testing.T) {
	server := New(Options{DataDir: t.TempDir(), Redact: true})
	st1, st2 := mcp.NewInMemoryTransports()
	server.Connect(context.Background(), st1, nil)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "c"}, nil).Connect(context.Background(), st2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_status"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(text(res), "isn't running") {
		t.Fatalf("got %s", text(res))
	}
}

func TestWebSocketMessages(t *testing.T) {
	e := setup(t, true)
	st := e.eng.Store()
	id := st.NextID()
	x := traffic.Exchange{ID: id, Kind: traffic.KindHTTP, State: traffic.StateStreaming, Upgraded: true,
		Request:   traffic.Request{Method: "GET", Scheme: "http", Host: "chat.test", Path: "/ws"},
		WebSocket: &traffic.WSStats{Messages: 3}}
	e.eng.Record(x)
	st.AppendMessage(id, traffic.WSMessage{Dir: "send", Type: "text", Data: []byte(`{"op":"auth","token":"sekrit-token"}`)})
	st.AppendMessage(id, traffic.WSMessage{Dir: "receive", Type: "text", Data: []byte(`{"op":"ready"}`)})
	st.AppendMessage(id, traffic.WSMessage{Dir: "receive", Type: "binary", Data: []byte{0, 1, 2}})

	var out messagesOut
	res := e.call(t, "get_websocket_messages", map[string]any{"id": id}, &out)
	if strings.Contains(text(res), "sekrit-token") {
		t.Fatal("token in a WebSocket message reached the assistant")
	}
	if len(out.Messages) != 3 || out.Messages[1].Text != `{"op":"ready"}` || !out.Messages[2].Binary {
		t.Fatalf("messages = %+v", out.Messages)
	}
	e.call(t, "get_websocket_messages", map[string]any{"id": id, "direction": "receive", "contains": "ready"}, &out)
	if len(out.Messages) != 1 || out.Messages[0].Seq != 2 {
		t.Fatalf("filtered = %+v", out.Messages)
	}
	var list listOut
	e.call(t, "list_traffic", map[string]any{"filter": "websocket"}, &list)
	if len(list.Exchanges) != 1 || !strings.HasPrefix(list.Exchanges[0].Type, "websocket") {
		t.Fatalf("list = %+v", list.Exchanges)
	}
}
