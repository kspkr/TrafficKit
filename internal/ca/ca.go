// Package ca manages TrafficKit's local certificate authority and the
// per-host certificates it issues for HTTPS interception.
//
// The CA key never leaves the data directory. Nothing here installs the CA
// into any trust store; browsers launched by TrafficKit trust it through a
// command-line flag scoped to that browser instance.
package ca

import (
	"container/list"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	certFile = "ca.crt"
	keyFile  = "ca.key"

	caValidity   = 365 * 24 * time.Hour
	leafValidity = 30 * 24 * time.Hour
	cacheSize    = 1024
)

// Authority issues leaf certificates signed by the local CA.
type Authority struct {
	dir string

	mu    sync.Mutex
	cert  *x509.Certificate
	key   crypto.Signer
	cache *leafCache
}

// Info is what the UI shows about the CA. None of it is secret.
type Info struct {
	Subject   string    `json:"subject"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	SHA256    string    `json:"sha256"` // fingerprint of the whole certificate, hex
	SPKI      string    `json:"spki"`   // base64 SHA-256 of the public key, what Chrome's flag takes
	Path      string    `json:"path"`   // PEM certificate on disk
}

// Load reads the CA from dir, creating a new one if none exists or the
// existing one expires within a week.
func Load(dir string) (*Authority, error) {
	a := &Authority{dir: dir, cache: newLeafCache(cacheSize)}
	err := a.load()
	if errors.Is(err, os.ErrNotExist) || (err == nil && time.Until(a.cert.NotAfter) < 7*24*time.Hour) {
		err = a.generate()
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Authority) load() error {
	certPEM, err := os.ReadFile(filepath.Join(a.dir, certFile))
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(filepath.Join(a.dir, keyFile))
	if err != nil {
		return err
	}
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return fmt.Errorf("ca: %s holds no PEM data", a.dir)
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return fmt.Errorf("ca: certificate: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return fmt.Errorf("ca: key: %w", err)
	}
	key, ok := k.(crypto.Signer)
	if !ok {
		return errors.New("ca: key cannot sign")
	}
	a.cert, a.key = cert, key
	return nil
}

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

// Regenerate replaces the CA with a new one. Anything that trusted the old
// CA (launched browsers, devices) has to be set up again.
func (a *Authority) Regenerate() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.generateLocked()
}

func (a *Authority) generate() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.generateLocked()
}

func (a *Authority) generateLocked() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	sn, err := serial()
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: sn,
		Subject: pkix.Name{
			// Unique per machine and per generation, so it's obvious in a
			// trust store which one this is.
			CommonName:   fmt.Sprintf("TrafficKit Local CA (%s, %s)", host, now.Format("2006-01-02")),
			Organization: []string{"TrafficKit"},
		},
		NotBefore:             now.Add(-24 * time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(a.dir, keyFile), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(a.dir, certFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	a.cert, a.key = cert, key
	a.cache = newLeafCache(cacheSize)
	return nil
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (a *Authority) Info() Info {
	a.mu.Lock()
	defer a.mu.Unlock()
	sum := sha256.Sum256(a.cert.Raw)
	spki := sha256.Sum256(a.cert.RawSubjectPublicKeyInfo)
	return Info{
		Subject:   a.cert.Subject.CommonName,
		NotBefore: a.cert.NotBefore,
		NotAfter:  a.cert.NotAfter,
		SHA256:    strings.ToUpper(hex.EncodeToString(sum[:])),
		SPKI:      base64.StdEncoding.EncodeToString(spki[:]),
		Path:      filepath.Join(a.dir, certFile),
	}
}

// CertPEM returns the CA certificate (not the key) in PEM form.
func (a *Authority) CertPEM() []byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw})
}

// Pool returns a pool containing just the CA, for tests and clients.
func (a *Authority) Pool() *x509.CertPool {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := x509.NewCertPool()
	p.AddCert(a.cert)
	return p
}

// CertFor returns a certificate for host (a DNS name or IP, no port).
func (a *Authority) CertFor(host string) (*tls.Certificate, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return nil, errors.New("ca: empty host")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if c := a.cache.get(host); c != nil && time.Until(c.Leaf.NotAfter) > time.Hour {
		return c, nil
	}
	c, err := a.issueLocked(host)
	if err != nil {
		return nil, err
	}
	a.cache.put(host, c)
	return c, nil
}

func (a *Authority) issueLocked(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	notAfter := now.Add(leafValidity)
	if notAfter.After(a.cert.NotAfter) {
		notAfter = a.cert.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber: sn,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"TrafficKit"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der, a.cert.Raw}, PrivateKey: key, Leaf: leaf}, nil
}

// leafCache is a small LRU keyed by host.
type leafCache struct {
	max   int
	order *list.List
	items map[string]*list.Element
}

type cacheEntry struct {
	host string
	cert *tls.Certificate
}

func newLeafCache(max int) *leafCache {
	return &leafCache{max: max, order: list.New(), items: make(map[string]*list.Element)}
}

func (c *leafCache) get(host string) *tls.Certificate {
	if e, ok := c.items[host]; ok {
		c.order.MoveToFront(e)
		return e.Value.(*cacheEntry).cert
	}
	return nil
}

func (c *leafCache) put(host string, cert *tls.Certificate) {
	if e, ok := c.items[host]; ok {
		e.Value.(*cacheEntry).cert = cert
		c.order.MoveToFront(e)
		return
	}
	c.items[host] = c.order.PushFront(&cacheEntry{host, cert})
	if c.order.Len() > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*cacheEntry).host)
	}
}
