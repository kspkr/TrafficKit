package launch

import (
	"os"
	"path/filepath"
	"testing"
)

// Run by hand: TK_E2E_TERMINAL=1 go test -run TestE2ETerminal ./internal/launch
// Opens a real console window that makes HTTPS requests through a running
// TrafficKit on 127.0.0.1:8877 and exits.
func TestE2ETerminal(t *testing.T) {
	if os.Getenv("TK_E2E_TERMINAL") == "" {
		t.Skip("set TK_E2E_TERMINAL=1 with TrafficKit running")
	}
	dataDir := filepath.Join(os.Getenv("APPDATA"), "TrafficKit")
	env := Env{ProxyAddr: "127.0.0.1:8877", CAPath: filepath.Join(dataDir, "ca", "ca.crt"), DataDir: dataDir}
	bundle, err := writeBundle(env)
	if err != nil {
		t.Fatal(err)
	}
	vars := TerminalEnv(env, bundle)
	home, err := writeCurlConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	vars["CURL_HOME"] = home
	script := `curl.exe -s -o NUL 'https://httpbin.org/get?tool=terminal-curl'; ` +
		`node -e "fetch('https://httpbin.org/get?tool=terminal-node').then(r=>r.text())"; ` +
		`python -c "import urllib.request as u; u.urlopen('https://httpbin.org/get?tool=terminal-python').read()"`
	p, err := startConsole([]string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, "-NoLogo", "-Command", script}, mergeEnv(vars))
	if err != nil {
		t.Fatal(err)
	}
	st, err := p.Wait()
	if err != nil || !st.Success() {
		t.Fatalf("terminal exited: %v %v", st, err)
	}
}
