package api

import (
	"errors"
	"net/http"
	"os"

	"github.com/traffickit/traffickit/internal/launch"
)

func (s *Server) getHTTPS(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.eng.HTTPS())
}

func (s *Server) setHTTPS(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Intercept   *bool    `json:"intercept"`
		Passthrough []string `json:"passthrough"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	cur := s.eng.HTTPS()
	intercept, pass := cur.Intercept, cur.Passthrough
	if req.Intercept != nil {
		intercept = *req.Intercept
	}
	if req.Passthrough != nil {
		pass = req.Passthrough
	}
	st, err := s.eng.SetHTTPS(intercept, pass)
	if err != nil {
		writeEngineError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) regenerateCA(w http.ResponseWriter, r *http.Request) {
	st, err := s.eng.RegenerateCA()
	if err != nil {
		writeEngineError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// The certificate is public; the key never leaves the engine.
func (s *Server) getCACert(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="traffickit-ca.crt"`)
	w.Write(s.eng.CA().CertPEM())
}

func (s *Server) revealCA(w http.ResponseWriter, r *http.Request) {
	if err := launch.Reveal(s.eng.CA().Info().Path); err != nil {
		writeError(w, http.StatusInternalServerError, "reveal_failed", "Could not open the file manager: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTargets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.eng.Targets()})
}

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.eng.Sources()})
}

func (s *Server) launchSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
		URL    string `json:"url"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	src, err := s.eng.Launch(req.Target, req.URL)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, launch.ErrUnknownTarget) {
			status = http.StatusNotFound
		}
		writeEngineError(w, status, err)
		return
	}
	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) stopSource(w http.ResponseWriter, r *http.Request) {
	if err := s.eng.StopSource(r.PathValue("id")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "not_found", "No running source with that ID.")
			return
		}
		writeError(w, http.StatusInternalServerError, "stop_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
