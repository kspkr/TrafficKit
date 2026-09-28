package proxy

import (
	"html/template"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// LocalHost is a name the proxy answers itself instead of forwarding.
// Launched browsers open https://traffickit.test/ first: if that page loads
// over HTTPS, interception works in that browser. .test is reserved and
// never resolves on the real internet.
const LocalHost = "traffickit.test"

func isLocalHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	return strings.EqualFold(host, LocalHost)
}

var startPage = template.Must(template.New("start").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Connected to TrafficKit</title>
<style>
  :root { color-scheme: light dark; --accent: #e5a445; }
  body { font: 15px/1.5 system-ui, sans-serif; max-width: 560px; margin: 12vh auto; padding: 0 20px; }
  h1 { font-size: 22px; margin: 0 0 6px; }
  .row { display: flex; gap: 10px; padding: 10px 0; border-bottom: 1px solid color-mix(in srgb, currentColor 15%, transparent); }
  .ok { color: #3fae6d; font-weight: 600; }
  .warn { color: #c9861b; font-weight: 600; }
  code { font: 13px ui-monospace, Consolas, monospace; }
  a { color: var(--accent); }
  .mark { width: 28px; height: 4px; background: var(--accent); border-radius: 2px; margin-bottom: 18px; }
</style>
</head>
<body>
  <div class="mark"></div>
  <h1>This window is connected to TrafficKit</h1>
  <p>Everything you open here shows up in TrafficKit as it happens. The window has its own
  temporary profile, so your usual logins and history aren't in it.</p>
  <div class="row"><span>Proxy</span><code>{{.Proxy}}</code></div>
  {{if .HTTPS}}
  <div class="row"><span>HTTPS</span><span class="ok">Intercepted: this page itself came over HTTPS through TrafficKit.</span></div>
  {{else}}
  <div class="row"><span>HTTPS</span><span class="warn">Not verified yet. <a href="https://{{.Host}}/">Check HTTPS</a></span></div>
  {{end}}
  <p>Try any site, e.g. <a href="https://example.com/">example.com</a>.</p>
  <p style="font-size:13px;opacity:.7">Setting up a device by hand? Download the
  <a href="/certificate">TrafficKit CA certificate</a>.</p>
</body>
</html>`))

// serveLocal answers requests addressed to the proxy itself: the start page
// on LocalHost, the CA certificate, and a hint for anyone who pasted the
// proxy address into a browser.
func (p *Proxy) serveLocal(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")

	if r.URL.Path == "/certificate" && p.ca != nil {
		h.Set("Content-Type", "application/x-x509-ca-cert")
		h.Set("Content-Disposition", `attachment; filename="traffickit-ca.crt"`)
		w.Write(p.ca.CertPEM())
		return
	}
	if isLocalHost(r.Host) || isLocalHost(r.URL.Host) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		proxyAddr := ""
		if port := p.selfPort.Load(); port != 0 {
			proxyAddr = "port " + strconv.FormatInt(port, 10)
		}
		startPage.Execute(w, map[string]any{
			"HTTPS": r.URL.Scheme == "https",
			"Host":  LocalHost,
			"Proxy": proxyAddr,
		})
		return
	}

	h.Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Path != "/" {
		w.WriteHeader(http.StatusNotFound)
	}
	io.WriteString(w, "This is a TrafficKit proxy port. Configure your client to use it as an HTTP proxy "+
		"rather than requesting it directly, e.g.\n\n    curl -x http://"+r.Host+" http://example.com/\n\n"+
		"The CA certificate for HTTPS interception is at http://"+r.Host+"/certificate\n")
}
