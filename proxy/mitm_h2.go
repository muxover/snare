package proxy

import (
	"bytes"
	"crypto/tls"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/intercept"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/http2"
)

type mitmH2Handler struct {
	hostname  string
	store     *capture.Store
	log       *slog.Logger
	parent    *Handler
	transport *http.Transport
}

func (m *mitmH2Handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	req.URL.Scheme = "https"
	req.URL.Host = m.hostname

	if m.parent.Mocks != nil {
		if rule := m.parent.Mocks.Match(req); rule != nil {
			m.parent.serveMock(rw, req, rule)
			return
		}
	}

	start := time.Now()
	capID := uuid.New().String()

	bodyBuf, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(bodyBuf))

	if m.parent.Intercept != nil && intercept.MatchesPattern(req, m.parent.InterceptMatch) {
		newBody, dropped := m.parent.holdAndApply(req, capID, start, req.URL.String(), bodyBuf)
		if dropped {
			http.Error(rw, "request dropped by intercept", http.StatusBadGateway)
			return
		}
		bodyBuf = newBody
	}

	outReq, _ := http.NewRequest(req.Method, req.URL.String(), bytes.NewReader(bodyBuf))
	outReq.Header = req.Header.Clone()
	outReq.Host = m.hostname
	m.parent.applyOutboundMods(outReq)

	resp, err := m.transport.RoundTrip(outReq)
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
	handler := &mitmH2Handler{
		hostname:  hostname,
		store:     h.Store,
		log:       h.Log,
		parent:    h,
		transport: h.Transport,
	}
	srv.ServeConn(clientConn, &http2.ServeConnOpts{Handler: handler})
}
