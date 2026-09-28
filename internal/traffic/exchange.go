// Package traffic defines captured exchanges and the store that holds them.
package traffic

import (
	"mime"
	"net/http"
	"sort"
	"time"
)

type Kind string

const (
	KindHTTP   Kind = "http"
	KindTunnel Kind = "tunnel"
)

type State string

const (
	StatePending   State = "pending"   // request seen, no response headers yet
	StateStreaming State = "streaming" // response headers seen, body in flight
	StateComplete  State = "complete"
	StateFailed    State = "failed"
)

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HeaderList flattens h into name/value pairs. The parser has already thrown
// away wire order, so we sort by name to at least be deterministic.
func HeaderList(h http.Header) []Header {
	names := make([]string, 0, len(h))
	n := 0
	for k, vv := range h {
		names = append(names, k)
		n += len(vv)
	}
	sort.Strings(names)
	out := make([]Header, 0, n)
	for _, k := range names {
		for _, v := range h[k] {
			out = append(out, Header{Name: k, Value: v})
		}
	}
	return out
}

type Body struct {
	Size      int64  `json:"size"`
	Captured  int64  `json:"captured"`
	Truncated bool   `json:"truncated"`
	Encoding  string `json:"encoding,omitempty"`

	data []byte
}

func (b *Body) Data() []byte { return b.data }

// SetData stores p as the captured bytes. p must not be modified afterwards;
// stored exchanges share it.
func (b *Body) SetData(p []byte, size int64, truncated bool) {
	b.data = p
	b.Size = size
	b.Captured = int64(len(p))
	b.Truncated = truncated
}

type Request struct {
	Method  string   `json:"method"`
	URL     string   `json:"url"`
	Scheme  string   `json:"scheme"`
	Host    string   `json:"host"`
	Path    string   `json:"path"`
	Proto   string   `json:"proto"`
	Headers []Header `json:"headers"`
	Body    Body     `json:"body"`
}

type Response struct {
	Status     int      `json:"status"`
	StatusText string   `json:"statusText"`
	Proto      string   `json:"proto"`
	Headers    []Header `json:"headers"`
	Body       Body     `json:"body"`
}

// Timings are in milliseconds. -1 means the phase didn't happen, e.g. no DNS
// lookup on a reused connection.
type Timings struct {
	DNS     float64 `json:"dns"`
	Connect float64 `json:"connect"`
	TLS     float64 `json:"tls"`
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
	Total   float64 `json:"total"`
}

func NoTimings() Timings {
	return Timings{-1, -1, -1, -1, -1, -1, -1}
}

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
	Hint    string `json:"hint,omitempty"`
}

type Exchange struct {
	ID       uint64    `json:"id"`
	Rev      uint64    `json:"rev"`
	Kind     Kind      `json:"kind"`
	State    State     `json:"state"`
	Started  time.Time `json:"started"`
	Client   string    `json:"client"`
	Upgraded bool      `json:"upgraded"`
	Request  Request   `json:"request"`
	Response *Response `json:"response,omitempty"`
	Timings  Timings   `json:"timings"`
	Error    *Failure  `json:"error,omitempty"`
}

// Clone returns a copy that can be stored while the original keeps changing.
// Header slices and body bytes are shared: the proxy only ever replaces them.
func (x *Exchange) Clone() Exchange {
	c := *x
	if x.Response != nil {
		r := *x.Response
		c.Response = &r
	}
	if x.Error != nil {
		e := *x.Error
		c.Error = &e
	}
	return c
}

type Summary struct {
	ID          uint64    `json:"id"`
	Rev         uint64    `json:"rev"`
	Kind        Kind      `json:"kind"`
	State       State     `json:"state"`
	Started     time.Time `json:"started"`
	Upgraded    bool      `json:"upgraded,omitempty"`
	Method      string    `json:"method"`
	Scheme      string    `json:"scheme"`
	Host        string    `json:"host"`
	Path        string    `json:"path"`
	Proto       string    `json:"proto"`
	Status      int       `json:"status,omitempty"`
	StatusText  string    `json:"statusText,omitempty"`
	ContentType string    `json:"contentType,omitempty"`
	ReqSize     int64     `json:"reqSize"`
	RespSize    int64     `json:"respSize"`
	Duration    float64   `json:"duration"`
	Error       string    `json:"error,omitempty"`
}

func (x *Exchange) Summary() Summary {
	s := Summary{
		ID:       x.ID,
		Rev:      x.Rev,
		Kind:     x.Kind,
		State:    x.State,
		Started:  x.Started,
		Upgraded: x.Upgraded,
		Method:   x.Request.Method,
		Scheme:   x.Request.Scheme,
		Host:     x.Request.Host,
		Path:     x.Request.Path,
		Proto:    x.Request.Proto,
		ReqSize:  x.Request.Body.Size,
		Duration: -1,
	}
	if x.Response != nil {
		s.Status = x.Response.Status
		s.StatusText = x.Response.StatusText
		s.RespSize = x.Response.Body.Size
		s.Proto = x.Response.Proto
		s.ContentType = mediaType(x.Response.Headers)
	}
	if x.State == StateComplete || x.State == StateFailed {
		s.Duration = x.Timings.Total
	}
	if x.Error != nil {
		s.Error = x.Error.Code
	}
	return s
}

func mediaType(hs []Header) string {
	for _, h := range hs {
		if h.Name != "Content-Type" {
			continue
		}
		if mt, _, err := mime.ParseMediaType(h.Value); err == nil {
			return mt
		}
		return h.Value
	}
	return ""
}
