package proxy

import (
	"bytes"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"github.com/muxover/snare/capture"
	"strconv"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/http2"
)

// mitmH2Handler is the http.Handler used when client uses HTTP/2 after CONNECT.
type mitmH2Handler struct {
	hostname string
	origin   net.Conn
	store    *capture.Store
	log      *slog.Logger
}

func (m *mitmH2Handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	req.URL.Scheme = "https"
	req.URL.Host = m.hostname
	start := time.Now()
	capID := uuid.New().String()

	bodyBuf, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(bodyBuf))

	outReq, _ := http.NewRequest(req.Method, req.URL.String(), bytes.NewReader(bodyBuf))
	outReq.Header = req.Header.Clone()
	outReq.Host = m.hostname

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	_ = http2.ConfigureTransport(tr)
	client := &http.Client{Transport: tr}
	resp, err := client.Do(outReq)
	if err != nil {
		m.store.Add(&capture.Capture{ID: capID, Timestamp: start, Error: err.Error()})
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	duration := time.Since(start)

	reqCaptureBody := decompressBody(bodyBuf, req.Header.Get("Content-Encoding"))
	reqHeaders := req.Header.Clone()
	if len(reqCaptureBody) != len(bodyBuf) {
		reqHeaders.Del("Content-Encoding")
		reqHeaders.Set("Content-Length", strconv.Itoa(len(reqCaptureBody)))
	}
	captureBody := decompressBody(respBody, resp.Header.Get("Content-Encoding"))
	respHeaders := resp.Header.Clone()
	if len(captureBody) != len(respBody) {
		respHeaders.Del("Content-Encoding")
		respHeaders.Set("Content-Length", strconv.Itoa(len(captureBody)))
	}
	m.store.Add(&capture.Capture{
		ID:        capID,
		Timestamp: start,
		Protocol:  "h2",
		Request: capture.RequestSnapshot{
			Method:  req.Method,
			URL:     req.URL.String(),
			Headers: reqHeaders,
			Body:    capture.BodyBytes(reqCaptureBody),
		},
		Response: &capture.ResponseSnapshot{
			StatusCode: resp.StatusCode,
			Headers:    respHeaders,
			Body:       capture.BodyBytes(captureBody),
		},
		Duration: duration,
	})

	for k, v := range resp.Header {
		for _, vv := range v {
			rw.Header().Add(k, vv)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	_, _ = rw.Write(respBody)
}

func (h *Handler) mitmHTTP2(clientConn net.Conn, originConn *tls.Conn, hostname string, _ *http.Request) {
	srv := &http2.Server{}
	handler := &mitmH2Handler{hostname: hostname, origin: originConn, store: h.Store, log: h.Log}
	srv.ServeConn(clientConn, &http2.ServeConnOpts{Handler: handler})
}
