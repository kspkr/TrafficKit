// Package launch starts browsers and terminals that are pre-wired to send
// their traffic through TrafficKit, and keeps track of them.
//
// Browsers get a throwaway profile and trust the TrafficKit CA through
// Chromium's --ignore-certificate-errors-spki-list, which applies to that
// browser instance only. Nothing is installed system-wide.
package launch

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Target is something TrafficKit knows how to launch on this machine.
type Target struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // browser | terminal
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

// Source is a launched target that's (probably) still running.
type Source struct {
	ID      string    `json:"id"`
	Target  string    `json:"target"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	// Tracked is false when the OS hands the process off (e.g. macOS
	// `open`), so we can't tell when it exits or close it for the user.
	Tracked bool `json:"tracked"`
}

// Env is what a launch needs to know about the running proxy.
type Env struct {
	ProxyAddr string // host:port a local client can connect to
	CAPath    string // PEM file with the TrafficKit CA
	SPKI      string // base64 SHA-256 of the CA public key
	DataDir   string
}

var ErrUnknownTarget = errors.New("no such launch target on this machine")

type running struct {
	src     Source
	proc    *os.Process
	cleanup func()
}

// starter starts a process. Terminals on Windows can't go through exec.Cmd
// (see terminal_windows.go), so launches are expressed this way.
type starter func() (*os.Process, error)

func startCmd(cmd *exec.Cmd) starter {
	return func() (*os.Process, error) {
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd.Process, nil
	}
}

type Manager struct {
	log      *slog.Logger
	onChange func([]Source)
	detect   func() []Target // swapped in tests

	mu     sync.Mutex
	active map[string]*running
}

func NewManager(log *slog.Logger, onChange func([]Source)) *Manager {
	return &Manager{log: log, onChange: onChange, detect: Detect, active: make(map[string]*running)}
}

// Detect lists the browsers installed here plus a terminal, if one can be
// opened. It only stats a handful of well-known paths.
func Detect() []Target {
	targets := detectBrowsers()
	if t, ok := terminalTarget(); ok {
		targets = append(targets, t)
	}
	return targets
}

func (m *Manager) Available() []Target { return m.detect() }

func (m *Manager) Active() []Source {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeLocked()
}

func (m *Manager) activeLocked() []Source {
	out := make([]Source, 0, len(m.active))
	for _, r := range m.active {
		out = append(out, r.src)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

func (m *Manager) changed() {
	if m.onChange != nil {
		m.onChange(m.Active())
	}
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Launch starts target. url is opened in browsers; empty means the
// TrafficKit start page.
func (m *Manager) Launch(targetID string, env Env, url string) (Source, error) {
	var target *Target
	for _, t := range m.detect() {
		if t.ID == targetID {
			target = &t
			break
		}
	}
	if target == nil {
		return Source{}, fmt.Errorf("%w: %q", ErrUnknownTarget, targetID)
	}

	id := newID()
	var (
		start   starter
		tracked = true
		cleanup = func() {}
		err     error
	)
	switch target.Kind {
	case "browser":
		profile := filepath.Join(env.DataDir, "profiles", target.ID+"-"+id)
		if err := os.MkdirAll(profile, 0o700); err != nil {
			return Source{}, err
		}
		start = startCmd(exec.Command(target.Path, BrowserArgs(profile, env, url)...))
		cleanup = func() { removeWithRetry(profile) }
	case "terminal":
		start, tracked, cleanup, err = terminalCommand(*target, env, id)
		if err != nil {
			return Source{}, err
		}
	default:
		return Source{}, fmt.Errorf("don't know how to launch a %s", target.Kind)
	}

	proc, err := start()
	if err != nil {
		cleanup()
		return Source{}, fmt.Errorf("starting %s: %w", target.Name, err)
	}
	src := Source{
		ID:      id,
		Target:  target.ID,
		Kind:    target.Kind,
		Name:    target.Name,
		PID:     proc.Pid,
		Started: time.Now(),
		Tracked: tracked,
	}
	r := &running{src: src, proc: proc, cleanup: cleanup}
	m.mu.Lock()
	m.active[id] = r
	m.mu.Unlock()
	m.log.Info("launched source", "target", target.ID, "pid", src.PID)
	m.changed()

	go func() {
		proc.Wait()
		if !tracked {
			// The launcher process exits right away; the real window lives on
			// elsewhere. Keep listing it until the user dismisses it.
			return
		}
		m.mu.Lock()
		delete(m.active, id)
		m.mu.Unlock()
		cleanup()
		m.log.Info("source exited", "target", target.ID, "pid", src.PID)
		m.changed()
	}()
	return src, nil
}

// Stop closes a launched source. Untracked sources are just forgotten.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	r, ok := m.active[id]
	if ok && !r.src.Tracked {
		delete(m.active, id)
	}
	m.mu.Unlock()
	if !ok {
		return os.ErrNotExist
	}
	if !r.src.Tracked {
		r.cleanup()
		m.changed()
		return nil
	}
	// The Wait goroutine removes it from the list once it's gone.
	return r.proc.Kill()
}

// StopAll closes everything we launched; used when the engine shuts down.
func (m *Manager) StopAll() {
	m.mu.Lock()
	rs := make([]*running, 0, len(m.active))
	for _, r := range m.active {
		rs = append(rs, r)
	}
	m.mu.Unlock()
	for _, r := range rs {
		if r.src.Tracked {
			r.proc.Kill()
		}
	}
}

// CleanStaleProfiles removes browser profiles left behind by a crash.
func CleanStaleProfiles(dataDir string) {
	os.RemoveAll(filepath.Join(dataDir, "profiles"))
}

// Browsers keep files open for a moment after the main process exits.
func removeWithRetry(dir string) {
	for range 20 {
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}
