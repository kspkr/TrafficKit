package engine

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/traffickit/traffickit/internal/bodyutil"
	"github.com/traffickit/traffickit/internal/traffic"
)

type SearchHit struct {
	Exchange traffic.Summary `json:"exchange"`
	Where    string          `json:"where"` // url, request header, response header, request body, response body
	Snippet  string          `json:"snippet"`
}

// searchScan bounds how many exchanges one search looks at, newest first.
const searchScan = 20_000

// Search finds exchanges whose URL, headers or (decoded) bodies contain
// text. One hit per exchange, newest first.
func (e *Engine) Search(ctx context.Context, text string, caseSensitive bool, limit int) ([]SearchHit, error) {
	if text == "" {
		return nil, nil
	}
	norm := func(s string) string { return s }
	if !caseSensitive {
		norm = strings.ToLower
	}
	needle := norm(text)
	find := func(where, hay string) (SearchHit, bool) {
		i := strings.Index(norm(hay), needle)
		if i < 0 {
			return SearchHit{}, false
		}
		return SearchHit{Where: where, Snippet: snippet(hay, i, len(needle))}, true
	}

	var hits []SearchHit
	for _, x := range e.store.Recent(searchScan) {
		if err := ctx.Err(); err != nil {
			return hits, err
		}
		hit, ok := searchExchange(&x, find)
		if !ok {
			continue
		}
		hit.Exchange = x.Summary()
		hits = append(hits, hit)
		if len(hits) == limit {
			break
		}
	}
	return hits, nil
}

func searchExchange(x *traffic.Exchange, find func(where, hay string) (SearchHit, bool)) (SearchHit, bool) {
	if h, ok := find("url", x.Request.URL); ok {
		return h, true
	}
	for _, hd := range x.Request.Headers {
		if h, ok := find("request header", hd.Name+": "+hd.Value); ok {
			return h, true
		}
	}
	if x.Response != nil {
		for _, hd := range x.Response.Headers {
			if h, ok := find("response header", hd.Name+": "+hd.Value); ok {
				return h, true
			}
		}
	}
	if h, ok := find("request body", bodyText(&x.Request.Body)); ok {
		return h, true
	}
	if x.Response != nil {
		if h, ok := find("response body", bodyText(&x.Response.Body)); ok {
			return h, true
		}
	}
	return SearchHit{}, false
}

func bodyText(b *traffic.Body) string {
	data := b.Data()
	if len(data) == 0 {
		return ""
	}
	if b.Encoding != "" {
		if d, err := bodyutil.Decode(b.Encoding, data); err == nil {
			data = d
		}
	}
	return string(data)
}

// snippet returns about 80 characters around the match, on one line.
func snippet(s string, at, n int) string {
	start, end := max(0, at-40), min(len(s), at+n+40)
	for start > 0 && !utf8.RuneStart(s[start]) {
		start--
	}
	for end < len(s) && !utf8.RuneStart(s[end]) {
		end++
	}
	out := strings.Join(strings.Fields(s[start:end]), " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out += "…"
	}
	return out
}
