package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// TerminalEnv returns the variables that route common tools through the
// proxy and make them trust the TrafficKit CA.
//
// Coverage is best-effort: curl, Node (22.12+ via NODE_USE_ENV_PROXY),
// Python requests/httpx/pip, git, Deno, Cargo, the AWS CLI. Tools that ignore
// proxy variables, or insist on the OS trust store (Go on Windows and macOS,
// .NET), aren't covered and will show up as tls_untrusted errors.
func TerminalEnv(env Env, bundle string) map[string]string {
	proxy := "http://" + env.ProxyAddr
	vars := map[string]string{
		"HTTP_PROXY":          proxy,
		"HTTPS_PROXY":         proxy,
		"http_proxy":          proxy,
		"https_proxy":         proxy,
		"NODE_USE_ENV_PROXY":  "1",
		"NODE_EXTRA_CA_CERTS": env.CAPath,
		"DENO_CERT":           env.CAPath,
		"SSL_CERT_FILE":       bundle,
		"REQUESTS_CA_BUNDLE":  bundle,
		"CURL_CA_BUNDLE":      bundle,
		"PIP_CERT":            bundle,
		"GIT_SSL_CAINFO":      bundle,
		"AWS_CA_BUNDLE":       bundle,
		"CARGO_HTTP_CAINFO":   bundle,
		"TRAFFICKIT_ACTIVE":   "1",
	}
	if runtime.GOOS == "windows" {
		// Git for Windows defaults to Schannel, which ignores GIT_SSL_CAINFO.
		vars["GIT_CONFIG_COUNT"] = "1"
		vars["GIT_CONFIG_KEY_0"] = "http.sslBackend"
		vars["GIT_CONFIG_VALUE_0"] = "openssl"
	}
	return vars
}

// systemBundles are where Unix-likes keep the root store as a PEM file.
var systemBundles = []string{
	"/etc/ssl/certs/ca-certificates.crt", // Debian, Ubuntu, Arch
	"/etc/pki/tls/certs/ca-bundle.crt",   // Fedora, RHEL
	"/etc/ssl/ca-bundle.pem",             // openSUSE
	"/etc/ssl/cert.pem",                  // macOS, Alpine
}

// writeBundle writes the CA plus the system roots (where they exist as a
// file) so tools that replace their trust store with SSL_CERT_FILE and
// friends can still reach hosts that bypass the proxy.
func writeBundle(env Env) (string, error) {
	ca, err := os.ReadFile(env.CAPath)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.Write(ca)
	for _, p := range systemBundles {
		if roots, err := os.ReadFile(p); err == nil {
			b.WriteString("\n")
			b.Write(roots)
			break
		}
	}
	path := filepath.Join(env.DataDir, "ca", "bundle.pem")
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}

// mergeEnv overlays vars on the current environment, replacing existing
// entries case-insensitively on Windows.
func mergeEnv(vars map[string]string) []string {
	fold := runtime.GOOS == "windows"
	key := func(s string) string {
		if fold {
			return strings.ToUpper(s)
		}
		return s
	}
	override := make(map[string]bool, len(vars))
	for k := range vars {
		override[key(k)] = true
	}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !override[key(k)] {
			out = append(out, kv)
		}
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+vars[k])
	}
	return out
}
