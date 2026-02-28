package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// HostCertCache caches per-host certificates.
type HostCertCache struct {
	mu    sync.RWMutex
	cache map[string]*cachedCert
	ca    *x509.Certificate
	key   *ecdsa.PrivateKey
}

type cachedCert struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// NewHostCertCache creates a cache that issues certs signed by the given CA.
func NewHostCertCache(ca *x509.Certificate, key *ecdsa.PrivateKey) *HostCertCache {
	return &HostCertCache{
		cache: make(map[string]*cachedCert),
		ca:    ca,
		key:   key,
	}
}

// GetCertificate returns a TLS certificate for the given host.
func (h *HostCertCache) GetCertificate(host string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	host = normalizeHost(host)
	h.mu.RLock()
	if c, ok := h.cache[host]; ok {
		h.mu.RUnlock()
		return c.cert, c.key, nil
	}
	h.mu.RUnlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.cache[host]; ok {
		return c.cert, c.key, nil
	}
	cert, key, err := h.issue(host)
	if err != nil {
		return nil, nil, err
	}
	h.cache[host] = &cachedCert{cert: cert, key: key}
	return cert, key, nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return strings.ToLower(host)
}

func (h *HostCertCache) issue(host string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Proxy"},
			CommonName:   host,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{host},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, h.ca, &key.PublicKey, h.key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}
