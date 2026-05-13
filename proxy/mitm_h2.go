package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/intercept"
	"golang.org/x/net/http2"
)

type mitmH2Handler struct {
	hostname  string
	parent    *Handler
	transport *http.Transport
}

func (m *mitmH2Handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	req.URL.Scheme = "https"
	req.URL.Host = m.hostname

	if isH2ExtendedWebSocket(req) {
		m.serveH2WebSocket(rw, req)
		return
	}

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
		req.Body = io.NopCloser(bytes.NewReader(bodyBuf))
	}

	outReq, _ := http.NewRequest(req.Method, req.URL.String(), bytes.NewReader(bodyBuf))
	outReq.Header = req.Header.Clone()
	outReq.Host = m.hostname
	m.parent.applyOutboundMods(outReq)

	resp, err := m.transport.RoundTrip(outReq)
	if err != nil {
		m.parent.addCapture(&capture.Capture{ID: capID, Timestamp: start, Error: err.Error()})
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
	m.parent.addCapture(&capture.Capture{
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

func (m *mitmH2Handler) serveH2WebSocket(rw http.ResponseWriter, req *http.Request) {
	start := time.Now()
	capID := uuid.New().String()
	var bodyBuf []byte

	if m.parent.Intercept != nil && intercept.MatchesPattern(req, m.parent.InterceptMatch) {
		newBody, dropped := m.parent.holdAndApply(req, capID, start, req.URL.String(), bodyBuf)
		if dropped {
			http.Error(rw, "request dropped by intercept", http.StatusBadGateway)
			return
		}
		bodyBuf = newBody
	}

	addr := net.JoinHostPort(m.hostname, "443")
	oc, err := tls.Dial("tcp", addr, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"http/1.1"},
	})
	if err != nil {
		m.parent.addCapture(&capture.Capture{ID: capID, Timestamp: start, Error: err.Error()})
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer oc.Close()

	ou, err := http.NewRequest(http.MethodGet, req.URL.String(), nil)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	copyWebSocketClientHeaders(req, ou)
	ou.Host = m.hostname
	m.parent.applyOutboundMods(ou)
	if err := ou.Write(oc); err != nil {
		m.parent.addCapture(&capture.Capture{ID: capID, Timestamp: start, Error: err.Error()})
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	if len(bodyBuf) > 0 {
		_, _ = oc.Write(bodyBuf)
	}

	originBR := bufio.NewReader(oc)
	resp, err := http.ReadResponse(originBR, ou)
	if err != nil || !isWebSocketAcceptResponse(resp) {
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		m.parent.addCapture(&capture.Capture{ID: capID, Timestamp: start, Error: "upstream websocket handshake failed"})
		http.Error(rw, "upstream websocket handshake failed", http.StatusBadGateway)
		return
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	reqHeaders := req.Header.Clone()
	rspSnap := &capture.ResponseSnapshot{
		StatusCode: http.StatusOK,
		Headers:    http.Header{},
	}
	for k, vv := range resp.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "sec-websocket") {
			for _, v := range vv {
				rspSnap.Headers.Add(k, v)
			}
		}
	}

	c := &capture.Capture{
		ID:        capID,
		Timestamp: start,
		Protocol:  "h2",
		Request: capture.RequestSnapshot{
			Method:  req.Method,
			URL:     req.URL.String(),
			Headers: reqHeaders,
			Body:    nil,
		},
		Response: rspSnap,
		Duration: time.Since(start),
	}

	rw.WriteHeader(http.StatusOK)
	if rc := http.NewResponseController(rw); rc != nil {
		_ = rc.Flush()
	}

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	clientBR := bufio.NewReader(bodyReaderWithCtx{r: req.Body, ctx: ctx})

	m.parent.relayWebSocketRFC6455(c, clientBR, &flushResponseWriter{rw}, originBR, oc, false, func() {
		cancel()
		_ = oc.Close()
	})
	m.parent.Log.Info("websocket", "url", req.URL.String(), "id", capID[:8], "proto", "h2")
}

func (h *Handler) mitmHTTP2(clientConn net.Conn, originConn *tls.Conn, hostname string, _ *http.Request) {
	srv := &http2.Server{}
	handler := &mitmH2Handler{
		hostname:  hostname,
		parent:    h,
		transport: h.Transport,
	}
	srv.ServeConn(clientConn, &http2.ServeConnOpts{Handler: handler})
}
