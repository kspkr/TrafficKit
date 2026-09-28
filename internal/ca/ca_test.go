package ca

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadCreatesAndReuses(t *testing.T) {
	dir := t.TempDir()
	a, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := a.Info()
	if !a.cert.IsCA || first.SPKI == "" || first.SHA256 == "" {
		t.Fatalf("bad CA: %+v", first)
	}

	b, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Info().SHA256 != first.SHA256 {
		t.Fatal("second Load generated a new CA instead of reusing the stored one")
	}
}

func TestKeyFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses ACLs; permission bits aren't meaningful")
	}
	dir := t.TempDir()
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v", st.Mode().Perm())
	}
}

func TestLeafVerifiesAgainstCA(t *testing.T) {
	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"example.com", "127.0.0.1", "::1"} {
		c, err := a.CertFor(host)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: a.Pool()})
		if err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
}

func TestLeafCache(t *testing.T) {
	a, _ := Load(t.TempDir())
	c1, _ := a.CertFor("Example.COM.")
	c2, _ := a.CertFor("example.com")
	if c1 != c2 {
		t.Fatal("same host issued twice")
	}
}

func TestRegenerateInvalidatesLeaves(t *testing.T) {
	a, _ := Load(t.TempDir())
	old := a.Info()
	leaf, _ := a.CertFor("example.com")
	if err := a.Regenerate(); err != nil {
		t.Fatal(err)
	}
	if a.Info().SHA256 == old.SHA256 {
		t.Fatal("CA unchanged")
	}
	fresh, _ := a.CertFor("example.com")
	if fresh == leaf {
		t.Fatal("leaf from the old CA still cached")
	}
	if _, err := fresh.Leaf.Verify(x509.VerifyOptions{DNSName: "example.com", Roots: a.Pool()}); err != nil {
		t.Fatal(err)
	}
}

func TestTLSHandshakeWithIssuedCert(t *testing.T) {
	a, _ := Load(t.TempDir())
	server, client := net.Pipe()
	go func() {
		s := tls.Server(server, &tls.Config{GetCertificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return a.CertFor(h.ServerName)
		}})
		s.Handshake()
		s.Close()
	}()
	c := tls.Client(client, &tls.Config{ServerName: "api.test", RootCAs: a.Pool()})
	if err := c.Handshake(); err != nil {
		t.Fatal(err)
	}
	c.Close()
}
