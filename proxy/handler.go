package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/intercept"
	"github.com/muxover/snare/mock"
	"github.com/muxover/snare/proxy/cert"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Handler struct {
	Transport        *http.Transport
	Store            *capture.Store
	Mocks            *mock.Store
	Intercept        *intercept.Queue
	InterceptMatch   string
	InterceptTimeout time.Duration
	HostCerts        *cert.HostCertCache
	Log              *slog.Logger
	MitmEnable       bool
	HostRewrites     []HostRewrite
	AddHeaders       []HeaderValue
	RemoveHeaders    []string
}

type HostRewrite struct {
	From string
	To   string
}

type HeaderValue struct {
	Key   string
	Value string
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			h.Log.Error("handler panic", "err", err)
			http.Error(rw, "proxy error", http.StatusInternalServerError)
		}
	}()

	h.Log.Info("request", "method", req.Method, "host", req.Host, "path", req.URL.Path, "remote", req.RemoteAddr)

	if req.Method == http.MethodConnect {
		h.serveCONNECT(rw, req)
		return
	}
	h.serveHTTP(rw, req)
}

func (h *Handler) serveHTTP(rw http.ResponseWriter, req *http.Request) {
	if h.Mocks != nil {
		if rule := h.Mocks.Match(req); rule != nil {
			h.serveMock(rw, req, rule)
			return
		}
	}

	start := time.Now()
	capID := uuid.New().String()

	var bodyBuf []byte
	if req.Body != nil {
		bodyBuf, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBuf))
	}

	u := req.URL
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Host == "" {
		u.Host = req.Host
	}

	if h.Intercept != nil && intercept.MatchesPattern(req, h.InterceptMatch) {
		newBody, dropped := h.holdAndApply(req, capID, start, u.String(), bodyBuf)
		if dropped {
			http.Error(rw, "request dropped by intercept", http.StatusBadGateway)
			return
		}
		bodyBuf = newBody
	}

	outReq, err := http.NewRequest(req.Method, u.String(), bytes.NewReader(bodyBuf))
	if err != nil {
		h.Log.Error("new request", "err", err)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	outReq.Header = req.Header.Clone()
	outReq.Host = req.Host
	h.applyOutboundMods(outReq)

	resp, err := h.Transport.RoundTrip(outReq)
	duration := time.Since(start)
	if err != nil {
		reqCaptureBody := decompressBody(bodyBuf, req.Header.Get("Content-Encoding"))
		reqHeaders := req.Header.Clone()
		if len(reqCaptureBody) != len(bodyBuf) {
			reqHeaders.Del("Content-Encoding")
			reqHeaders.Set("Content-Length", strconv.Itoa(len(reqCaptureBody)))
		}
		h.Store.Add(&capture.Capture{
			ID:        capID,
			Timestamp: start,
			Protocol:  "h1",
			Request: capture.RequestSnapshot{
				Method:  req.Method,
				URL:     req.URL.String(),
				Headers: reqHeaders,
				Body:    capture.BodyBytes(reqCaptureBody),
			},
			Duration: duration,
			Error:    err.Error(),
		})
		h.Log.Info("captured (error)", "method", req.Method, "url", req.URL.String(), "err", err)
		h.Log.Error("roundtrip", "err", err)
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	captureBody := decompressBody(respBody, resp.Header.Get("Content-Encoding"))
	respHeaders := resp.Header.Clone()
	if len(captureBody) != len(respBody) {
		respHeaders.Del("Content-Encoding")
		respHeaders.Set("Content-Length", strconv.Itoa(len(captureBody)))
	}
	reqCaptureBody := decompressBody(bodyBuf, req.Header.Get("Content-Encoding"))
	reqHeaders := req.Header.Clone()
	if len(reqCaptureBody) != len(bodyBuf) {
		reqHeaders.Del("Content-Encoding")
		reqHeaders.Set("Content-Length", strconv.Itoa(len(reqCaptureBody)))
	}
	c := &capture.Capture{
		ID:        capID,
		Timestamp: start,
		Protocol:  "h1",
		Request: capture.RequestSnapshot{
			Method:  req.Method,
			URL:     outReq.URL.String(),
			Headers: reqHeaders,
			Body:    capture.BodyBytes(reqCaptureBody),
		},
		Response: &capture.ResponseSnapshot{
			StatusCode: resp.StatusCode,
			Headers:    respHeaders,
			Body:       capture.BodyBytes(captureBody),
		},
		Duration: duration,
	}
	h.Store.Add(c)
	h.Log.Info("captured", "method", req.Method, "url", outReq.URL.String(), "status", resp.StatusCode, "id", capID[:8])

	for k, v := range resp.Header {
		for _, vv := range v {
			rw.Header().Add(k, vv)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	_, _ = rw.Write(respBody)
}

func (h *Handler) serveCONNECT(rw http.ResponseWriter, req *http.Request) {
	if !h.MitmEnable || h.HostCerts == nil {
		h.tunnelCONNECT(rw, req)
		return
	}
	h.mitmCONNECT(rw, req)
}

func (h *Handler) tunnelCONNECT(rw http.ResponseWriter, req *http.Request) {
	dest, err := net.DialTimeout("tcp", req.Host, 15*time.Second)
	if err != nil {
		h.Log.Error("connect dial", "host", req.Host, "err", err)
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer dest.Close()

	rw.WriteHeader(http.StatusOK)
	if hj, ok := rw.(http.Hijacker); ok {
		clientConn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		defer clientConn.Close()
		go io.Copy(dest, clientConn)
		io.Copy(clientConn, dest)
	}
}

func (h *Handler) mitmCONNECT(rw http.ResponseWriter, req *http.Request) {
	host := req.Host
	if idx := strings.Index(host, ":"); idx == -1 {
		host = host + ":443"
	}

	originConn, err := tls.Dial("tcp", host, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"http/1.1"},
	})
	if err != nil {
		h.Log.Error("mitm dial origin", "host", host, "err", err)
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer originConn.Close()

	rw.WriteHeader(http.StatusOK)
	hj, ok := rw.(http.Hijacker)
	if !ok {
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()

	hostname := req.Host
	if idx := strings.Index(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}
	tlsCert, tlsKey, err := h.HostCerts.GetCertificate(hostname)
	if err != nil {
		h.Log.Error("get cert", "host", hostname, "err", err)
		return
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{tlsCert.Raw},
			PrivateKey:  tlsKey,
		}},
		NextProtos: []string{"h2", "http/1.1"},
	}
	tlsClientConn := tls.Server(clientConn, tlsConfig)
	if err := tlsClientConn.Handshake(); err != nil {
		h.Log.Error("client TLS handshake", "err", err)
		return
	}

	clientProto := tlsClientConn.ConnectionState().NegotiatedProtocol
	if clientProto == "h2" {
		h.mitmHTTP2(tlsClientConn, originConn, hostname, req)
		return
	}
	h.mitmHTTP1(tlsClientConn, originConn, hostname, req)
}

func (h *Handler) mitmHTTP1(clientConn net.Conn, originConn *tls.Conn, hostname string, _ *http.Request) {
	originReader := bufio.NewReader(originConn)
	clientReader := bufio.NewReader(clientConn)

	for {
		req, err := http.ReadRequest(clientReader)
		if err != nil {
			if err != io.EOF {
				h.Log.Debug("read client request", "err", err)
			}
			return
		}
		req.URL.Scheme = "https"
		req.URL.Host = hostname
		h.applyOutboundMods(req)

		if h.Mocks != nil {
			if rule := h.Mocks.Match(req); rule != nil {
				if writeMockH1(clientConn, req, rule, h.Log) {
					continue
				}
			}
		}

		start := time.Now()
		capID := uuid.New().String()
		bodyBuf, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBuf))

		if h.Intercept != nil && intercept.MatchesPattern(req, h.InterceptMatch) {
			newBody, dropped := h.holdAndApply(req, capID, start, req.URL.String(), bodyBuf)
			if dropped {
				_ = writeDropH1(clientConn)
				continue
			}
			bodyBuf = newBody
			req.Body = io.NopCloser(bytes.NewReader(bodyBuf))
		}

		reqBytes, _ := httputil.DumpRequest(req, false)
		if _, err := originConn.Write(reqBytes); err != nil {
			h.Store.Add(&capture.Capture{ID: capID, Timestamp: start, Error: err.Error()})
			return
		}
		if len(bodyBuf) > 0 {
			_, _ = originConn.Write(bodyBuf)
		}

		resp, err := http.ReadResponse(originReader, req)
		if err != nil {
			h.Store.Add(&capture.Capture{ID: capID, Timestamp: start, Error: err.Error()})
			return
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
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
		h.Store.Add(&capture.Capture{
			ID:        capID,
			Timestamp: start,
			Protocol:  "h1",
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
		h.Log.Info("captured", "method", req.Method, "url", req.URL.String(), "status", resp.StatusCode, "id", capID[:8])

		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		respBytes := bytes.NewBuffer(nil)
		_ = resp.Write(respBytes)
		_, _ = clientConn.Write(respBytes.Bytes())
	}
}

func (h *Handler) holdAndApply(req *http.Request, id string, ts time.Time, url string, body []byte) ([]byte, bool) {
	timeout := h.InterceptTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	pr := &intercept.PendingRequest{
		ID:        id,
		Timestamp: ts,
		Method:    req.Method,
		URL:       url,
		Headers:   req.Header.Clone(),
		Body:      string(body),
	}
	if err := h.Intercept.Enqueue(pr); err != nil {
		h.Log.Error("intercept enqueue", "err", err)
		return body, false
	}
	h.Log.Info("intercepted — waiting for decision", "id", id[:8], "method", req.Method, "url", url)
	decided, err := h.Intercept.WaitDecision(id, timeout)
	_ = h.Intercept.Remove(id)
	if err != nil {
		h.Log.Warn("intercept timeout, dropping", "id", id[:8])
		return nil, true
	}
	if decided.Decision == intercept.DecisionDrop {
		h.Log.Info("intercept dropped", "id", id[:8])
		return nil, true
	}
	if decided.ModMethod != "" {
		req.Method = decided.ModMethod
	}
	if decided.ModHeaders != nil {
		req.Header = decided.ModHeaders
	}
	if decided.ModBody != "" {
		return []byte(decided.ModBody), false
	}
	return body, false
}

func writeDropH1(conn net.Conn) error {
	resp := "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
	_, err := conn.Write([]byte(resp))
	return err
}

func writeMockH1(conn net.Conn, req *http.Request, rule *mock.Rule, log *slog.Logger) bool {
	status := rule.Status
	if status == 0 {
		status = http.StatusOK
	}
	body := []byte(rule.Body)
	ct := rule.ContentType
	if ct == "" {
		ct = "application/json"
	}
	resp := &http.Response{
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
	resp.Header.Set("Content-Type", ct)
	for k, v := range rule.Headers {
		resp.Header.Set(k, v)
	}
	if err := resp.Write(conn); err != nil {
		log.Error("write mock h1", "err", err)
		return false
	}
	log.Info("mocked", "method", req.Method, "url", req.URL.String(), "status", status, "rule", rule.ID[:8])
	return true
}

func (h *Handler) serveMock(rw http.ResponseWriter, req *http.Request, rule *mock.Rule) {
	ct := rule.ContentType
	if ct == "" {
		ct = "application/json"
	}
	for k, v := range rule.Headers {
		rw.Header().Set(k, v)
	}
	if rw.Header().Get("Content-Type") == "" {
		rw.Header().Set("Content-Type", ct)
	}
	status := rule.Status
	if status == 0 {
		status = http.StatusOK
	}
	rw.WriteHeader(status)
	_, _ = rw.Write([]byte(rule.Body))
	h.Log.Info("mocked", "method", req.Method, "url", req.URL.String(), "status", status, "rule", rule.ID[:8])
}

func (h *Handler) applyOutboundMods(req *http.Request) {
	h.applyHostRewrite(req)
	for _, key := range h.RemoveHeaders {
		req.Header.Del(key)
	}
	for _, kv := range h.AddHeaders {
		req.Header.Set(kv.Key, kv.Value)
	}
}

func (h *Handler) applyHostRewrite(req *http.Request) {
	if req.URL == nil || req.URL.Host == "" || len(h.HostRewrites) == 0 {
		return
	}
	currentHost := req.URL.Hostname()
	currentPort := req.URL.Port()
	for _, rule := range h.HostRewrites {
		if !strings.EqualFold(currentHost, rule.From) {
			continue
		}
		targetHost := rule.To
		if _, _, err := net.SplitHostPort(rule.To); err != nil && currentPort != "" {
			targetHost = net.JoinHostPort(rule.To, currentPort)
		}
		req.URL.Host = targetHost
		req.Host = targetHost
		return
	}
}
