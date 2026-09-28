// Command traffickit is the TrafficKit engine and command-line interface.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/traffickit/traffickit/internal/config"
)

// Set at build time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const usage = `TrafficKit - HTTP debugging proxy

Usage:
  traffickit <command> [flags]

Commands:
  proxy     Run the proxy in the terminal and print each exchange
  mcp       Serve captured traffic to AI assistants (Model Context Protocol)
  serve     Run the engine and control API (used by the desktop app)
  version   Print the version

Run 'traffickit <command> -h' for the flags a command takes.
Configuration: %s
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage, config.DefaultPath())
		os.Exit(2)
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "proxy":
		err = runProxy(args)
	case "serve":
		err = runServe(args)
	case "mcp":
		err = runMCP(args)
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Printf(usage, config.DefaultPath())
	default:
		fmt.Fprintf(os.Stderr, "traffickit: unknown command %q\n\n", cmd)
		fmt.Fprintf(os.Stderr, usage, config.DefaultPath())
		os.Exit(2)
	}
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "traffickit:", err)
		os.Exit(1)
	}
}

// commonFlags are the settings every long-running command accepts. Flags win
// over environment variables, which win over the config file.
type commonFlags struct {
	configPath string
	bind       string
	port       int
	logLevel   string
}

func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.configPath, "config", "", "config file (default "+config.DefaultPath()+" if present)")
	fs.StringVar(&c.bind, "bind", "", "proxy listen address (default 127.0.0.1)")
	fs.IntVar(&c.port, "port", 0, "proxy port (default 8877)")
	fs.StringVar(&c.logLevel, "log-level", "", "debug, info, warn or error")
}

func (c *commonFlags) load() (config.Config, error) {
	cfg, err := config.Load(c.configPath)
	if err != nil {
		return cfg, err
	}
	if c.bind != "" {
		cfg.Proxy.Bind = c.bind
	}
	if c.port != 0 {
		cfg.Proxy.Port = c.port
	}
	if c.logLevel != "" {
		cfg.Log.Level = c.logLevel
	}
	return cfg, cfg.Validate()
}

func newLogger(cfg config.Log, w io.Writer) *slog.Logger {
	var level slog.Level
	level.UnmarshalText([]byte(cfg.Level)) // validated already
	opts := &slog.HandlerOptions{Level: level}
	if cfg.Format == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }
