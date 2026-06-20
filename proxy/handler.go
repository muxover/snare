package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/intercept"
	"github.com/muxover/snare/v2/mock"
	"github.com/muxover/snare/v2/proxy/cert"
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
	OnCapture        func(*capture.Capture)
	IgnorePatterns   []string
	MapRemotes       []MapRemoteRule
	BodyRewrites     []BodyRewrite
	MaxBodySize      int64
	Mode             string
	ReverseTarget    *url.URL
	Delay            time.Duration
	ChaosRate        float64
	Shadows          []*url.URL
	Plugins          []string
	ProtoDecoder     *ProtoDecoder
}

type HostRewrite struct {
	From string
	To   string
}

type HeaderValue struct {
	Key   string
	Value string
}

type MapRemoteRule struct {
	SourceHost string
	TargetBase *url.URL
}

type BodyRewrite struct {
	Pattern     *regexp.Regexp
	Replacement []byte
}

func (h *Handler) addCapture(c *capture.Capture) {
	if h.Store != nil {
		h.Store.Add(c)
	}
	if h.OnCapture != nil {
		go h.OnCapture(c)
	}
	if len(h.Plugins) > 0 {
		go h.runPlugins(c)
	}
}

func (h *Handler) runPlugins(c *capture.Capture) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	for _, pluginCmd := range h.Plugins {
		go func(cmd string) {
			var proc *exec.Cmd
			if runtime.GOOS == "windows" {
				proc = exec.Command("cmd", "/C", cmd)
			} else {
				proc = exec.Command("sh", "-c", cmd)
			}
			proc.Stdin = bytes.NewReader(data)
			proc.Stderr = os.Stderr
			_ = proc.Run()
		}(pluginCmd)
	}
}

func (h *Handler) fireShadows(method, rawURL string, header http.Header, body []byte) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	for _, shadow := range h.Shadows {
		target := *shadow
		target.Path = u.Path
		target.RawQuery = u.RawQuery
		req, err := http.NewRequest(method, target.String(), bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header = header.Clone()
		resp, err := h.Transport.RoundTrip(req)
		if err != nil {
			h.Log.Debug("shadow failed", "url", target.String(), "err", err)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func isGRPC(h http.Header) bool {
	return strings.HasPrefix(h.Get("Content-Type"), "application/grpc")
}

func parseGRPCFrames(body []byte, direction string) []capture.GRPCFrame {
	var frames []capture.GRPCFrame
	for len(body) >= 5 {
		compressed := body[0] == 1
		msgLen := binary.BigEndian.Uint32(body[1:5])
		body = body[5:]
		if uint32(len(body)) < msgLen {
			break
		}
		payload := make([]byte, msgLen)
		copy(payload, body[:msgLen])
		frames = append(frames, capture.GRPCFrame{
			Direction:  direction,
			Compressed: compressed,
			Data:       capture.BodyBytes(payload),
		})
		body = body[msgLen:]
	}
	return frames
}

func reqProto(req *http.Request) string {
	switch req.ProtoMajor {
	case 3:
		return "h3"
	case 2:
		return "h2"
	default:
		return "h1"
	}
}

func (h *Handler) isIgnored(urlStr string) bool {
	for _, p := range h.IgnorePatterns {
		if strings.Contains(urlStr, p) {
			return true
		}
	}
	return false
}

func (h *Handler) applyBodyRewrites(body []byte) []byte {
	for _, rw := range h.BodyRewrites {
		body = rw.Pattern.ReplaceAll(body, rw.Replacement)
	}
	return body
}

func (h *Handler) shouldChaos() bool {
	return h.ChaosRate > 0 && rand.Float64()*100 < h.ChaosRate
}

func (h *Handler) capSlice(b []byte) []byte {
	if h.MaxBodySize > 0 && int64(len(b)) > h.MaxBodySize {
		return b[:h.MaxBodySize]
	}
	return b
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			h.Log.Error("handler panic", "err", err)
			http.Error(rw, "proxy error", http.StatusInternalServerError)
		}
	}()

	h.Log.Info("request", "method", req.Method, "host", req.Host, "path", req.URL.Path, "remote", req.RemoteAddr)

	if h.shouldChaos() {
		h.Log.Info("chaos: dropping request", "method", req.Method, "host", req.Host)
		http.Error(rw, "chaos: request dropped", http.StatusServiceUnavailable)
		return
	}

	if h.Mode == "reverse" {
		if req.Method == http.MethodConnect {
			http.Error(rw, "CONNECT not supported in reverse proxy mode", http.StatusMethodNotAllowed)
			return
		}
		h.serveReverse(rw, req)
		return
	}

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

	bodyBuf, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(bodyBuf))

	u := req.URL
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Host == "" {
		u.Host = req.Host
	}

	ignored := h.isIgnored(u.String())

	if !ignored && h.Intercept != nil && intercept.MatchesPattern(req, h.InterceptMatch) {
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

	if !ignored && isWebSocketUpgrade(outReq) && h.tryServeHTTPWebSocket(rw, outReq, capID, start, bodyBuf) {
		return
	}

	resp, err := h.Transport.RoundTrip(outReq)
	duration := time.Since(start)
	if err != nil {
		if ignored {
			http.Error(rw, err.Error(), http.StatusBadGateway)
			return
		}
		reqCaptureBody := decompressBody(bodyBuf, req.Header.Get("Content-Encoding"))
		reqHeaders := req.Header.Clone()
		if len(reqCaptureBody) != len(bodyBuf) {
			reqHeaders.Del("Content-Encoding")
			reqHeaders.Set("Content-Length", strconv.Itoa(len(reqCaptureBody)))
		}
		h.addCapture(&capture.Capture{
			ID:        capID,
			Timestamp: start,
			Protocol:  reqProto(req),
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
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	resp.Header.Del("Alt-Svc")

	reqCaptureBody := decompressBody(bodyBuf, req.Header.Get("Content-Encoding"))
	reqHeaders := outReq.Header.Clone()
	if len(reqCaptureBody) != len(bodyBuf) {
		reqHeaders.Del("Content-Encoding")
		reqHeaders.Set("Content-Length", strconv.Itoa(len(reqCaptureBody)))
	}

	if !ignored && isSSE(resp.Header) {
		c := &capture.Capture{
			ID:        capID,
			Timestamp: start,
			Protocol:  reqProto(req),
			Request: capture.RequestSnapshot{
				Method:  req.Method,
				URL:     outReq.URL.String(),
				Headers: reqHeaders,
				Body:    capture.BodyBytes(h.capSlice(reqCaptureBody)),
			},
			Duration: duration,
		}
		c.GraphQL = detectGraphQL(outReq.Header.Get("Content-Type"), reqCaptureBody)
		if len(h.Shadows) > 0 {
			go h.fireShadows(req.Method, outReq.URL.String(), req.Header, bodyBuf)
		}
		if h.Delay > 0 {
			time.Sleep(h.Delay)
		}
		h.streamSSE(rw, resp, c)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	if len(h.BodyRewrites) > 0 {
		respBody = h.applyBodyRewrites(respBody)
	}
	captureBody := decompressBody(respBody, resp.Header.Get("Content-Encoding"))
	respHeaders := resp.Header.Clone()
	if len(captureBody) != len(respBody) {
		respHeaders.Del("Content-Encoding")
		respHeaders.Set("Content-Length", strconv.Itoa(len(captureBody)))
	}

	if !ignored {
		c := &capture.Capture{
			ID:        capID,
			Timestamp: start,
			Protocol:  reqProto(req),
			Request: capture.RequestSnapshot{
				Method:  req.Method,
				URL:     outReq.URL.String(),
				Headers: reqHeaders,
				Body:    capture.BodyBytes(h.capSlice(reqCaptureBody)),
			},
			Response: &capture.ResponseSnapshot{
				StatusCode: resp.StatusCode,
				Headers:    respHeaders,
				Body:       capture.BodyBytes(h.capSlice(captureBody)),
			},
			Duration: duration,
		}
		c.GraphQL = detectGraphQL(req.Header.Get("Content-Type"), reqCaptureBody)
		if isGRPC(outReq.Header) || isGRPC(resp.Header) {
			frames := append(parseGRPCFrames(reqCaptureBody, "request"), parseGRPCFrames(captureBody, "response")...)
			if len(frames) > 0 {
				c.GRPC = &capture.GRPCCapture{ServiceMethod: outReq.URL.Path, Frames: frames}
				h.decodeGRPC(c.GRPC, outReq.URL.Path, reqCaptureBody, captureBody)
			}
		}
		h.addCapture(c)
		h.Log.Info("captured", "method", req.Method, "url", outReq.URL.String(), "status", resp.StatusCode, "id", capID[:8])
	}

	if len(h.Shadows) > 0 {
		go h.fireShadows(req.Method, outReq.URL.String(), req.Header, bodyBuf)
	}
	if h.Delay > 0 {
		time.Sleep(h.Delay)
	}
	for k, v := range resp.Header {
		for _, vv := range v {
			rw.Header().Add(k, vv)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	_, _ = rw.Write(respBody)
}

func (h *Handler) serveReverse(rw http.ResponseWriter, req *http.Request) {
	if h.Mocks != nil {
		if rule := h.Mocks.Match(req); rule != nil {
			h.serveMock(rw, req, rule)
			return
		}
	}

	start := time.Now()
	capID := uuid.New().String()

	bodyBuf, _ := io.ReadAll(req.Body)

	outURL := *h.ReverseTarget
	outURL.Path = req.URL.Path
	outURL.RawQuery = req.URL.RawQuery

	ignored := h.isIgnored(outURL.String())

	if !ignored && h.Intercept != nil && intercept.MatchesPattern(req, h.InterceptMatch) {
		newBody, dropped := h.holdAndApply(req, capID, start, outURL.String(), bodyBuf)
		if dropped {
			http.Error(rw, "request dropped by intercept", http.StatusBadGateway)
			return
		}
		bodyBuf = newBody
	}

	outReq, err := http.NewRequest(req.Method, outURL.String(), bytes.NewReader(bodyBuf))
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	outReq.Header = req.Header.Clone()
	outReq.Host = h.ReverseTarget.Host
	h.applyOutboundMods(outReq)

	resp, err := h.Transport.RoundTrip(outReq)
	duration := time.Since(start)
	if err != nil {
		if !ignored {
			h.addCapture(&capture.Capture{ID: capID, Timestamp: start, Protocol: reqProto(req),
				Request:  capture.RequestSnapshot{Method: req.Method, URL: outURL.String(), Headers: outReq.Header.Clone()},
				Duration: duration, Error: err.Error(),
			})
		}
		http.Error(rw, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	resp.Header.Del("Alt-Svc")

	reqCaptureBody := decompressBody(bodyBuf, req.Header.Get("Content-Encoding"))
	reqHeaders := outReq.Header.Clone()
	if len(reqCaptureBody) != len(bodyBuf) {
		reqHeaders.Del("Content-Encoding")
		reqHeaders.Set("Content-Length", strconv.Itoa(len(reqCaptureBody)))
	}

	if !ignored && isSSE(resp.Header) {
		c := &capture.Capture{
			ID:        capID,
			Timestamp: start,
			Protocol:  reqProto(req),
			Request: capture.RequestSnapshot{
				Method:  req.Method,
				URL:     outURL.String(),
				Headers: reqHeaders,
				Body:    capture.BodyBytes(h.capSlice(reqCaptureBody)),
			},
			Duration: duration,
		}
		c.GraphQL = detectGraphQL(outReq.Header.Get("Content-Type"), reqCaptureBody)
		if len(h.Shadows) > 0 {
			go h.fireShadows(req.Method, outURL.String(), req.Header, bodyBuf)
		}
		if h.Delay > 0 {
			time.Sleep(h.Delay)
		}
		h.streamSSE(rw, resp, c)
		return
	}

	respBody, _ := io.ReadAll(resp.Body)
	if len(h.BodyRewrites) > 0 {
		respBody = h.applyBodyRewrites(respBody)
	}
	captureBody := decompressBody(respBody, resp.Header.Get("Content-Encoding"))
	respHeaders := resp.Header.Clone()
	if len(captureBody) != len(respBody) {
		respHeaders.Del("Content-Encoding")
		respHeaders.Set("Content-Length", strconv.Itoa(len(captureBody)))
	}

	if !ignored {
		c := &capture.Capture{
			ID:        capID,
			Timestamp: start,
			Protocol:  reqProto(req),
			Request: capture.RequestSnapshot{
				Method:  req.Method,
				URL:     outURL.String(),
				Headers: reqHeaders,
				Body:    capture.BodyBytes(h.capSlice(reqCaptureBody)),
			},
			Response: &capture.ResponseSnapshot{
				StatusCode: resp.StatusCode,
				Headers:    respHeaders,
				Body:       capture.BodyBytes(h.capSlice(captureBody)),
			},
			Duration: duration,
		}
		c.GraphQL = detectGraphQL(req.Header.Get("Content-Type"), reqCaptureBody)
		if isGRPC(outReq.Header) || isGRPC(resp.Header) {
			frames := append(parseGRPCFrames(reqCaptureBody, "request"), parseGRPCFrames(captureBody, "response")...)
			if len(frames) > 0 {
				c.GRPC = &capture.GRPCCapture{ServiceMethod: outReq.URL.Path, Frames: frames}
				h.decodeGRPC(c.GRPC, outReq.URL.Path, reqCaptureBody, captureBody)
			}
		}
		h.addCapture(c)
		h.Log.Info("captured", "method", req.Method, "url", outURL.String(), "status", resp.StatusCode, "id", capID[:8])
	}

	if len(h.Shadows) > 0 {
		go h.fireShadows(req.Method, outURL.String(), req.Header, bodyBuf)
	}
	if h.Delay > 0 {
		time.Sleep(h.Delay)
	}
	for k, v := range resp.Header {
		for _, vv := range v {
			rw.Header().Add(k, vv)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	_, _ = rw.Write(respBody)
}

func (h *Handler) tryServeHTTPWebSocket(rw http.ResponseWriter, outReq *http.Request, capID string, start time.Time, bodyBuf []byte) bool {
	hj, ok := rw.(http.Hijacker)
	if !ok {
		return false
	}
	host := outReq.URL.Hostname()
	port := outReq.URL.Port()
	if port == "" {
		if strings.EqualFold(outReq.URL.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	addr := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var oc net.Conn
	var err error
	if strings.EqualFold(outReq.URL.Scheme, "https") {
		oc, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"http/1.1", "http/1.0"}})
	} else {
		oc, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		h.Log.Error("ws dial origin", "addr", addr, "err", err)
		return false
	}
	if err := outReq.Write(oc); err != nil {
		oc.Close()
		h.Log.Error("ws write origin", "err", err)
		return false
	}
	if len(bodyBuf) > 0 {
		if _, err := oc.Write(bodyBuf); err != nil {
			oc.Close()
			return false
		}
	}
	originBR := bufio.NewReader(oc)
	resp, err := http.ReadResponse(originBR, outReq)
	if err != nil || !isWebSocketAcceptResponse(resp) {
		if err == nil && resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		oc.Close()
		return false
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	reqCaptureBody := decompressBody(bodyBuf, outReq.Header.Get("Content-Encoding"))
	reqHeaders := outReq.Header.Clone()
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
	c := &capture.Capture{
		ID:        capID,
		Timestamp: start,
		Protocol:  "ws",
		Request: capture.RequestSnapshot{
			Method:  outReq.Method,
			URL:     outReq.URL.String(),
			Headers: reqHeaders,
			Body:    capture.BodyBytes(reqCaptureBody),
		},
		Response: &capture.ResponseSnapshot{
			StatusCode: resp.StatusCode,
			Headers:    respHeaders,
			Body:       capture.BodyBytes(captureBody),
		},
		Duration: time.Since(start),
	}
	brw, _, err := hj.Hijack()
	if err != nil {
		oc.Close()
		return false
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	var wire bytes.Buffer
	if err := resp.Write(&wire); err != nil {
		brw.Close()
		oc.Close()
		return false
	}
	if _, err := brw.Write(wire.Bytes()); err != nil {
		brw.Close()
		oc.Close()
		return false
	}
	clientBR := bufio.NewReader(brw)
	h.relayWebSocketRFC6455(c, clientBR, brw, originBR, oc, true, func() {
		_ = brw.Close()
		_ = oc.Close()
	})
	h.Log.Info("websocket", "url", outReq.URL.String(), "id", capID[:8], "proto", "h1-proxy")
	return true
}

func (h *Handler) serveCONNECT(rw http.ResponseWriter, req *http.Request) {
	if h.isIgnored(req.Host) {
		h.tunnelCONNECT(rw, req)
		return
	}
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

		ignored := h.isIgnored(req.URL.String())

		if !ignored && h.shouldChaos() {
			h.Log.Info("chaos: dropping request", "url", req.URL.String())
			_ = writeDropH1(clientConn)
			continue
		}

		if !ignored && h.Mocks != nil {
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

		if !ignored && h.Intercept != nil && intercept.MatchesPattern(req, h.InterceptMatch) {
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
			if !ignored {
				h.addCapture(&capture.Capture{ID: capID, Timestamp: start, Error: err.Error()})
			}
			return
		}
		if len(bodyBuf) > 0 {
			_, _ = originConn.Write(bodyBuf)
		}

		resp, err := http.ReadResponse(originReader, req)
		if err != nil {
			if !ignored {
				h.addCapture(&capture.Capture{ID: capID, Timestamp: start, Error: err.Error()})
			}
			return
		}
		duration := time.Since(start)

		resp.Header.Del("Alt-Svc")

		reqCaptureBody := decompressBody(bodyBuf, req.Header.Get("Content-Encoding"))
		reqHeaders := req.Header.Clone()
		if len(reqCaptureBody) != len(bodyBuf) {
			reqHeaders.Del("Content-Encoding")
			reqHeaders.Set("Content-Length", strconv.Itoa(len(reqCaptureBody)))
		}

		if !ignored && isSSE(resp.Header) {
			c := &capture.Capture{
				ID:        capID,
				Timestamp: start,
				Protocol:  "h1",
				Request: capture.RequestSnapshot{
					Method:  req.Method,
					URL:     req.URL.String(),
					Headers: reqHeaders,
					Body:    capture.BodyBytes(h.capSlice(reqCaptureBody)),
				},
				Duration: duration,
			}
			c.GraphQL = detectGraphQL(req.Header.Get("Content-Type"), reqCaptureBody)
			fmt.Fprintf(clientConn, "HTTP/1.1 %d %s\r\n", resp.StatusCode, http.StatusText(resp.StatusCode))
			for k, v := range resp.Header {
				for _, vv := range v {
					fmt.Fprintf(clientConn, "%s: %s\r\n", k, vv)
				}
			}
			fmt.Fprintf(clientConn, "\r\n")
			frames := parseSSEStream(resp.Body, func(line string) bool {
				_, err := fmt.Fprintf(clientConn, "%s\n", line)
				return err == nil
			})
			resp.Body.Close()
			if len(frames) > 0 {
				c.SSE = &capture.SSECapture{Frames: frames}
			}
			h.addCapture(c)
			h.Log.Info("captured (sse)", "method", req.Method, "url", req.URL.String(), "id", capID[:8])
			return
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if len(h.BodyRewrites) > 0 {
			respBody = h.applyBodyRewrites(respBody)
		}

		if !ignored {
			captureBody := decompressBody(respBody, resp.Header.Get("Content-Encoding"))
			respHeaders := resp.Header.Clone()
			if len(captureBody) != len(respBody) {
				respHeaders.Del("Content-Encoding")
				respHeaders.Set("Content-Length", strconv.Itoa(len(captureBody)))
			}
			proto := "h1"
			if isWebSocketUpgrade(req) && isWebSocketAcceptResponse(resp) {
				proto = "ws"
			}
			c := &capture.Capture{
				ID:        capID,
				Timestamp: start,
				Protocol:  proto,
				Request: capture.RequestSnapshot{
					Method:  req.Method,
					URL:     req.URL.String(),
					Headers: reqHeaders,
					Body:    capture.BodyBytes(h.capSlice(reqCaptureBody)),
				},
				Response: &capture.ResponseSnapshot{
					StatusCode: resp.StatusCode,
					Headers:    respHeaders,
					Body:       capture.BodyBytes(h.capSlice(captureBody)),
				},
				Duration: duration,
			}
			c.GraphQL = detectGraphQL(req.Header.Get("Content-Type"), reqCaptureBody)
			if proto == "ws" {
				h.Log.Info("websocket", "url", req.URL.String(), "id", capID[:8])
				resp.Body = io.NopCloser(bytes.NewReader(respBody))
				respBytes := bytes.NewBuffer(nil)
				_ = resp.Write(respBytes)
				_, _ = clientConn.Write(respBytes.Bytes())
				h.relayWebSocketCapture(c, clientReader, clientConn, originReader, originConn)
				return
			}
			if isGRPC(req.Header) || isGRPC(resp.Header) {
				frames := append(parseGRPCFrames(reqCaptureBody, "request"), parseGRPCFrames(captureBody, "response")...)
				if len(frames) > 0 {
					c.GRPC = &capture.GRPCCapture{ServiceMethod: req.URL.Path, Frames: frames}
					h.decodeGRPC(c.GRPC, req.URL.Path, reqCaptureBody, captureBody)
				}
			}
			h.addCapture(c)
			h.Log.Info("captured", "method", req.Method, "url", req.URL.String(), "status", resp.StatusCode, "id", capID[:8])
		}

		if h.Delay > 0 {
			time.Sleep(h.Delay)
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		respBytes := bytes.NewBuffer(nil)
		_ = resp.Write(respBytes)
		_, _ = clientConn.Write(respBytes.Bytes())
	}
}

func (h *Handler) holdAndApply(req *http.Request, id string, ts time.Time, rawURL string, body []byte) ([]byte, bool) {
	timeout := h.InterceptTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	pr := &intercept.PendingRequest{
		ID:        id,
		Timestamp: ts,
		Method:    req.Method,
		URL:       rawURL,
		Headers:   req.Header.Clone(),
		Body:      string(body),
	}
	if err := h.Intercept.Enqueue(pr); err != nil {
		h.Log.Error("intercept enqueue", "err", err)
		return body, false
	}
	h.Log.Info("intercepted — waiting for decision", "id", id[:8], "method", req.Method, "url", rawURL)
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
	_, err := conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
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
	h.applyMapRemote(req)
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

func (h *Handler) decodeGRPC(g *capture.GRPCCapture, method string, reqBody, respBody []byte) {
	if h.ProtoDecoder == nil {
		return
	}
	if decoded, err := h.ProtoDecoder.DecodeRequest(method, reqBody); err == nil {
		g.DecodedRequest = decoded
	}
	if decoded, err := h.ProtoDecoder.DecodeResponse(method, respBody); err == nil {
		g.DecodedResponse = decoded
	}
}

func (h *Handler) applyMapRemote(req *http.Request) {
	if req.URL == nil || len(h.MapRemotes) == 0 {
		return
	}
	for _, rule := range h.MapRemotes {
		if strings.EqualFold(req.URL.Hostname(), rule.SourceHost) {
			newURL := *rule.TargetBase
			newURL.Path = req.URL.Path
			newURL.RawQuery = req.URL.RawQuery
			req.URL = &newURL
			req.Host = rule.TargetBase.Host
			return
		}
	}
}
