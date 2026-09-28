package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/traffickit/traffickit/internal/engine"
	"github.com/traffickit/traffickit/internal/traffic"
)

func runProxy(args []string) error {
	fs := flag.NewFlagSet("proxy", flag.ContinueOnError)
	var common commonFlags
	common.register(fs)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: traffickit proxy [flags]\n\nRuns the proxy in the foreground and prints one line per finished exchange.\n\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := common.load()
	if err != nil {
		return err
	}
	// Terminal output is the point of this command, so logs default to
	// warnings only unless asked for.
	if common.logLevel == "" {
		cfg.Log.Level = "warn"
	}
	cfg.Log.Format = "text"
	eng, err := engine.New(cfg, newLogger(cfg.Log, os.Stderr))
	if err != nil {
		return err
	}

	var mu sync.Mutex
	eng.OnRecord(func(x traffic.Exchange) {
		if x.State != traffic.StateComplete && x.State != traffic.StateFailed {
			return
		}
		mu.Lock()
		printExchange(os.Stdout, x)
		mu.Unlock()
	})

	st, err := eng.StartProxy(cfg.Proxy.Bind, cfg.Proxy.Port)
	if err != nil {
		if ee, ok := err.(*engine.Error); ok && ee.Hint != "" {
			return fmt.Errorf("%s\n%s", ee.Message, ee.Hint)
		}
		return err
	}
	fmt.Fprintf(os.Stderr, "Proxy listening on http://%s  (Ctrl+C to stop)\n", st.Address)
	fmt.Fprintf(os.Stderr, "Try: curl -x http://%s http://example.com/\n\n", st.Address)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	eng.Shutdown(shutdownCtx)
	return nil
}

func printExchange(w io.Writer, x traffic.Exchange) {
	ts := x.Started.Format("15:04:05.000")
	target := x.Request.URL
	if x.Kind == traffic.KindTunnel {
		target += " (tunnel)"
	}
	status := "---"
	if x.Response != nil {
		status = fmt.Sprint(x.Response.Status)
	}
	if x.Error != nil {
		fmt.Fprintf(w, "%s  %-7s %s  %s  ERROR %s: %s\n", ts, x.Request.Method, status, target, x.Error.Code, x.Error.Message)
		return
	}
	var size int64
	if x.Response != nil {
		size = x.Response.Body.Size
	}
	fmt.Fprintf(w, "%s  %-7s %s  %s  %s  %s\n", ts, x.Request.Method, status, target, formatBytes(size), formatMs(x.Timings.Total))
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func formatMs(ms float64) string {
	if ms < 0 {
		return ""
	}
	if ms >= 1000 {
		return strings.TrimSuffix(fmt.Sprintf("%.2f", ms/1000), "0") + " s"
	}
	return fmt.Sprintf("%.0f ms", ms)
}
