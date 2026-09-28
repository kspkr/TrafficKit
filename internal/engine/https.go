package engine

import (
	"net"
	"net/url"
	"strconv"

	"github.com/traffickit/traffickit/internal/ca"
	"github.com/traffickit/traffickit/internal/config"
	"github.com/traffickit/traffickit/internal/launch"
)

type HTTPSStatus struct {
	Intercept   bool     `json:"intercept"`
	Passthrough []string `json:"passthrough"`
	CA          ca.Info  `json:"ca"`
}

func (e *Engine) shouldIntercept(host string) bool {
	e.httpsMu.RLock()
	defer e.httpsMu.RUnlock()
	return e.https.Intercept && !config.MatchHost(e.https.Passthrough, host)
}

func (e *Engine) HTTPS() HTTPSStatus {
	e.httpsMu.RLock()
	defer e.httpsMu.RUnlock()
	pass := append([]string{}, e.https.Passthrough...)
	return HTTPSStatus{Intercept: e.https.Intercept, Passthrough: pass, CA: e.ca.Info()}
}

func (e *Engine) CA() *ca.Authority { return e.ca }

// SetHTTPS changes interception at runtime. It applies to new connections;
// open tunnels keep whatever they started with.
func (e *Engine) SetHTTPS(intercept bool, passthrough []string) (HTTPSStatus, error) {
	for _, p := range passthrough {
		if err := config.CheckHostPattern(p); err != nil {
			return e.HTTPS(), &Error{Code: "invalid_host", Message: err.Error(), Hint: `Use a hostname like "api.example.com" or "*.example.com".`}
		}
	}
	e.httpsMu.Lock()
	e.https = config.HTTPS{Intercept: intercept, Passthrough: append([]string{}, passthrough...)}
	e.httpsMu.Unlock()
	st := e.HTTPS()
	e.hub.Publish("https", st)
	e.log.Info("https settings changed", "intercept", intercept, "passthrough", len(passthrough))
	return st, nil
}

// RegenerateCA replaces the CA. Browsers launched earlier trust the old one
// and are closed, since every HTTPS request they make would now fail.
func (e *Engine) RegenerateCA() (HTTPSStatus, error) {
	if err := e.ca.Regenerate(); err != nil {
		return e.HTTPS(), &Error{Code: "ca_failed", Message: "Could not create a new certificate authority.", Hint: err.Error(), err: err}
	}
	e.launcher.StopAll()
	st := e.HTTPS()
	e.hub.Publish("https", st)
	e.log.Info("certificate authority regenerated")
	return st, nil
}

func (e *Engine) Targets() []launch.Target { return e.launcher.Available() }
func (e *Engine) Sources() []launch.Source { return e.launcher.Active() }

// Launch starts a browser or terminal wired to the running proxy. startURL
// is optional and must be an absolute http(s) URL; it ends up on a command
// line, so nothing else is allowed through.
func (e *Engine) Launch(target, startURL string) (launch.Source, error) {
	if startURL != "" {
		u, err := url.Parse(startURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return launch.Source{}, &Error{Code: "invalid_url", Message: "Only http and https URLs can be opened.", Hint: "Enter a full address such as https://example.com/."}
		}
		startURL = u.String()
	}
	st := e.ProxyStatus()
	if !st.Running {
		return launch.Source{}, &Error{Code: "proxy_stopped", Message: "The proxy isn't running.", Hint: "Start the proxy first; launched apps send their traffic to it."}
	}
	host := st.Bind
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1" // listening everywhere; connect over loopback
	}
	info := e.ca.Info()
	src, err := e.launcher.Launch(target, launch.Env{
		ProxyAddr: net.JoinHostPort(host, strconv.Itoa(st.Port)),
		CAPath:    info.Path,
		SPKI:      info.SPKI,
		DataDir:   e.cfg.DataDir,
	}, startURL)
	if err != nil {
		return src, &Error{Code: "launch_failed", Message: "Could not launch " + target + ".", Hint: err.Error(), err: err}
	}
	return src, nil
}

func (e *Engine) StopSource(id string) error {
	return e.launcher.Stop(id)
}
