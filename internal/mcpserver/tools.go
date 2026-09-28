package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/traffickit/traffickit/internal/engine"
	"github.com/traffickit/traffickit/internal/launch"
	"github.com/traffickit/traffickit/internal/traffic"
)

type tools struct {
	c      *client
	redact bool
}

func ptr[T any](v T) *T { return &v }

var (
	readOnly    = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptr(false)}
	destructive = &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)}
)

func (t *tools) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_status",
		Description: "Whether the TrafficKit proxy is running, its address, whether HTTPS is decrypted, how much traffic is captured, which browsers can be launched and which launched windows are open.",
		Annotations: readOnly,
	}, t.getStatus)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_traffic",
		Description: "List captured HTTP exchanges, newest first, as one compact line each. Filter by host, method, status or free-text terms. Use before_id to page back through older traffic.",
		Annotations: readOnly,
	}, t.listTraffic)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_exchange",
		Description: "Full details of one exchange by id: request and response headers, decoded bodies (text only, size-limited), status, timings and any error with an explanation.",
		Annotations: readOnly,
	}, t.getExchange)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_websocket_messages",
		Description: "Messages sent and received on a WebSocket connection (an exchange whose type is websocket), oldest first. Filter by direction or text; page with after_seq.",
		Annotations: readOnly,
	}, t.getMessages)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_traffic",
		Description: "Find exchanges whose URL, headers or bodies contain some text, newest first, with a snippet around each match.",
		Annotations: readOnly,
	}, t.searchTraffic)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "wait_for_request",
		Description: "Wait until a new request matching the filters is captured (and finishes), then return it. Use after launch_browser or while the user reproduces a problem. Only traffic that starts after this call counts.",
		Annotations: readOnly,
	}, t.waitForRequest)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "launch_browser",
		Description: "Open a new, isolated browser window (temporary profile, no user logins) whose traffic goes through TrafficKit with HTTPS decrypted. Tell the user a window was opened.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
	}, t.launchBrowser)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "close_source",
		Description: "Close a browser or terminal window that TrafficKit launched, by source id from get_status.",
		Annotations: destructive,
	}, t.closeSource)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "proxy_settings",
		Description: "Proxy URL, CA certificate path and environment variables for routing your own commands (curl, Node, Python, git...) through TrafficKit so they show up in the capture.",
		Annotations: readOnly,
	}, t.proxySettings)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "clear_traffic",
		Description: "Delete all captured traffic in TrafficKit. The user loses what's currently shown, so only do this when they asked for a clean slate.",
		Annotations: destructive,
	}, t.clearTraffic)
}

// identify records which assistant is calling, so TrafficKit can show it.
func (t *tools) identify(req *mcp.CallToolRequest) {
	if req == nil || req.Session == nil {
		return
	}
	if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil && p.ClientInfo.Name != "" {
		t.c.name.Store(p.ClientInfo.Name)
	}
}

// Compact exchange description used in lists.
type trafficItem struct {
	ID         uint64  `json:"id"`
	Time       string  `json:"time"`
	Method     string  `json:"method"`
	URL        string  `json:"url"`
	Status     int     `json:"status,omitempty"`
	Type       string  `json:"type,omitempty"`
	SizeBytes  int64   `json:"size_bytes"`
	DurationMs float64 `json:"duration_ms,omitempty"`
	State      string  `json:"state,omitempty"`
	Error      string  `json:"error,omitempty"`
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func item(r *redactor, s traffic.Summary) trafficItem {
	it := trafficItem{
		ID:        s.ID,
		Time:      s.Started.Local().Format("15:04:05.000"),
		Method:    s.Method,
		Status:    s.Status,
		Type:      s.ContentType,
		SizeBytes: s.RespSize,
		Error:     s.Error,
	}
	if s.Kind == traffic.KindTunnel {
		it.URL = s.Host + " (encrypted tunnel, contents not decrypted)"
	} else {
		it.URL = r.URL(s.Scheme + "://" + s.Host + s.Path)
	}
	if s.Duration >= 0 {
		it.DurationMs = round1(s.Duration)
	}
	if s.State == traffic.StatePending || s.State == traffic.StateStreaming {
		it.State = "in progress"
	}
	if s.WebSocket {
		it.Type = fmt.Sprintf("websocket, %d messages", s.Messages)
	}
	return it
}

// get_status

type statusOut struct {
	Proxy          string          `json:"proxy"`
	HTTPSDecrypted bool            `json:"https_decrypted"`
	Exchanges      int             `json:"captured_exchanges"`
	Browsers       []string        `json:"launchable_browsers"`
	OpenSources    []launch.Source `json:"open_sources"`
	Redaction      bool            `json:"credentials_redacted"`
	Version        string          `json:"traffickit_version"`
}

func (t *tools) getStatus(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, statusOut, error) {
	t.identify(req)
	var st struct {
		Version   string             `json:"version"`
		Proxy     engine.ProxyStatus `json:"proxy"`
		Exchanges int                `json:"exchanges"`
	}
	var https engine.HTTPSStatus
	var targets struct {
		Items []launch.Target `json:"items"`
	}
	var srcs struct {
		Items []launch.Source `json:"items"`
	}
	for _, call := range []struct {
		path string
		out  any
	}{{"/v1/status", &st}, {"/v1/https", &https}, {"/v1/targets", &targets}, {"/v1/sources", &srcs}} {
		if err := t.c.json(ctx, "GET", call.path, nil, call.out); err != nil {
			return nil, statusOut{}, err
		}
	}
	out := statusOut{
		Proxy:          "stopped",
		HTTPSDecrypted: https.Intercept,
		Exchanges:      st.Exchanges,
		Browsers:       []string{},
		OpenSources:    srcs.Items,
		Redaction:      t.redact,
		Version:        st.Version,
	}
	if st.Proxy.Running {
		out.Proxy = "running on " + st.Proxy.Address
	}
	for _, tg := range targets.Items {
		if tg.Kind == "browser" {
			out.Browsers = append(out.Browsers, tg.ID)
		}
	}
	if out.OpenSources == nil {
		out.OpenSources = []launch.Source{}
	}
	return nil, out, nil
}

// list_traffic

type listIn struct {
	Filter     string `json:"filter,omitempty" jsonschema:"Free-text terms that must all appear in method, host, path, status or content type; prefix a term with - to exclude it, e.g. 'api -png'"`
	Host       string `json:"host,omitempty" jsonschema:"Only hosts containing this text"`
	Method     string `json:"method,omitempty" jsonschema:"Only this HTTP method, e.g. POST"`
	StatusMin  int    `json:"status_min,omitempty" jsonschema:"Only responses with status >= this"`
	StatusMax  int    `json:"status_max,omitempty" jsonschema:"Only responses with status <= this"`
	FailedOnly bool   `json:"failed_only,omitempty" jsonschema:"Only failed exchanges and 4xx/5xx responses"`
	BeforeID   uint64 `json:"before_id,omitempty" jsonschema:"Only exchanges older than this id, for paging"`
	Limit      int    `json:"limit,omitempty" jsonschema:"How many to return, 1-200 (default 30)"`
}

type listOut struct {
	Exchanges []trafficItem `json:"exchanges"`
	More      bool          `json:"more"`
	Redacted  []string      `json:"redacted,omitempty"`
}

func queryValues(in listIn) url.Values {
	v := url.Values{}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	num := func(k string, n uint64) {
		if n != 0 {
			v.Set(k, strconv.FormatUint(n, 10))
		}
	}
	set("filter", in.Filter)
	set("host", in.Host)
	set("method", in.Method)
	num("status_min", uint64(max(in.StatusMin, 0)))
	num("status_max", uint64(max(in.StatusMax, 0)))
	num("before", in.BeforeID)
	if in.FailedOnly {
		v.Set("failed", "1")
	}
	return v
}

type queryResult struct {
	Items  []traffic.Summary `json:"items"`
	More   bool              `json:"more"`
	LastID uint64            `json:"lastId"`
}

func (t *tools) listTraffic(ctx context.Context, req *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, listOut, error) {
	t.identify(req)
	limit := in.Limit
	if limit <= 0 {
		limit = 30
	}
	limit = min(limit, 200)
	v := queryValues(in)
	v.Set("limit", strconv.Itoa(limit))
	var res queryResult
	if err := t.c.json(ctx, "GET", "/v1/query?"+v.Encode(), nil, &res); err != nil {
		return nil, listOut{}, err
	}
	r := newRedactor(t.redact)
	out := listOut{Exchanges: make([]trafficItem, 0, len(res.Items)), More: res.More}
	for _, s := range res.Items {
		out.Exchanges = append(out.Exchanges, item(r, s))
	}
	out.Redacted = r.Hidden()
	return nil, out, nil
}

// get_exchange

type getIn struct {
	ID           uint64 `json:"id" jsonschema:"Exchange id from list_traffic or search_traffic"`
	SkipBodies   bool   `json:"skip_bodies,omitempty" jsonschema:"Leave out request and response bodies"`
	MaxBodyChars int    `json:"max_body_chars,omitempty" jsonschema:"Characters of each body to include, up to 100000 (default 8000)"`
}

type bodyOut struct {
	SizeBytes   int64  `json:"size_bytes"`
	ContentType string `json:"content_type,omitempty"`
	Encoding    string `json:"content_encoding,omitempty"`
	Text        string `json:"text,omitempty"`
	Binary      bool   `json:"binary,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Note        string `json:"note,omitempty"`
}

type messageOut struct {
	Status      int              `json:"status,omitempty"`
	StatusText  string           `json:"status_text,omitempty"`
	HTTPVersion string           `json:"http_version"`
	Headers     []traffic.Header `json:"headers"`
	Body        *bodyOut         `json:"body,omitempty"`
}

type exchangeOut struct {
	ID         uint64           `json:"id"`
	Method     string           `json:"method"`
	URL        string           `json:"url"`
	State      string           `json:"state"`
	Started    string           `json:"started"`
	DurationMs float64          `json:"duration_ms,omitempty"`
	Tunnel     bool             `json:"encrypted_tunnel,omitempty"`
	Error      *traffic.Failure `json:"error,omitempty"`
	Request    messageOut       `json:"request"`
	Response   *messageOut      `json:"response,omitempty"`
	Timings    traffic.Timings  `json:"timings_ms"`
	Redacted   []string         `json:"redacted,omitempty"`
}

func headerValue(hs []traffic.Header, name string) string {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// looksText reports whether b is readable UTF-8 without binary control bytes.
func looksText(b []byte) bool {
	sample := b[:min(len(b), 4096)]
	for len(sample) > 0 && !utf8.Valid(sample) && len(b) > len(sample) {
		sample = sample[:len(sample)-1] // cut a rune split at the boundary
	}
	if !utf8.Valid(sample) {
		return false
	}
	control := 0
	for _, c := range sample {
		if c == 0 {
			return false
		}
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			control++
		}
	}
	return control*50 < len(sample)+1
}

func (t *tools) body(ctx context.Context, r *redactor, id uint64, side string, b traffic.Body, contentType string, maxChars int) *bodyOut {
	if b.Size == 0 {
		return nil
	}
	out := &bodyOut{SizeBytes: b.Size, ContentType: contentType, Encoding: b.Encoding}
	if b.Captured == 0 {
		out.Note = "Transferred but not captured (still in flight, or the capture limit is 0)."
		return out
	}
	data, err := t.c.body(ctx, id, side)
	if err != nil {
		out.Note = "Could not load the body: " + err.Error()
		return out
	}
	if !looksText(data.bytes) {
		out.Binary = true
		out.Note = fmt.Sprintf("Binary content (%d bytes) not included.", len(data.bytes))
		return out
	}
	text := r.Body(contentType, string(data.bytes))
	if n := utf8.RuneCountInString(text); n > maxChars {
		cut := 0
		for i := range text {
			if cut == maxChars {
				text = text[:i]
				break
			}
			cut++
		}
		out.Truncated = true
		out.Note = fmt.Sprintf("Showing the first %d of %d characters; call again with a larger max_body_chars for more.", maxChars, n)
	}
	if data.truncated {
		out.Truncated = true
		out.Note += fmt.Sprintf(" TrafficKit only captured the first %d of %d bytes.", b.Captured, b.Size)
	}
	if data.decodeError != "" {
		out.Note += " Could not undo the content encoding: " + data.decodeError
	}
	out.Text = text
	return out
}

func (t *tools) getExchange(ctx context.Context, req *mcp.CallToolRequest, in getIn) (*mcp.CallToolResult, exchangeOut, error) {
	t.identify(req)
	var x traffic.Exchange
	if err := t.c.json(ctx, "GET", fmt.Sprintf("/v1/exchanges/%d", in.ID), nil, &x); err != nil {
		return nil, exchangeOut{}, err
	}
	maxChars := in.MaxBodyChars
	if maxChars <= 0 {
		maxChars = 8000
	}
	maxChars = min(maxChars, 100_000)
	r := newRedactor(t.redact)

	out := exchangeOut{
		ID:      x.ID,
		Method:  x.Request.Method,
		URL:     r.URL(x.Request.URL),
		State:   string(x.State),
		Started: x.Started.Local().Format(time.RFC3339Nano),
		Tunnel:  x.Kind == traffic.KindTunnel,
		Error:   x.Error,
		Timings: x.Timings,
		Request: messageOut{
			HTTPVersion: x.Request.Proto,
			Headers:     r.Headers(x.Request.Headers),
		},
	}
	if x.Timings.Total >= 0 {
		out.DurationMs = round1(x.Timings.Total)
	}
	if !in.SkipBodies && !out.Tunnel {
		out.Request.Body = t.body(ctx, r, x.ID, "request", x.Request.Body, headerValue(x.Request.Headers, "Content-Type"), maxChars)
	}
	if x.Response != nil {
		out.Response = &messageOut{
			Status:      x.Response.Status,
			StatusText:  x.Response.StatusText,
			HTTPVersion: x.Response.Proto,
			Headers:     r.Headers(x.Response.Headers),
		}
		if !in.SkipBodies && !out.Tunnel {
			out.Response.Body = t.body(ctx, r, x.ID, "response", x.Response.Body, headerValue(x.Response.Headers, "Content-Type"), maxChars)
		}
	}
	out.Redacted = r.Hidden()
	return nil, out, nil
}

// get_websocket_messages

type messagesIn struct {
	ID        uint64 `json:"id" jsonschema:"Exchange id of the WebSocket connection"`
	Direction string `json:"direction,omitempty" jsonschema:"send (client to server) or receive; both by default"`
	Contains  string `json:"contains,omitempty" jsonschema:"Only messages whose payload contains this text"`
	AfterSeq  int    `json:"after_seq,omitempty" jsonschema:"Only messages after this sequence number"`
	Limit     int    `json:"limit,omitempty" jsonschema:"1-500 (default 50)"`
	MaxChars  int    `json:"max_chars,omitempty" jsonschema:"Characters of each payload to include, up to 20000 (default 2000)"`
}

type wsMessageOut struct {
	Seq       int    `json:"seq"`
	Time      string `json:"time"`
	Direction string `json:"direction"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"size_bytes"`
	Text      string `json:"text,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	CloseCode int    `json:"close_code,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Note      string `json:"note,omitempty"`
}

type messagesOut struct {
	Messages []wsMessageOut `json:"messages"`
	More     bool           `json:"more"`
	Total    int            `json:"total_kept"`
	Dropped  int            `json:"older_dropped,omitempty"`
	Redacted []string       `json:"redacted,omitempty"`
}

func (t *tools) getMessages(ctx context.Context, req *mcp.CallToolRequest, in messagesIn) (*mcp.CallToolResult, messagesOut, error) {
	t.identify(req)
	if in.Direction != "" && in.Direction != "send" && in.Direction != "receive" {
		return nil, messagesOut{}, errors.New(`direction must be "send" or "receive"`)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	maxChars := in.MaxChars
	if maxChars <= 0 {
		maxChars = 2000
	}
	maxChars = min(maxChars, 20_000)
	v := url.Values{"limit": {strconv.Itoa(min(limit, 500))}, "after": {strconv.Itoa(max(in.AfterSeq, 0))}}
	if in.Direction != "" {
		v.Set("dir", in.Direction)
	}
	if in.Contains != "" {
		v.Set("q", in.Contains)
	}
	var res struct {
		Items []struct {
			Seq       int       `json:"seq"`
			Time      time.Time `json:"time"`
			Dir       string    `json:"dir"`
			Type      string    `json:"type"`
			Size      int64     `json:"size"`
			Truncated bool      `json:"truncated"`
			Note      string    `json:"note"`
			CloseCode int       `json:"closeCode"`
			Text      *string   `json:"text"`
			Base64    string    `json:"base64"`
		} `json:"items"`
		More    bool `json:"more"`
		Total   int  `json:"total"`
		Dropped int  `json:"dropped"`
	}
	if err := t.c.json(ctx, "GET", fmt.Sprintf("/v1/exchanges/%d/messages?%s", in.ID, v.Encode()), nil, &res); err != nil {
		return nil, messagesOut{}, err
	}
	r := newRedactor(t.redact)
	out := messagesOut{Messages: make([]wsMessageOut, 0, len(res.Items)), More: res.More, Total: res.Total, Dropped: res.Dropped}
	for _, m := range res.Items {
		o := wsMessageOut{
			Seq: m.Seq, Time: m.Time.Local().Format("15:04:05.000"), Direction: m.Dir, Type: m.Type,
			SizeBytes: m.Size, CloseCode: m.CloseCode, Truncated: m.Truncated, Note: m.Note,
		}
		if m.Text != nil {
			text := r.Body("", *m.Text)
			if utf8.RuneCountInString(text) > maxChars {
				text = string([]rune(text)[:maxChars])
				o.Truncated = true
			}
			o.Text = text
		} else if m.Base64 != "" {
			o.Binary = true
		}
		out.Messages = append(out.Messages, o)
	}
	out.Redacted = r.Hidden()
	return nil, out, nil
}

// search_traffic

type searchIn struct {
	Query         string `json:"query" jsonschema:"Text to look for in URLs, header lines and decoded bodies"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Maximum matches, 1-100 (default 20)"`
}

type searchHit struct {
	Exchange trafficItem `json:"exchange"`
	Where    string      `json:"where"`
	Snippet  string      `json:"snippet"`
}

type searchOut struct {
	Hits     []searchHit `json:"hits"`
	Redacted []string    `json:"redacted,omitempty"`
}

func (t *tools) searchTraffic(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
	t.identify(req)
	if in.Query == "" {
		return nil, searchOut{}, errors.New("query is empty")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	v := url.Values{"q": {in.Query}, "limit": {strconv.Itoa(min(limit, 100))}}
	if in.CaseSensitive {
		v.Set("case", "1")
	}
	var res struct {
		Items []engine.SearchHit `json:"items"`
	}
	if err := t.c.json(ctx, "GET", "/v1/search?"+v.Encode(), nil, &res); err != nil {
		return nil, searchOut{}, err
	}
	r := newRedactor(t.redact)
	out := searchOut{Hits: make([]searchHit, 0, len(res.Items))}
	for _, h := range res.Items {
		snip := h.Snippet
		switch h.Where {
		case "request header", "response header":
			if name, value, ok := cutHeader(snip); ok {
				red := r.Header(traffic.Header{Name: name, Value: value})
				snip = red.Name + ": " + red.Value
			} else {
				snip = r.Text(snip)
			}
		case "url":
			snip = r.URL(snip)
		default:
			snip = r.Body("", snip)
		}
		out.Hits = append(out.Hits, searchHit{Exchange: item(r, h.Exchange), Where: h.Where, Snippet: snip})
	}
	out.Redacted = r.Hidden()
	return nil, out, nil
}

// cutHeader splits a "Name: value" snippet when it starts at the name
// (snippets cut mid-line start with "…").
func cutHeader(s string) (string, string, bool) {
	name, value, ok := strings.Cut(s, ":")
	if !ok || name == "" || strings.ContainsAny(name, " …") {
		return "", "", false
	}
	return name, strings.TrimLeft(value, " "), true
}

// wait_for_request

type waitIn struct {
	Filter         string `json:"filter,omitempty" jsonschema:"Free-text terms as in list_traffic"`
	Host           string `json:"host,omitempty" jsonschema:"Host must contain this text"`
	Method         string `json:"method,omitempty" jsonschema:"HTTP method to match"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"How long to wait, 1-120 seconds (default 30)"`
}

type waitOut struct {
	Found    bool         `json:"found"`
	Exchange *trafficItem `json:"exchange,omitempty"`
	Note     string       `json:"note,omitempty"`
}

const pollEvery = 400 * time.Millisecond

func (t *tools) waitForRequest(ctx context.Context, req *mcp.CallToolRequest, in waitIn) (*mcp.CallToolResult, waitOut, error) {
	t.identify(req)
	timeout := in.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(min(timeout, 120))*time.Second)
	defer cancel()

	var start queryResult
	if err := t.c.json(ctx, "GET", "/v1/query?limit=1", nil, &start); err != nil {
		return nil, waitOut{}, err
	}
	v := queryValues(listIn{Filter: in.Filter, Host: in.Host, Method: in.Method})
	v.Set("after", strconv.FormatUint(start.LastID, 10))
	v.Set("limit", "200")

	r := newRedactor(t.redact)
	var found *traffic.Summary
	for found == nil {
		var res queryResult
		if err := t.c.json(ctx, "GET", "/v1/query?"+v.Encode(), nil, &res); err != nil {
			if ctx.Err() != nil {
				break
			}
			return nil, waitOut{}, err
		}
		if n := len(res.Items); n > 0 {
			found = &res.Items[n-1] // oldest match since we started waiting
			break
		}
		if !sleep(ctx, pollEvery) {
			break
		}
	}
	if found == nil {
		return nil, waitOut{Note: fmt.Sprintf("No matching request within %d seconds.", min(timeout, 120))}, nil
	}

	// Give an in-flight exchange the rest of the timeout to finish.
	for found.State == traffic.StatePending || found.State == traffic.StateStreaming {
		if !sleep(ctx, pollEvery) {
			break
		}
		var x traffic.Exchange
		if err := t.c.json(ctx, "GET", fmt.Sprintf("/v1/exchanges/%d", found.ID), nil, &x); err != nil {
			break
		}
		s := x.Summary()
		found = &s
	}
	it := item(r, *found)
	return nil, waitOut{Found: true, Exchange: &it, Note: "Use get_exchange with this id for headers and bodies."}, nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// launch_browser / close_source

type launchIn struct {
	Browser string `json:"browser,omitempty" jsonschema:"chrome, edge, brave, chromium or vivaldi; defaults to the first one installed"`
	URL     string `json:"url,omitempty" jsonschema:"Absolute http(s) URL to open; defaults to a page confirming the connection"`
}

type launchOut struct {
	Source launch.Source `json:"source"`
	Note   string        `json:"note"`
}

var browserPreference = []string{"chrome", "edge", "brave", "chromium", "vivaldi"}

func (t *tools) launchBrowser(ctx context.Context, req *mcp.CallToolRequest, in launchIn) (*mcp.CallToolResult, launchOut, error) {
	t.identify(req)
	target := in.Browser
	if target == "" {
		var targets struct {
			Items []launch.Target `json:"items"`
		}
		if err := t.c.json(ctx, "GET", "/v1/targets", nil, &targets); err != nil {
			return nil, launchOut{}, err
		}
		installed := map[string]bool{}
		for _, tg := range targets.Items {
			installed[tg.ID] = tg.Kind == "browser"
		}
		for _, id := range browserPreference {
			if installed[id] {
				target = id
				break
			}
		}
		if target == "" {
			return nil, launchOut{}, errors.New("no supported browser is installed (Chrome, Edge, Brave, Chromium or Vivaldi)")
		}
	}
	var src launch.Source
	if err := t.c.json(ctx, "POST", "/v1/sources", map[string]string{"target": target, "url": in.URL}, &src); err != nil {
		return nil, launchOut{}, err
	}
	return nil, launchOut{
		Source: src,
		Note:   "The window is open on the user's screen. Its traffic appears in list_traffic; wait_for_request can wait for a specific request.",
	}, nil
}

type closeIn struct {
	ID string `json:"id" jsonschema:"Source id from get_status or launch_browser"`
}

type closeOut struct {
	Closed bool `json:"closed"`
}

func (t *tools) closeSource(ctx context.Context, req *mcp.CallToolRequest, in closeIn) (*mcp.CallToolResult, closeOut, error) {
	t.identify(req)
	if err := t.c.json(ctx, "DELETE", "/v1/sources/"+url.PathEscape(in.ID), nil, nil); err != nil {
		return nil, closeOut{}, err
	}
	return nil, closeOut{Closed: true}, nil
}

// proxy_settings

type proxyOut struct {
	ProxyURL      string            `json:"proxy_url"`
	CACertificate string            `json:"ca_certificate_path"`
	Environment   map[string]string `json:"environment"`
	CurlExample   string            `json:"curl_example"`
	Note          string            `json:"note"`
}

func (t *tools) proxySettings(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, proxyOut, error) {
	t.identify(req)
	var st struct {
		Proxy engine.ProxyStatus `json:"proxy"`
	}
	var https engine.HTTPSStatus
	if err := t.c.json(ctx, "GET", "/v1/status", nil, &st); err != nil {
		return nil, proxyOut{}, err
	}
	if !st.Proxy.Running {
		return nil, proxyOut{}, errors.New("the TrafficKit proxy is stopped; ask the user to start it")
	}
	if err := t.c.json(ctx, "GET", "/v1/https", nil, &https); err != nil {
		return nil, proxyOut{}, err
	}
	addr := "127.0.0.1:" + strconv.Itoa(st.Proxy.Port)
	env := launch.TerminalEnv(launch.Env{ProxyAddr: addr, CAPath: https.CA.Path}, https.CA.Path)
	delete(env, "TRAFFICKIT_ACTIVE")
	curl := fmt.Sprintf("curl --proxy http://%s --cacert %q https://example.com/", addr, https.CA.Path)
	if runtime.GOOS == "windows" {
		// Windows' curl.exe (Schannel) can't check revocation for our certificates.
		curl = fmt.Sprintf("curl.exe --proxy http://%s --cacert %q --ssl-revoke-best-effort https://example.com/", addr, https.CA.Path)
	}
	return nil, proxyOut{
		ProxyURL:      "http://" + addr,
		CACertificate: https.CA.Path,
		Environment:   env,
		CurlExample:   curl,
		Note:          "Set these variables for a single command or shell session only. Tools that ignore proxy variables, or only trust the OS certificate store (Go on Windows/macOS, .NET, Java), need their own configuration.",
	}, nil
}

// clear_traffic

type clearOut struct {
	Cleared bool `json:"cleared"`
}

func (t *tools) clearTraffic(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, clearOut, error) {
	t.identify(req)
	if err := t.c.json(ctx, "DELETE", "/v1/exchanges", nil, nil); err != nil {
		return nil, clearOut{}, err
	}
	return nil, clearOut{Cleared: true}, nil
}
