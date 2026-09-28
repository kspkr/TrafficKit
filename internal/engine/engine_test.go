package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/traffickit/traffickit/internal/config"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	e, err := New(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	return e
}

func TestStartStopProxy(t *testing.T) {
	e := newEngine(t)
	sub := e.Hub().Subscribe(16)

	st, err := e.StartProxy("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || st.Port == 0 {
		t.Fatalf("status = %+v", st)
	}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hi")
	}))
	defer origin.Close()
	proxyURL, _ := url.Parse("http://" + st.Address)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	deadline := time.Now().Add(3 * time.Second)
	for e.Store().Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if e.Store().Len() != 1 {
		t.Fatalf("store has %d exchanges", e.Store().Len())
	}

	st = e.StopProxy(context.Background())
	if st.Running {
		t.Fatal("still running after stop")
	}
	if _, err := net.DialTimeout("tcp", proxyURL.Host, 500*time.Millisecond); err == nil {
		t.Fatal("proxy port still accepting connections")
	}
	if len(sub.C) < 3 { // proxy start, exchange(s), proxy stop
		t.Fatalf("only %d events published", len(sub.C))
	}
}

func TestStartProxyPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	e := newEngine(t)
	st, err := e.StartProxy("127.0.0.1", port)
	var ee *Error
	if !errors.As(err, &ee) || ee.Code != "port_in_use" {
		t.Fatalf("err = %v", err)
	}
	if st.Running || st.Error == nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestFailedMoveKeepsProxyRunning(t *testing.T) {
	e := newEngine(t)
	before, err := e.StartProxy("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()

	after, err := e.StartProxy("127.0.0.1", busy.Addr().(*net.TCPAddr).Port)
	if err == nil {
		t.Fatal("expected port_in_use")
	}
	if !after.Running || after.Address != before.Address {
		t.Fatalf("proxy should still be on %s, status = %+v", before.Address, after)
	}
	c, err := net.DialTimeout("tcp", before.Address, time.Second)
	if err != nil {
		t.Fatalf("old listener closed: %v", err)
	}
	c.Close()
}

func TestStartProxyRejectsRemoteBind(t *testing.T) {
	e := newEngine(t)
	_, err := e.StartProxy("0.0.0.0", 0)
	var ee *Error
	if !errors.As(err, &ee) || ee.Code != "remote_bind_disabled" {
		t.Fatalf("err = %v", err)
	}
}

func TestRestartOnNewPort(t *testing.T) {
	e := newEngine(t)
	first, err := e.StartProxy("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	free := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	second, err := e.StartProxy("127.0.0.1", free)
	if err != nil {
		t.Fatal(err)
	}
	if second.Port != free || second.Port == first.Port {
		t.Fatalf("second = %+v", second)
	}
	if c, err := net.DialTimeout("tcp", first.Address, 500*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("old listener still open")
	}
}
