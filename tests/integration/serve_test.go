// Package integration runs the real traffickit binary the way the desktop
// shell does and drives it over its API and proxy port.
package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "traffickit-it")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "traffickit")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../../cmd/traffickit")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		panic("building traffickit: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type ready struct {
	API   string `json:"api"`
	Token string `json:"token"`
	Proxy struct {
		Running bool   `json:"running"`
		Address string `json:"address"`
	} `json:"proxy"`
}

type engineProc struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	ready ready
	env   []string // environment, so other commands find the same data dir
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startEngine(t *testing.T) *engineProc {
	t.Helper()
	cmd := exec.Command(binary, "serve", "--exit-on-stdin-close", "--port", fmt.Sprint(freePort(t)))
	// Keep a user's real config file out of the test.
	home := t.TempDir()
	cmd.Env = append(os.Environ(), "APPDATA="+home, "XDG_CONFIG_HOME="+home, "HOME="+home)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &engineProc{cmd: cmd, stdin: stdin, env: cmd.Env}
	t.Cleanup(func() {
		stdin.Close()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			t.Error("engine did not exit after stdin closed")
		}
	})

	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(stdout).ReadString('\n')
		line <- l
		io.Copy(io.Discard, stdout)
	}()
	select {
	case l := <-line:
		if err := json.Unmarshal([]byte(l), &p.ready); err != nil {
			t.Fatalf("ready line %q: %v", l, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no ready line")
	}
	if !p.ready.Proxy.Running || p.ready.Token == "" {
		t.Fatalf("ready = %+v", p.ready)
	}
	return p
}

func (p *engineProc) get(t *testing.T, path string, v any) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", p.ready.API+path, nil)
	req.Header.Set("Authorization", "Bearer "+p.ready.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	return resp
}

func TestCaptureThroughEngine(t *testing.T) {
	e := startEngine(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, `{"echo":%q}`, b)
	}))
	defer origin.Close()

	proxyURL, _ := url.Parse("http://" + e.ready.Proxy.Address)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Post(origin.URL+"/api/items?x=1", "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	var list struct {
		Items []struct {
			ID     uint64 `json:"id"`
			State  string `json:"state"`
			Method string `json:"method"`
			Path   string `json:"path"`
			Status int    `json:"status"`
		} `json:"items"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.get(t, "/v1/exchanges", &list)
		if len(list.Items) == 1 && list.Items[0].State == "complete" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("exchange not captured: %+v", list.Items)
		}
		time.Sleep(20 * time.Millisecond)
	}
	it := list.Items[0]
	if it.Method != "POST" || it.Path != "/api/items?x=1" || it.Status != 200 {
		t.Fatalf("summary = %+v", it)
	}

	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/v1/exchanges/%d/body/request", e.ready.API, it.ID), nil)
	req.Header.Set("Authorization", "Bearer "+e.ready.Token)
	bresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(bresp.Body)
	bresp.Body.Close()
	if string(body) != "hello" {
		t.Fatalf("request body = %q", body)
	}
}

func TestAPIRejectsMissingToken(t *testing.T) {
	e := startEngine(t)
	resp, err := http.Get(e.ready.API + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestAPIListensOnLoopbackOnly(t *testing.T) {
	e := startEngine(t)
	u, _ := url.Parse(e.ready.API)
	if host, _, _ := net.SplitHostPort(u.Host); host != "127.0.0.1" {
		t.Fatalf("API bound to %s", host)
	}
	if host, _, _ := net.SplitHostPort(e.ready.Proxy.Address); host != "127.0.0.1" {
		t.Fatalf("proxy bound to %s by default", host)
	}
}
