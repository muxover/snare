package proxy

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/quic-go/quic-go/http3"
)

type Server struct {
	*http.Server
	Listener net.Listener
	h3srv    *http3.Server
}

type logListener struct {
	net.Listener
	log *slog.Logger
}

func (l *logListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.log.Debug("connection", "remote", conn.RemoteAddr().String())
	return conn, nil
}

func NewServer(addr string, handler http.Handler, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	ln = &logListener{Listener: ln, log: log}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return &Server{Server: srv, Listener: ln}, nil
}

func (s *Server) Start() {
	go func() {
		_ = s.Serve(s.Listener)
	}()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.h3srv != nil {
		_ = s.h3srv.Close()
	}
	return s.Server.Shutdown(ctx)
}

// StartH3 starts an HTTP/3 (QUIC) server on the same address as the TCP server.
// tlsCfg must include at least one certificate (or GetCertificate). The TLS
// config is wrapped with the QUIC ALPN identifiers by quic-go automatically.
func (s *Server) StartH3(handler http.Handler, tlsCfg *tls.Config, log *slog.Logger) {
	addr := s.Listener.Addr().String()
	s.h3srv = &http3.Server{
		Addr:      addr,
		Handler:   handler,
		TLSConfig: http3.ConfigureTLSConfig(tlsCfg),
	}
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Warn("H3: UDP listen failed, HTTP/3 disabled", "addr", addr, "err", err)
		s.h3srv = nil
		return
	}
	log.Info("H3 listening", "addr", addr)
	go func() { _ = s.h3srv.Serve(conn) }()
}
