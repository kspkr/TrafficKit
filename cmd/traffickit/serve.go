package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/traffickit/traffickit/internal/api"
	"github.com/traffickit/traffickit/internal/discovery"
	"github.com/traffickit/traffickit/internal/engine"
)

type readyLine struct {
	Event   string             `json:"event"`
	Version string             `json:"version"`
	API     string             `json:"api"`
	Token   string             `json:"token"`
	Proxy   engine.ProxyStatus `json:"proxy"`
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var (
		common      commonFlags
		apiPort     int
		noProxy     bool
		origins     stringList
		devState    string
		exitOnStdin bool
	)
	common.register(fs)
	fs.IntVar(&apiPort, "api-port", 0, "control API port on 127.0.0.1 (default: any free port)")
	fs.BoolVar(&noProxy, "no-proxy", false, "don't start the proxy until asked to through the API")
	fs.Var(&origins, "allow-origin", "extra origin allowed to call the API (repeatable)")
	fs.StringVar(&devState, "dev-state", "", "also write the ready line to this file (development only)")
	fs.BoolVar(&exitOnStdin, "exit-on-stdin-close", false, "exit when stdin closes (for use as a child process)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: traffickit serve [flags]\n\nRuns the engine and prints one JSON ready line with the API address and token.\n\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if apiPort < 0 || apiPort > 65535 {
		return fmt.Errorf("-api-port %d is outside 0-65535", apiPort)
	}

	cfg, err := common.load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.Log, os.Stderr)
	eng, err := engine.New(cfg, log)
	if err != nil {
		return err
	}

	token, err := newToken()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)))
	if err != nil {
		return fmt.Errorf("control API: %w", err)
	}
	srv := api.New(eng, api.Options{
		Token:          token,
		AllowedOrigins: append(append([]string{}, api.TauriOrigins...), origins...),
		Version:        version,
		Logger:         log.With("component", "api"),
	})
	srv.SetPort(ln.Addr().(*net.TCPAddr).Port)
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go httpSrv.Serve(ln)

	if cfg.Proxy.AutoStart && !noProxy {
		// A busy port shouldn't stop the app from opening; the UI shows
		// the error and lets the user pick another.
		if _, err := eng.StartProxy(cfg.Proxy.Bind, cfg.Proxy.Port); err != nil {
			log.Warn("proxy did not start", "err", err)
		}
	}

	ready, _ := json.Marshal(readyLine{
		Event:   "ready",
		Version: version,
		API:     "http://" + ln.Addr().String(),
		Token:   token,
		Proxy:   eng.ProxyStatus(),
	})
	ready = append(ready, '\n')
	os.Stdout.Write(ready)
	// Let `traffickit mcp` and other local tools find this engine.
	if err := discovery.Write(cfg.DataDir, discovery.Info{
		API:     "http://" + ln.Addr().String(),
		Token:   token,
		PID:     os.Getpid(),
		Version: version,
	}); err != nil {
		log.Warn("could not write discovery file; `traffickit mcp` won't find this engine", "err", err)
	}
	defer discovery.Remove(cfg.DataDir, os.Getpid())
	if devState != "" {
		if err := writePrivate(devState, ready); err != nil {
			return fmt.Errorf("dev state: %w", err)
		}
		defer os.Remove(devState)
	}
	log.Info("engine ready", "api", ln.Addr().String(), "version", version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if exitOnStdin {
		go func() {
			io.Copy(io.Discard, os.Stdin)
			stop()
		}()
	}
	<-ctx.Done()

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	eng.Shutdown(shutdownCtx)
	// Event streams never finish on their own, so don't wait long for them.
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		httpSrv.Close()
	}
	return nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// writePrivate writes data to path readable only by the current user.
// On Windows the file inherits the ACL of its directory, which for the
// repo's dev folder is the user's profile.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
