package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/traffickit/traffickit/internal/traffic"
)

// Everything the MCP server returns ends up with an AI provider. Unless the
// user turns it off, credential-looking values are replaced before they
// leave, keeping names and shapes so the traffic still makes sense.

var (
	sensitiveName = regexp.MustCompile(`(?i)(auth|token|secret|passw|pwd|api[-_]?key|apikey|session|cookie|signature|credential|private[-_]?key|csrf|xsrf|otp|ssn|card[-_]?number|cvv)`)
	// Short query/form names that are commonly secrets but too generic for
	// the pattern above.
	sensitiveExact = map[string]bool{"key": true, "code": true, "sig": true, "jwt": true, "access": true, "pass": true}

	jwtPattern    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	bearerPattern = regexp.MustCompile(`(?i)\b(bearer|basic|token)(\s+)[A-Za-z0-9._~+/=-]{8,}`)
)

func isSensitive(name string) bool {
	return sensitiveExact[strings.ToLower(name)] || sensitiveName.MatchString(name)
}

type redactor struct {
	enabled bool
	hidden  map[string]bool
}

func newRedactor(enabled bool) *redactor {
	return &redactor{enabled: enabled, hidden: make(map[string]bool)}
}

func (r *redactor) note(what string) { r.hidden[what] = true }

// Hidden lists what was redacted, for the tool result.
func (r *redactor) Hidden() []string {
	out := make([]string, 0, len(r.hidden))
	for k := range r.hidden {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func masked(v string) string { return fmt.Sprintf("[redacted, %d chars]", len(v)) }

// Text removes tokens that are recognisable wherever they appear.
func (r *redactor) Text(s string) string {
	if !r.enabled {
		return s
	}
	out := jwtPattern.ReplaceAllStringFunc(s, func(m string) string {
		r.note("JWT")
		return "[redacted JWT]"
	})
	return bearerPattern.ReplaceAllStringFunc(out, func(m string) string {
		r.note("bearer/basic credential")
		sub := bearerPattern.FindStringSubmatch(m)
		return sub[1] + sub[2] + "[redacted]"
	})
}

func (r *redactor) Header(h traffic.Header) traffic.Header {
	if !r.enabled {
		return h
	}
	name := strings.ToLower(h.Name)
	switch name {
	case "authorization", "proxy-authorization":
		scheme, rest, found := strings.Cut(h.Value, " ")
		r.note("header " + h.Name)
		if found {
			return traffic.Header{Name: h.Name, Value: scheme + " " + masked(rest)}
		}
		return traffic.Header{Name: h.Name, Value: masked(h.Value)}
	case "cookie":
		r.note("cookie values")
		var parts []string
		for c := range strings.SplitSeq(h.Value, ";") {
			k, _, _ := strings.Cut(strings.TrimSpace(c), "=")
			parts = append(parts, k+"=[redacted]")
		}
		return traffic.Header{Name: h.Name, Value: strings.Join(parts, "; ")}
	case "set-cookie":
		r.note("cookie values")
		first, attrs, _ := strings.Cut(h.Value, ";")
		k, _, _ := strings.Cut(first, "=")
		v := k + "=[redacted]"
		if attrs != "" {
			v += ";" + attrs
		}
		return traffic.Header{Name: h.Name, Value: v}
	}
	if isSensitive(h.Name) {
		r.note("header " + h.Name)
		return traffic.Header{Name: h.Name, Value: masked(h.Value)}
	}
	return traffic.Header{Name: h.Name, Value: r.Text(h.Value)}
}

func (r *redactor) Headers(hs []traffic.Header) []traffic.Header {
	out := make([]traffic.Header, len(hs))
	for i, h := range hs {
		out[i] = r.Header(h)
	}
	return out
}

// query redacts a raw query string (or form body), keeping order and the
// encoding of everything it leaves alone.
func (r *redactor) query(raw string) string {
	if !r.enabled || raw == "" {
		return raw
	}
	parts := strings.Split(raw, "&")
	for i, p := range parts {
		k, _, hasValue := strings.Cut(p, "=")
		name, err := url.QueryUnescape(k)
		if err != nil {
			name = k
		}
		if hasValue && isSensitive(name) {
			r.note("parameter " + name)
			parts[i] = k + "=[redacted]"
		}
	}
	return r.Text(strings.Join(parts, "&"))
}

// URL redacts sensitive query parameters in an absolute URL or a path.
func (r *redactor) URL(u string) string {
	base, q, found := strings.Cut(u, "?")
	if !found {
		return r.Text(u)
	}
	return r.Text(base) + "?" + r.query(q)
}

// Body redacts a decoded text body. JSON and form bodies have values of
// sensitive fields replaced; everything else only gets the token patterns.
func (r *redactor) Body(contentType, text string) string {
	if !r.enabled {
		return text
	}
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "json") || looksJSON(text):
		if out, ok := r.json(text); ok {
			return out
		}
	case strings.Contains(ct, "x-www-form-urlencoded"):
		return r.query(text)
	}
	return r.Text(text)
}

func looksJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

// json returns text unchanged when nothing needed redacting, so formatting
// and key order survive in the common case.
func (r *redactor) json(text string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	changed := false
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				if isSensitive(k) {
					if _, nested := child.(map[string]any); !nested {
						if _, list := child.([]any); !list {
							r.note("JSON field " + k)
							t[k] = "[redacted]"
							changed = true
							continue
						}
					}
				}
				t[k] = walk(child)
			}
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		case string:
			// JSON is often embedded in strings (GraphQL variables, echoed
			// requests, logged payloads); redact inside it too.
			if looksJSON(t) {
				if red, ok := r.json(t); ok && red != t {
					changed = true
					return red
				}
			}
			if red := r.Text(t); red != t {
				changed = true
				return red
			}
		}
		return v
	}
	v = walk(v)
	if !changed {
		return text, true
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimSpace(buf.String()), true
}
