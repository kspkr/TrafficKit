package api

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/traffickit/traffickit/internal/traffic"
)

type messageJSON struct {
	Seq        int       `json:"seq"`
	Time       time.Time `json:"time"`
	Dir        string    `json:"dir"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	Compressed bool      `json:"compressed,omitempty"`
	Truncated  bool      `json:"truncated,omitempty"`
	Note       string    `json:"note,omitempty"`
	CloseCode  int       `json:"closeCode,omitempty"`
	// Payload as text when it's valid UTF-8, otherwise base64.
	Text   *string `json:"text,omitempty"`
	Base64 string  `json:"base64,omitempty"`
}

func toJSON(m traffic.WSMessage) messageJSON {
	out := messageJSON{
		Seq: m.Seq, Time: m.Time, Dir: m.Dir, Type: m.Type, Size: m.Size,
		Compressed: m.Compressed, Truncated: m.Truncated, Note: m.Note, CloseCode: m.CloseCode,
	}
	if m.Type != "binary" && utf8.Valid(m.Data) {
		t := string(m.Data)
		out.Text = &t
	} else if len(m.Data) > 0 {
		out.Base64 = base64.StdEncoding.EncodeToString(m.Data)
	}
	return out
}

// GET /v1/exchanges/{id}/messages?after=&limit=&dir=send|receive&q=
func (s *Server) getMessages(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", "Exchange IDs are positive integers.")
		return
	}
	q := r.URL.Query()
	after, err1 := parseUint(q.Get("after"), 0)
	limit, err2 := parseUint(q.Get("limit"), 500)
	dir := q.Get("dir")
	if err1 != nil || err2 != nil || limit == 0 || limit > 5000 || (dir != "" && dir != "send" && dir != "receive") {
		writeError(w, http.StatusBadRequest, "bad_request", "after and limit (1-5000) must be numbers; dir is send or receive.")
		return
	}
	query := traffic.MessageQuery{After: int(min(after, 1<<31)), Dir: dir, Limit: int(limit)}
	if text := strings.ToLower(q.Get("q")); text != "" {
		query.Match = func(m traffic.WSMessage) bool {
			return strings.Contains(strings.ToLower(string(m.Data)), text)
		}
	}
	items, more, total, dropped, ok := s.eng.Store().Messages(id, query)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "No exchange with that ID.")
		return
	}
	out := make([]messageJSON, len(items))
	for i, m := range items {
		out[i] = toJSON(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "more": more, "total": total, "dropped": dropped})
}
