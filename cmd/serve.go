package cmd

import (
	"context"
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/muxover/snare/proxy"
	"github.com/muxover/snare/proxy/cert"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	servePort     string
	serveBind     string
	serveNoMITM   bool
	serveStoreDir string
	serveVerbose  bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the proxy server",
	Long:  "Start the HTTP/HTTPS proxy. Every request is captured and saved to the store directory. Use --bind 0.0.0.0 to accept connections from other machines.",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVarP(&servePort, "port", "p", "8888", "Port to listen on (1-65535)")
	serveCmd.Flags().StringVarP(&serveBind, "bind", "b", "127.0.0.1", "Address to bind (use 0.0.0.0 for all interfaces)")
	serveCmd.Flags().BoolVar(&serveNoMITM, "no-mitm", false, "Disable HTTPS MITM; CONNECT is tunneled only")
	serveCmd.Flags().StringVar(&serveStoreDir, "store-dir", "", "Directory to save each capture (default: SNARE_STORE or ~/.snare/captures)")
	serveCmd.Flags().BoolVarP(&serveVerbose, "verbose", "v", false, "Enable debug logging (connection-level and verbose output)")
}

func runServe(cmd *cobra.Command, args []string) error {
	port, err := parsePort(servePort)
	if err != nil {
		return err
	}
	servePort = port

	storeDir := serveStoreDir
	if storeDir == "" {
		storeDir = config.StoreDir()
	}
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return err
	}
	store := capture.NewStore(1000, storeDir)

	transport := proxy.ProxyTransport(true)
	logLevel := slog.LevelInfo
	if serveVerbose {
		logLevel = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	var hostCerts *cert.HostCertCache
	mitmEnable := !serveNoMITM
	if mitmEnable {
		ca, key, err := cert.LoadOrCreateCA(config.CADir())
		if err != nil {
			log.Warn("CA load failed, MITM disabled", "err", err)
			mitmEnable = false
		} else {
			hostCerts = cert.NewHostCertCache(ca, key)
		}
	}

	handler := &proxy.Handler{
		Transport:  transport,
		Store:      store,
		HostCerts:  hostCerts,
		Log:        log,
		MitmEnable: mitmEnable,
	}

	addr := serveBind + ":" + servePort
	srv, err := proxy.NewServer(addr, handler, log)
	if err != nil {
		return err
	}

	log.Info("proxy listening", "addr", addr, "mitm", mitmEnable)
	log.Info("intercepted requests saved to", "dir", storeDir)
	log.Info("set HTTP_PROXY and HTTPS_PROXY to http://" + addr)
	if serveBind == "0.0.0.0" {
		for _, ip := range localIPs() {
			log.Info("external clients can use", "url", "http://"+ip+":"+servePort)
		}
	}
	srv.Start()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func localIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			out = append(out, ipnet.IP.String())
		}
	}
	return out
}

func parsePort(s string) (string, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("invalid port: %q (must be 1-65535)", s)
		}
		n = n*10 + int(c-'0')
		if n > 65535 {
			return "", fmt.Errorf("invalid port: %q (must be 1-65535)", s)
		}
	}
	if n < 1 {
		return "", fmt.Errorf("invalid port: %q (must be 1-65535)", s)
	}
	return s, nil
}
