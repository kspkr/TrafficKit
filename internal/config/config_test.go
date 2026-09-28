package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFileAndEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(path, []byte(`{"proxy":{"port":9001},"capture":{"maxExchanges":10}}`), 0o600)
	t.Setenv("TRAFFICKIT_PROXY_PORT", "9002")
	t.Setenv("TRAFFICKIT_LOG_LEVEL", "debug")

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Proxy.Port != 9002 {
		t.Errorf("env should override file: port = %d", c.Proxy.Port)
	}
	if c.Capture.MaxExchanges != 10 {
		t.Errorf("file value lost: maxExchanges = %d", c.Capture.MaxExchanges)
	}
	if c.Capture.MaxBodyBytes != Default().Capture.MaxBodyBytes {
		t.Errorf("default lost: maxBodyBytes = %d", c.Capture.MaxBodyBytes)
	}
	if c.Log.Level != "debug" {
		t.Errorf("log level = %q", c.Log.Level)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(path, []byte(`{"proxy":{"prot":9001}}`), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("typo in config was accepted")
	}
}

func TestLoadMissingExplicitFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("missing explicit config should fail")
	}
}

func TestBadEnv(t *testing.T) {
	env := map[string]string{"TRAFFICKIT_PROXY_PORT": "eighty", "TRAFFICKIT_PROXY_AUTOSTART": "maybe"}
	c := Default()
	err := c.applyEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckBind(t *testing.T) {
	cases := []struct {
		bind   string
		remote bool
		want   error
	}{
		{"127.0.0.1", false, nil},
		{"::1", false, nil},
		{"localhost", false, nil},
		{"0.0.0.0", false, ErrRemoteDenied},
		{"0.0.0.0", true, nil},
		{"192.168.1.5", false, ErrRemoteDenied},
		{"example.com", true, ErrInvalidBind},
		{"127.0.0.1; rm -rf /", true, ErrInvalidBind},
	}
	for _, tc := range cases {
		err := CheckBind(tc.bind, tc.remote)
		if !errors.Is(err, tc.want) && !(err == nil && tc.want == nil) {
			t.Errorf("CheckBind(%q, %v) = %v, want %v", tc.bind, tc.remote, err, tc.want)
		}
	}
}

func TestHostPatterns(t *testing.T) {
	pats := []string{"api.example.com", "*.bank.test"}
	for host, want := range map[string]bool{
		"api.example.com":  true,
		"API.Example.com.": true,
		"example.com":      false,
		"x.bank.test":      true,
		"a.b.bank.test":    true,
		"bank.test":        false,
	} {
		if got := MatchHost(pats, host); got != want {
			t.Errorf("MatchHost(%q) = %v", host, got)
		}
	}
	for _, bad := range []string{"", "*.", "http://x.com", "a b", "*.x*.com"} {
		if CheckHostPattern(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
