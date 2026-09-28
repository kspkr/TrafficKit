// Package engine wires the proxy, the store and the event hub together and
// owns the proxy listener's lifecycle.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/traffickit/traffickit/internal/ca"
	"github.com/traffickit/traffickit/internal/config"
	"github.com/traffickit/traffickit/internal/events"
	"github.com/traffickit/traffickit/internal/launch"
	"github.com/traffickit/traffickit/internal/proxy"
	"github.com/traffickit/traffickit/internal/traffic"
)

// Error is a failure with a stable code and text meant for people.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	err     error
}

func (e *Error) Error() string {
	if e.err != nil {
		return e.Message + ": " + e.err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.err }

type ProxyStatus struct {
	Running bool   `json:"running"`
	Address string `json:"address,omitempty"`
	Bind    string `json:"bind"`
	Port    int    `json:"port"`
	Error   *Error `json:"error,omitempty"`
}

type Engine struct {
	cfg   config.Config
	log   *slog.Logger
	store *traffic.Store
	hub   *events.Hub

	mu     sync.Mutex // guards the proxy lifecycle
	srv    *http.Server
	px     *proxy.Proxy
	status ProxyStatus

	// Separate from mu: stopping the proxy holds mu while in-flight
	// exchanges finish, and those call Record.
	obsMu    sync.RWMutex
	onRecord []func(traffic.Exchange)

	ca       *ca.Authority
	launcher *launch.Manager
	httpsMu  sync.RWMutex
	https    config.HTTPS
	clients  clientTracker
}

// New loads (or creates) the CA under cfg.DataDir and sets up the engine.
// The proxy isn't started; call StartProxy.
func New(cfg config.Config, log *slog.Logger) (*Engine, error) {
	authority, err := ca.Load(filepath.Join(cfg.DataDir, "ca"))
	if err != nil {
		return nil, fmt.Errorf("certificate authority: %w", err)
	}
	e := &Engine{
		cfg:    cfg,
		log:    log,
		store:  traffic.NewStore(cfg.Capture.MaxExchanges),
		hub:    events.NewHub(),
		status: ProxyStatus{Bind: cfg.Proxy.Bind, Port: cfg.Proxy.Port},
		ca:     authority,
		https:  cfg.HTTPS,
	}
	launch.CleanStaleProfiles(cfg.DataDir)
	e.launcher = launch.NewManager(log.With("component", "launch"), func(s []launch.Source) {
		e.hub.Publish("sources", s)
	})
	return e, nil
}

func (e *Engine) Config() config.Config { return e.cfg }
func (e *Engine) Store() *traffic.Store { return e.store }
func (e *Engine) Hub() *events.Hub      { return e.hub }
func (e *Engine) NextID() uint64        { return e.store.NextID() }

// OnRecord registers fn to be called with every exchange snapshot. Register
// before starting the proxy; fn runs on proxy goroutines and must be quick.
func (e *Engine) OnRecord(fn func(traffic.Exchange)) {
	e.obsMu.Lock()
	e.onRecord = append(e.onRecord, fn)
	e.obsMu.Unlock()
}

// Record implements proxy.Sink.
func (e *Engine) Record(x traffic.Exchange) {
	sum, evicted, ok := e.store.Put(x)
	if !ok {
		return
	}
	e.hub.Publish("exchange", sum)
	if len(evicted) > 0 {
		e.hub.Publish("removed", map[string]any{"ids": evicted})
	}
	e.obsMu.RLock()
	fns := e.onRecord
	e.obsMu.RUnlock()
	for _, fn := range fns {
		fn(x)
	}
}

// Message implements proxy.Sink. The exchange's message count, carried on
// its summary, is what tells clients there's something new to fetch.
func (e *Engine) Message(id uint64, m traffic.WSMessage) {
	e.store.AppendMessage(id, m)
}

func (e *Engine) Delete(id uint64) bool {
	if !e.store.Delete(id) {
		return false
	}
	e.hub.Publish("removed", map[string]any{"ids": []uint64{id}})
	return true
}

func (e *Engine) Clear() {
	e.store.Clear()
	e.hub.Publish("cleared", nil)
}

func (e *Engine) ProxyStatus() ProxyStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// StartProxy starts listening on bind:port, restarting the proxy if it is
// already running elsewhere. Port 0 picks a free port.
func (e *Engine) StartProxy(bind string, port int) (ProxyStatus, error) {
	if port < 0 || port > 65535 {
		return e.ProxyStatus(), &Error{Code: "invalid_port", Message: fmt.Sprintf("Port %d is outside 1-65535.", port)}
	}
	if err := config.CheckBind(bind, e.cfg.Proxy.AllowRemote); err != nil {
		code, hint := "invalid_bind", "Use an IP address such as 127.0.0.1."
		if errors.Is(err, config.ErrRemoteDenied) {
			code = "remote_bind_disabled"
			hint = "Listening beyond this machine exposes captured traffic to the network. Set proxy.allowRemote in the config file if you really want this."
		}
		return e.ProxyStatus(), &Error{Code: code, Message: "Can't listen on " + bind + ".", Hint: hint, err: err}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.status.Running && e.status.Bind == bind && (port == 0 || e.status.Port == port) {
		return e.status, nil
	}
	// Moving to a new port: open it before closing the old one, so a typo in
	// the settings doesn't stop a working proxy. Same port on a different
	// address would collide with ourselves, so that case stops first.
	if e.status.Running && e.status.Port == port {
		e.stopLocked(context.Background())
	}

	addr := net.JoinHostPort(bind, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		lerr := listenError(err, port)
		if e.status.Running {
			e.log.Warn("proxy move failed, keeping current address", "addr", addr, "code", lerr.Code)
			return e.status, lerr
		}
		e.status = ProxyStatus{Bind: bind, Port: port, Error: lerr}
		e.hub.Publish("proxy", e.status)
		e.log.Warn("proxy listen failed", "addr", addr, "code", lerr.Code, "err", err)
		return e.status, lerr
	}
	e.stopLocked(context.Background())
	actual := ln.Addr().(*net.TCPAddr).Port

	px := proxy.New(proxy.Options{
		Sink:         e,
		Logger:       e.log.With("component", "proxy"),
		MaxBodyBytes: e.cfg.Capture.MaxBodyBytes,
		CA:           e.ca,
		Intercept:    e.shouldIntercept,
	})
	px.SetSelfPort(actual)
	srv := &http.Server{
		Handler:           px,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// net/http logs client hiccups (TLS garbage on the plain port, resets)
		// at a volume that's only useful when debugging the proxy itself.
		ErrorLog: slog.NewLogLogger(e.log.Handler(), slog.LevelDebug),
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !proxy.IsClosedConn(err) {
			e.log.Error("proxy stopped unexpectedly", "err", err)
		}
	}()

	e.srv, e.px = srv, px
	e.status = ProxyStatus{Running: true, Address: ln.Addr().String(), Bind: bind, Port: actual}
	e.hub.Publish("proxy", e.status)
	e.log.Info("proxy started", "addr", e.status.Address)
	return e.status, nil
}

func listenError(err error, port int) *Error {
	var errno syscall.Errno
	errors.As(err, &errno)
	switch {
	case errors.Is(err, syscall.EADDRINUSE) || errno == 10048:
		return &Error{
			Code:    "port_in_use",
			Message: fmt.Sprintf("Port %d is already in use.", port),
			Hint:    "Pick another port, or stop the program that is using it.",
			err:     err,
		}
	case errors.Is(err, syscall.EACCES) || errno == 10013:
		// On Windows this usually means Hyper-V/WSL reserved the port range.
		return &Error{
			Code:    "port_forbidden",
			Message: fmt.Sprintf("The system won't allow listening on port %d.", port),
			Hint:    "Ports below 1024 need elevated rights; on Windows, check `netsh int ipv4 show excludedportrange protocol=tcp` for reserved ranges.",
			err:     err,
		}
	}
	return &Error{Code: "listen_failed", Message: "Could not start the proxy.", Hint: err.Error(), err: err}
}

func (e *Engine) StopProxy(ctx context.Context) ProxyStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopLocked(ctx) {
		e.hub.Publish("proxy", e.status)
		e.log.Info("proxy stopped")
	}
	return e.status
}

func (e *Engine) stopLocked(ctx context.Context) bool {
	if e.srv == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	e.px.Close()
	if err := e.srv.Shutdown(ctx); err != nil {
		e.srv.Close()
	}
	e.srv, e.px = nil, nil
	e.status = ProxyStatus{Bind: e.status.Bind, Port: e.status.Port}
	return true
}

func (e *Engine) Shutdown(ctx context.Context) {
	e.launcher.StopAll()
	e.StopProxy(ctx)
}
