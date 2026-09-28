package api

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/traffickit/traffickit/internal/bodyutil"
	"github.com/traffickit/traffickit/internal/engine"
	"github.com/traffickit/traffickit/internal/events"
	"github.com/traffickit/traffickit/internal/traffic"
)

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.eng.Config()
	exe, _ := os.Executable()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    s.version,
		"proxy":      s.eng.ProxyStatus(),
		"exchanges":  s.eng.Store().Len(),
		"capture":    cfg.Capture,
		"clients":    s.eng.Clients(),
		"enginePath": exe,
	})
}

func (s *Server) queryExchanges(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var bad []string
	num := func(key string) uint64 {
		n, err := parseUint(q.Get(key), 0)
		if err != nil {
			bad = append(bad, key)
		}
		return n
	}
	query := traffic.Query{
		Filter:     q.Get("filter"),
		Host:       q.Get("host"),
		Method:     q.Get("method"),
		StatusMin:  int(num("status_min")),
		StatusMax:  int(num("status_max")),
		FailedOnly: q.Get("failed") == "1",
		Before:     num("before"),
		After:      num("after"),
		Limit:      int(num("limit")),
	}
	if len(bad) > 0 || query.Limit > 1000 || query.StatusMin > 999 || query.StatusMax > 999 {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid query parameters: "+strings.Join(bad, ", "))
		return
	}
	items, more := s.eng.Store().Query(query)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "more": more, "lastId": s.eng.Store().LastID()})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text := q.Get("q")
	limit, err := parseUint(q.Get("limit"), 20)
	if err != nil || limit == 0 || limit > 200 || text == "" || len(text) > 1000 {
		writeError(w, http.StatusBadRequest, "bad_request", "q is required (up to 1000 characters) and limit must be 1-200.")
		return
	}
	hits, err := s.eng.Search(r.Context(), text, q.Get("case") == "1", int(limit))
	if err != nil {
		return // client went away
	}
	if hits == nil {
		hits = []engine.SearchHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits})
}

func (s *Server) startProxy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bind *string `json:"bind"`
		Port *int    `json:"port"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	cur := s.eng.ProxyStatus()
	bind, port := cur.Bind, cur.Port
	if req.Bind != nil {
		bind = *req.Bind
	}
	if req.Port != nil {
		port = *req.Port
	}
	st, err := s.eng.StartProxy(bind, port)
	if err != nil {
		status := http.StatusBadRequest
		var ee *engine.Error
		if errors.As(err, &ee) {
			switch ee.Code {
			case "port_in_use", "port_forbidden", "listen_failed":
				status = http.StatusConflict // the request was fine, the machine said no
			}
		}
		writeEngineError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) stopProxy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.eng.StopProxy(r.Context()))
}

func (s *Server) listExchanges(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after, err := parseUint(q.Get("after"), 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "after must be a non-negative integer.")
		return
	}
	limit, err := parseUint(q.Get("limit"), 1000)
	if err != nil || limit == 0 || limit > 5000 {
		writeError(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 5000.")
		return
	}
	items, more := s.eng.Store().List(after, int(limit))
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "more": more})
}

func (s *Server) clearExchanges(w http.ResponseWriter, r *http.Request) {
	s.eng.Clear()
	w.WriteHeader(http.StatusNoContent)
}

func parseUint(v string, def uint64) (uint64, error) {
	if v == "" {
		return def, nil
	}
	return strconv.ParseUint(v, 10, 64)
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (traffic.Exchange, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", "Exchange IDs are positive integers.")
		return traffic.Exchange{}, false
	}
	x, ok := s.eng.Store().Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "No exchange with that ID. It may have been deleted or evicted.")
	}
	return x, ok
}

func (s *Server) getExchange(w http.ResponseWriter, r *http.Request) {
	if x, ok := s.lookup(w, r); ok {
		writeJSON(w, http.StatusOK, x)
	}
}

func (s *Server) deleteExchange(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", "Exchange IDs are positive integers.")
		return
	}
	if !s.eng.Delete(id) {
		writeError(w, http.StatusNotFound, "not_found", "No exchange with that ID.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getBody(w http.ResponseWriter, r *http.Request) {
	x, ok := s.lookup(w, r)
	if !ok {
		return
	}
	var body *traffic.Body
	switch r.PathValue("side") {
	case "request":
		body = &x.Request.Body
	case "response":
		if x.Response == nil {
			writeError(w, http.StatusNotFound, "not_found", "This exchange has no response yet.")
			return
		}
		body = &x.Response.Body
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "side must be request or response.")
		return
	}

	h := w.Header()
	// Captured bytes are hostile input. Never let a browser interpret them.
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Security-Policy", "sandbox")
	if body.Truncated {
		h.Set("X-TK-Truncated", "1")
	}
	data := body.Data()
	if r.URL.Query().Get("decode") == "1" && body.Encoding != "" && len(data) > 0 {
		decoded, err := bodyutil.Decode(body.Encoding, data)
		if err != nil {
			msg := err.Error()
			if body.Truncated {
				msg = "body was truncated before it could be fully decoded (" + msg + ")"
			}
			h.Set("X-TK-Decode-Error", strings.ReplaceAll(msg, "\n", " "))
		} else {
			data = decoded
			h.Set("X-TK-Decoded", body.Encoding)
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

const pingInterval = 15 * time.Second

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	sub := s.eng.Hub().Subscribe(4096)
	defer s.eng.Hub().Unsubscribe(sub)
	rc := http.NewResponseController(w)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	hello := events.Encode("hello", map[string]any{
		"proxy":   s.eng.ProxyStatus(),
		"https":   s.eng.HTTPS(),
		"sources": s.eng.Sources(),
		"clients": s.eng.Clients(),
		"lastId":  s.eng.Store().LastID(),
	})
	if _, err := w.Write(hello); err != nil {
		return
	}
	rc.Flush()

	resync := events.Encode("resync", nil)
	ping := events.Encode("ping", nil)
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		var batch [][]byte
		select {
		case <-r.Context().Done():
			return
		case line := <-sub.C:
			batch = append(batch, line)
			// Drain whatever else is queued so a burst costs one flush.
			for n := len(sub.C); n > 0; n-- {
				batch = append(batch, <-sub.C)
			}
		case <-ticker.C:
			batch = append(batch, ping)
		}
		if sub.Lagged() {
			batch = append([][]byte{resync}, batch...)
		}
		for _, line := range batch {
			if _, err := w.Write(line); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
