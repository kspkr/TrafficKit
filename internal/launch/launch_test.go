package launch

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// When the test binary is run as a fake browser, it writes its arguments to
// a file and waits to be killed.
func TestMain(m *testing.M) {
	if out := os.Getenv("TK_FAKE_BROWSER_OUT"); out != "" {
		os.WriteFile(out, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestBrowserArgs(t *testing.T) {
	args := BrowserArgs("/tmp/p", Env{ProxyAddr: "127.0.0.1:8877", SPKI: "abc="}, "")
	for _, want := range []string{
		"--user-data-dir=/tmp/p",
		"--proxy-server=http://127.0.0.1:8877",
		"--proxy-bypass-list=<-loopback>",
		"--ignore-certificate-errors-spki-list=abc=",
		StartURL,
	} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %q in %v", want, args)
		}
	}
	if args[len(args)-1] != StartURL {
		t.Error("URL must come last")
	}
}

func TestTerminalEnv(t *testing.T) {
	vars := TerminalEnv(Env{ProxyAddr: "127.0.0.1:1", CAPath: "/d/ca.crt"}, "/d/bundle.pem")
	if vars["HTTPS_PROXY"] != "http://127.0.0.1:1" || vars["https_proxy"] != vars["HTTPS_PROXY"] {
		t.Error("proxy variables not set")
	}
	if vars["NODE_EXTRA_CA_CERTS"] != "/d/ca.crt" || vars["SSL_CERT_FILE"] != "/d/bundle.pem" {
		t.Error("certificate variables not set")
	}
}

func TestMergeEnvOverrides(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://old")
	env := mergeEnv(map[string]string{"HTTPS_PROXY": "http://new"})
	var found []string
	for _, kv := range env {
		if strings.HasPrefix(strings.ToUpper(kv), "HTTPS_PROXY=") {
			found = append(found, kv)
		}
	}
	if len(found) != 1 || found[0] != "HTTPS_PROXY=http://new" {
		t.Fatalf("got %v", found)
	}
}

func TestWriteBundleIncludesCA(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "ca"), 0o700)
	caPath := filepath.Join(dir, "ca", "ca.crt")
	os.WriteFile(caPath, []byte("-----BEGIN CERTIFICATE-----\nX\n-----END CERTIFICATE-----\n"), 0o644)
	path, err := writeBundle(Env{CAPath: caPath, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), "-----BEGIN CERTIFICATE-----\nX") {
		t.Fatalf("bundle = %.60q", b)
	}
}

func TestLaunchTrackStop(t *testing.T) {
	dataDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("TK_FAKE_BROWSER_OUT", argsFile)

	var mu sync.Mutex
	var changes [][]Source
	m := NewManager(slog.New(slog.DiscardHandler), func(s []Source) {
		mu.Lock()
		changes = append(changes, s)
		mu.Unlock()
	})
	exe, _ := os.Executable()
	m.detect = func() []Target {
		return []Target{{ID: "fake", Kind: "browser", Name: "Fake", Path: exe}}
	}

	if _, err := m.Launch("nope", Env{}, ""); err == nil {
		t.Fatal("unknown target accepted")
	}
	src, err := m.Launch("fake", Env{ProxyAddr: "127.0.0.1:9", SPKI: "k", DataDir: dataDir}, "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if !src.Tracked || src.PID == 0 || len(m.Active()) != 1 {
		t.Fatalf("source = %+v, active = %d", src, len(m.Active()))
	}

	waitFor(t, func() bool { _, err := os.Stat(argsFile); return err == nil })
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--proxy-server=http://127.0.0.1:9") || !strings.HasSuffix(string(args), "http://example.com/") {
		t.Fatalf("args = %s", args)
	}
	profiles, _ := os.ReadDir(filepath.Join(dataDir, "profiles"))
	if len(profiles) != 1 {
		t.Fatalf("profiles = %d", len(profiles))
	}

	if err := m.Stop(src.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(m.Active()) == 0 })
	waitFor(t, func() bool {
		profiles, _ := os.ReadDir(filepath.Join(dataDir, "profiles"))
		return len(profiles) == 0
	})
	mu.Lock()
	defer mu.Unlock()
	if len(changes) < 2 {
		t.Fatalf("onChange called %d times", len(changes))
	}
}

func TestDetectFindsSomethingPlausible(t *testing.T) {
	for _, target := range Detect() {
		if target.ID == "" || target.Name == "" || target.Path == "" {
			t.Errorf("incomplete target %+v", target)
		}
		if target.Kind == "browser" && runtime.GOOS != "linux" && !isFile(target.Path) {
			t.Errorf("%s: %s is not a file", target.ID, target.Path)
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
