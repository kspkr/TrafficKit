package traffic

import (
	"strconv"
	"strings"
)

// Query selects summaries, newest first. Zero fields don't filter.
type Query struct {
	// Filter uses the UI's quick-filter syntax: whitespace-separated terms
	// that must all appear in method, host+path, status, content type or
	// error; a leading "-" excludes.
	Filter     string
	Host       string // substring of the host, case-insensitive
	Method     string // exact, case-insensitive
	StatusMin  int
	StatusMax  int
	FailedOnly bool
	Before     uint64 // only IDs below this (for paging backwards)
	After      uint64 // only IDs above this (for waiting on new traffic)
	Limit      int
}

type matcher struct {
	include, exclude []string
}

func compileFilter(f string) matcher {
	var m matcher
	for t := range strings.FieldsSeq(strings.ToLower(f)) {
		if len(t) > 1 && t[0] == '-' {
			m.exclude = append(m.exclude, t[1:])
		} else {
			m.include = append(m.include, t)
		}
	}
	return m
}

func haystack(s *Summary) string {
	var b strings.Builder
	b.WriteString(s.Method)
	b.WriteByte(' ')
	b.WriteString(s.Host)
	b.WriteString(s.Path)
	if s.Status != 0 {
		b.WriteByte(' ')
		b.WriteString(strconv.Itoa(s.Status))
	}
	for _, v := range []string{s.StatusText, s.ContentType, s.Error} {
		b.WriteByte(' ')
		b.WriteString(v)
	}
	if s.Kind == KindTunnel {
		b.WriteString(" tunnel")
	}
	if s.WebSocket {
		b.WriteString(" websocket")
	}
	return strings.ToLower(b.String())
}

func (m matcher) match(s *Summary) bool {
	if len(m.include) == 0 && len(m.exclude) == 0 {
		return true
	}
	h := haystack(s)
	for _, t := range m.include {
		if !strings.Contains(h, t) {
			return false
		}
	}
	for _, t := range m.exclude {
		if strings.Contains(h, t) {
			return false
		}
	}
	return true
}

func (q *Query) matches(m matcher, s *Summary) bool {
	switch {
	case q.Before != 0 && s.ID >= q.Before,
		q.After != 0 && s.ID <= q.After,
		q.Host != "" && !strings.Contains(strings.ToLower(s.Host), strings.ToLower(q.Host)),
		q.Method != "" && !strings.EqualFold(s.Method, q.Method),
		q.StatusMin != 0 && s.Status < q.StatusMin,
		q.StatusMax != 0 && (s.Status == 0 || s.Status > q.StatusMax),
		q.FailedOnly && s.Error == "" && s.Status < 400:
		return false
	}
	return m.match(s)
}

// Query returns matching summaries newest first, and whether more matched
// beyond the limit.
func (s *Store) Query(q Query) ([]Summary, bool) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	m := compileFilter(q.Filter)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Summary
	for i := len(s.order) - 1; i >= 0; i-- {
		id := s.order[i]
		if q.After != 0 && id <= q.After {
			break
		}
		sum := s.byID[id].Summary()
		if !q.matches(m, &sum) {
			continue
		}
		if len(out) == q.Limit {
			return out, true
		}
		out = append(out, sum)
	}
	return out, false
}

// Recent returns up to n exchanges, newest first.
func (s *Store) Recent(n int) []Exchange {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n = min(n, len(s.order))
	out := make([]Exchange, 0, n)
	for i := len(s.order) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, s.byID[s.order[i]].Clone())
	}
	return out
}
