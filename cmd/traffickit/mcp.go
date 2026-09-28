package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/traffickit/traffickit/internal/api"
	"github.com/traffickit/traffickit/internal/config"
	"github.com/traffickit/traffickit/internal/discovery"
	"github.com/traffickit/traffickit/internal/engine"
	"github.com/traffickit/traffickit/internal/mcpserver"
)

func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	var (
		common     commonFlags
		standalone bool
		noRedact   bool
	)
	common.register(fs)
	fs.BoolVar(&standalone, "standalone", false, "run a private engine and proxy instead of connecting to the TrafficKit app")
	fs.BoolVar(&noRedact, "no-redact", false, "send credentials (Authorization, cookies, API keys, tokens) to the assistant unredacted")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `Usage: traffickit mcp [flags]

Runs a Model Context Protocol server on stdin/stdout so AI assistants can read
captured traffic and open intercepted browsers. By default it connects to the
running TrafficKit app. Register it with your assistant, e.g.

    claude mcp add traffickit -- traffickit mcp

Flags:`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := common.load()
	if err != nil {
		return err
	}
	// stdout carries the protocol; logs go to stderr and stay quiet.
	if common.logLevel == "" {
		cfg.Log.Level = "warn"
	}
	cfg.Log.Format = "text"
	log := newLogger(cfg.Log, os.Stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := mcpserver.Options{DataDir: cfg.DataDir, Redact: !noRedact, Version: version, Logger: log}
	if standalone {
		info, shutdown, err := startPrivateEngine(cfg, log)
		if err != nil {
			return err
		}
		defer shutdown()
		opts.Fixed = &info
	}
	return mcpserver.New(opts).Run(ctx, &mcp.StdioTransport{})
}

// startPrivateEngine runs an engine and control API inside this process, for
// using TrafficKit from an assistant without the desktop app (e.g. in CI).
func startPrivateEngine(cfg config.Config, log *slog.Logger) (discovery.Info, func(), error) {
	eng, err := engine.New(cfg, log)
	if err != nil {
		return discovery.Info{}, nil, err
	}
	if _, err := eng.StartProxy(cfg.Proxy.Bind, cfg.Proxy.Port); err != nil {
		return discovery.Info{}, nil, err
	}
	token, err := newToken()
	if err != nil {
		return discovery.Info{}, nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return discovery.Info{}, nil, err
	}
	srv := api.New(eng, api.Options{Token: token, Version: version, Logger: log})
	srv.SetPort(ln.Addr().(*net.TCPAddr).Port)
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go httpSrv.Serve(ln)
	fmt.Fprintf(os.Stderr, "traffickit: standalone proxy on %s\n", eng.ProxyStatus().Address)

	info := discovery.Info{API: "http://" + ln.Addr().String(), Token: token, PID: os.Getpid(), Version: version}
	return info, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		eng.Shutdown(ctx)
		httpSrv.Close()
	}, nil
}
