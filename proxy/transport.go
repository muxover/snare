package proxy

import (
	"crypto/tls"
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

// ProxyTransport returns a transport for forwarding (no proxy, skip TLS verify).
func ProxyTransport(skipVerify bool) *http.Transport {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if skipVerify {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	_ = http2.ConfigureTransport(t)
	return t
}
