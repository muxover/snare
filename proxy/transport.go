package proxy

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/http2"
)

func ProxyTransport(skipVerify bool, upstreamProxy string) (*http.Transport, error) {
	proxyFunc := http.ProxyFromEnvironment
	if upstreamProxy != "" {
		u, err := url.Parse(upstreamProxy)
		if err != nil {
			return nil, fmt.Errorf("invalid --upstream-proxy: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("invalid --upstream-proxy scheme: %q (use http or https)", u.Scheme)
		}
		if u.Host == "" {
			return nil, fmt.Errorf("invalid --upstream-proxy: missing host")
		}
		proxyFunc = http.ProxyURL(u)
	}
	t := &http.Transport{
		Proxy:                 proxyFunc,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if skipVerify {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	_ = http2.ConfigureTransport(t)
	return t, nil
}
