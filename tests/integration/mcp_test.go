package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPOverStdio wires things up the way a user does: the app's engine is
// running, and an assistant spawns `traffickit mcp`, which finds the engine
// through the discovery file.
func TestMCPOverStdio(t *testing.T) {
	e := startEngine(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello from origin")
	}))
	defer origin.Close()
	proxyURL, _ := url.Parse("http://" + e.ready.Proxy.Address)
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Get(origin.URL + "/checkout?step=2")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	cmd := exec.Command(binary, "mcp")
	cmd.Env = e.env
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "integration-test"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_traffic", Arguments: map[string]any{"filter": "checkout"}})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res.StructuredContent)
		if res.IsError {
			t.Fatalf("list_traffic failed: %+v", res.Content)
		}
		if strings.Contains(string(b), "/checkout?step=2") && strings.Contains(string(b), `"status":200`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("exchange never showed up: %s", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
