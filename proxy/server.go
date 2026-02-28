package proxy

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server wraps the proxy HTTP server.
type Server struct {
	*http.Server
	Listener net.Listener
}

// logListener logs each accepted connection.
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

// NewServer creates a proxy server listening on addr.
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

// Start runs the server in a goroutine.
func (s *Server) Start() {
	go func() {
		_ = s.Serve(s.Listener)
	}()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.Server.Shutdown(ctx)
}
