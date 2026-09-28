// Package api serves the engine's control API on loopback. See
// docs/architecture/ipc.md for the protocol.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/traffickit/traffickit/internal/engine"
)

// Origins the Tauri webview uses on each platform.
var TauriOrigins = []string{"tauri://localhost", "http://tauri.localhost", "https://tauri.localhost"}

type Options struct {
	Token          string
	AllowedOrigins []string
	Version        string
	Logger         *slog.Logger
}

type Server struct {
	eng     *engine.Engine
	token   []byte
	origins map[string]bool
	version string
	log     *slog.Logger
	port    atomic.Int64
	handler http.Handler
}

func New(eng *engine.Engine, opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Server{
		eng:     eng,
		token:   []byte(opts.Token),
		origins: make(map[string]bool),
		version: opts.Version,
		log:     opts.Logger,
	}
	for _, o := range opts.AllowedOrigins {
		s.origins[o] = true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.getStatus)
	mux.HandleFunc("POST /v1/proxy/start", s.startProxy)
	mux.HandleFunc("POST /v1/proxy/stop", s.stopProxy)
	mux.HandleFunc("GET /v1/exchanges", s.listExchanges)
	mux.HandleFunc("GET /v1/query", s.queryExchanges)
	mux.HandleFunc("GET /v1/search", s.search)
	mux.HandleFunc("DELETE /v1/exchanges", s.clearExchanges)
	mux.HandleFunc("GET /v1/exchanges/{id}", s.getExchange)
	mux.HandleFunc("DELETE /v1/exchanges/{id}", s.deleteExchange)
	mux.HandleFunc("GET /v1/exchanges/{id}/body/{side}", s.getBody)
	mux.HandleFunc("GET /v1/https", s.getHTTPS)
	mux.HandleFunc("POST /v1/https", s.setHTTPS)
	mux.HandleFunc("GET /v1/https/ca.pem", s.getCACert)
	mux.HandleFunc("POST /v1/https/ca/regenerate", s.regenerateCA)
	mux.HandleFunc("POST /v1/https/ca/reveal", s.revealCA)
	mux.HandleFunc("GET /v1/targets", s.listTargets)
	mux.HandleFunc("GET /v1/sources", s.listSources)
	mux.HandleFunc("POST /v1/sources", s.launchSource)
	mux.HandleFunc("DELETE /v1/sources/{id}", s.stopSource)
	mux.HandleFunc("GET /v1/events", s.streamEvents)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "No such endpoint.")
	})
	s.handler = s.guard(mux)
	return s
}

// SetPort records the port the API listens on; the Host check needs it.
func (s *Server) SetPort(port int) { s.port.Store(int64(port)) }

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) hostAllowed(host string) bool {
	h, p, err := net.SplitHostPort(host)
	if err != nil || p != strconv.FormatInt(s.port.Load(), 10) {
		return false
	}
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func (s *Server) authorized(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && len(s.token) > 0 && subtle.ConstantTimeCompare([]byte(got), s.token) == 1
}

// guard applies the Host, Origin and token checks to every request.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			writeError(w, http.StatusMisdirectedRequest, "bad_host", "Requests must be addressed to the loopback API address.")
			return
		}
		h := w.Header()
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.origins[origin] {
				s.log.Warn("api request from unknown origin rejected", "origin", origin)
				writeError(w, http.StatusForbidden, "bad_origin", "This origin may not use the TrafficKit API.")
				return
			}
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", "X-TK-Truncated, X-TK-Decoded, X-TK-Decode-Error")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or wrong API token.")
			return
		}
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		if c := clientName(r.Header.Get("X-TK-Client")); c != "" {
			s.eng.NoteClient(c)
		}
		next.ServeHTTP(w, r)
	})
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: msg}})
}

func writeEngineError(w http.ResponseWriter, status int, err error) {
	var ee *engine.Error
	if errors.As(err, &ee) {
		writeJSON(w, status, map[string]apiError{"error": {Code: ee.Code, Message: ee.Message, Hint: ee.Hint}})
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", err.Error())
}

const maxRequestBody = 1 << 20

// readJSON decodes an optional JSON body strictly. An empty body leaves v alone.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil && dec.More() {
		err = errors.New("trailing data after JSON value")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// clientName cleans the self-reported client name shown in the UI.
func clientName(v string) string {
	var b strings.Builder
	for _, r := range v {
		if b.Len() >= 64 {
			break
		}
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
