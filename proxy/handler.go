package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/proxy/cert"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Handler implements http.Handler for the proxy and capture.
type Handler struct {
	Transport  *http.Transport
	Store      *capture.Store
	HostCerts  *cert.HostCertCache
	Log        *slog.Logger
	MitmEnable bool
}

// ServeHTTP handles both plain HTTP and CONNECT.
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
	outReq, err := http.NewRequest(req.Method, u.String(), bytes.NewReader(bodyBuf))
	if err != nil {
		h.Log.Error("new request", "err", err)
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	outReq.Header = req.Header.Clone()
	outReq.Host = req.Host

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

		start := time.Now()
		capID := uuid.New().String()
		bodyBuf, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBuf))

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
