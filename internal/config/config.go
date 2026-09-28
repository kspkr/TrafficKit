// Package config loads engine settings from defaults, an optional JSON file
// and TRAFFICKIT_* environment variables, in that order.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	// DataDir holds the CA and launcher profiles. Defaults to the directory
	// the config file lives in.
	DataDir string  `json:"dataDir"`
	Proxy   Proxy   `json:"proxy"`
	HTTPS   HTTPS   `json:"https"`
	Capture Capture `json:"capture"`
	Log     Log     `json:"log"`
}

type HTTPS struct {
	// Intercept decrypts CONNECT tunnels with the local CA. Clients that
	// don't trust the CA will fail, and show up as tls_untrusted errors.
	Intercept bool `json:"intercept"`
	// Passthrough hosts are always relayed without decryption. Entries are
	// exact hostnames or "*.example.com" for any subdomain.
	Passthrough []string `json:"passthrough"`
}

type Proxy struct {
	Bind string `json:"bind"`
	Port int    `json:"port"`
	// AllowRemote permits binding to non-loopback addresses. Off by default
	// because it exposes the proxy, and whatever it captures, to the network.
	AllowRemote bool `json:"allowRemote"`
	AutoStart   bool `json:"autoStart"`
}

type Capture struct {
	MaxBodyBytes int64 `json:"maxBodyBytes"`
	MaxExchanges int   `json:"maxExchanges"`
}

type Log struct {
	Level  string `json:"level"`  // debug, info, warn, error
	Format string `json:"format"` // json, text
}

func Default() Config {
	return Config{
		DataDir: filepath.Dir(DefaultPath()),
		Proxy:   Proxy{Bind: "127.0.0.1", Port: 8877, AutoStart: true},
		HTTPS:   HTTPS{Intercept: true},
		Capture: Capture{MaxBodyBytes: 4 << 20, MaxExchanges: 50_000},
		Log:     Log{Level: "info", Format: "json"},
	}
}

// DefaultPath is where Load looks when no path is given, e.g.
// %AppData%\TrafficKit\config.json or ~/.config/traffickit/config.json.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	name := "traffickit"
	if os.PathSeparator == '\\' {
		name = "TrafficKit"
	}
	return filepath.Join(dir, name, "config.json")
}

// Load builds a Config. An explicit path must exist; the default path is
// optional.
func Load(path string) (Config, error) {
	cfg := Default()
	explicit := path != ""
	if !explicit {
		path = DefaultPath()
	}
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&cfg); err != nil {
				return cfg, fmt.Errorf("config %s: %w", path, err)
			}
		case explicit || !errors.Is(err, os.ErrNotExist):
			return cfg, fmt.Errorf("config: %w", err)
		}
	}
	if err := cfg.applyEnv(os.LookupEnv); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func (c *Config) applyEnv(lookup func(string) (string, bool)) error {
	str := func(key string, dst *string) {
		if v, ok := lookup(key); ok {
			*dst = v
		}
	}
	num := func(key string, set func(int64)) error {
		v, ok := lookup(key)
		if !ok {
			return nil
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return fmt.Errorf("%s: not a number: %q", key, v)
		}
		set(n)
		return nil
	}
	flag := func(key string, dst *bool) error {
		v, ok := lookup(key)
		if !ok {
			return nil
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s: not a boolean: %q", key, v)
		}
		*dst = b
		return nil
	}

	str("TRAFFICKIT_DATA_DIR", &c.DataDir)
	str("TRAFFICKIT_PROXY_BIND", &c.Proxy.Bind)
	str("TRAFFICKIT_LOG_LEVEL", &c.Log.Level)
	str("TRAFFICKIT_LOG_FORMAT", &c.Log.Format)
	return errors.Join(
		num("TRAFFICKIT_PROXY_PORT", func(n int64) { c.Proxy.Port = int(n) }),
		num("TRAFFICKIT_MAX_BODY_BYTES", func(n int64) { c.Capture.MaxBodyBytes = n }),
		num("TRAFFICKIT_MAX_EXCHANGES", func(n int64) { c.Capture.MaxExchanges = int(n) }),
		flag("TRAFFICKIT_PROXY_ALLOW_REMOTE", &c.Proxy.AllowRemote),
		flag("TRAFFICKIT_PROXY_AUTOSTART", &c.Proxy.AutoStart),
		flag("TRAFFICKIT_HTTPS_INTERCEPT", &c.HTTPS.Intercept),
	)
}

func (c *Config) Validate() error {
	var errs []error
	if c.DataDir == "" || c.DataDir == "." {
		errs = append(errs, errors.New("dataDir is not set and no user config directory was found"))
	}
	for _, h := range c.HTTPS.Passthrough {
		if err := CheckHostPattern(h); err != nil {
			errs = append(errs, err)
		}
	}
	if err := CheckBind(c.Proxy.Bind, c.Proxy.AllowRemote); err != nil {
		errs = append(errs, err)
	}
	if c.Proxy.Port < 1 || c.Proxy.Port > 65535 {
		errs = append(errs, fmt.Errorf("proxy.port %d is outside 1-65535", c.Proxy.Port))
	}
	if c.Capture.MaxBodyBytes < 0 {
		errs = append(errs, errors.New("capture.maxBodyBytes cannot be negative"))
	}
	if c.Capture.MaxExchanges < 1 {
		errs = append(errs, errors.New("capture.maxExchanges must be at least 1"))
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("log.level %q is not one of debug, info, warn, error", c.Log.Level))
	}
	switch c.Log.Format {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("log.format %q is not json or text", c.Log.Format))
	}
	return errors.Join(errs...)
}

var (
	ErrInvalidBind  = errors.New("bind address must be an IP address or localhost")
	ErrRemoteDenied = errors.New("binding to a non-loopback address requires proxy.allowRemote")
)

// CheckBind validates a listen address for the proxy.
func CheckBind(bind string, allowRemote bool) error {
	if bind == "localhost" {
		return nil
	}
	ip := net.ParseIP(bind)
	if ip == nil {
		return fmt.Errorf("%w: %q", ErrInvalidBind, bind)
	}
	if !ip.IsLoopback() && !allowRemote {
		return fmt.Errorf("%w (got %s)", ErrRemoteDenied, bind)
	}
	return nil
}

// CheckHostPattern validates a passthrough entry: "example.com" or
// "*.example.com".
func CheckHostPattern(p string) error {
	h := strings.TrimPrefix(p, "*.")
	if h == "" || strings.ContainsAny(h, " /:*?#@") {
		return fmt.Errorf("%q is not a hostname or *.domain pattern", p)
	}
	return nil
}

// MatchHost reports whether host matches any of the patterns.
func MatchHost(patterns []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, p := range patterns {
		p = strings.ToLower(p)
		if suffix, ok := strings.CutPrefix(p, "*."); ok {
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if host == p {
			return true
		}
	}
	return false
}
