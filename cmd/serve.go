package cmd

import (
	"context"
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/muxover/snare/mock"
	"github.com/muxover/snare/proxy"
	"github.com/muxover/snare/proxy/cert"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	servePort          string
	serveBind          string
	serveNoMITM        bool
	serveStoreDir      string
	serveVerbose       bool
	serveMaxCaptures   int
	serveUpstreamProxy string
	serveRewriteHost   []string
	serveAddHeader     []string
	serveRemoveHeader  []string
	serveMockFile      string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the proxy server",
	Long:  "Start the HTTP/HTTPS proxy. Every request is captured and saved to the store directory. Use --bind 0.0.0.0 to accept connections from other machines. Use --max-captures to limit stored captures (oldest pruned).",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVarP(&servePort, "port", "p", "8888", "Port to listen on (1-65535)")
	serveCmd.Flags().StringVarP(&serveBind, "bind", "b", "127.0.0.1", "Address to bind (use 0.0.0.0 for all interfaces)")
	serveCmd.Flags().BoolVar(&serveNoMITM, "no-mitm", false, "Disable HTTPS MITM; CONNECT is tunneled only")
	serveCmd.Flags().StringVar(&serveStoreDir, "store-dir", "", "Directory to save each capture (default: SNARE_STORE or ~/.snare/captures)")
	serveCmd.Flags().BoolVarP(&serveVerbose, "verbose", "v", false, "Enable debug logging (connection-level and verbose output)")
	serveCmd.Flags().IntVar(&serveMaxCaptures, "max-captures", 1000, "Maximum number of captures to keep (oldest files pruned when exceeded)")
	serveCmd.Flags().StringVar(&serveUpstreamProxy, "upstream-proxy", "", "Send outbound traffic through this proxy URL (http://host:port)")
	serveCmd.Flags().StringArrayVar(&serveRewriteHost, "rewrite-host", nil, "Rewrite outbound host using from=to (repeatable)")
	serveCmd.Flags().StringArrayVar(&serveAddHeader, "add-header", nil, "Add or override outbound header (Key: Value); repeatable")
	serveCmd.Flags().StringArrayVar(&serveRemoveHeader, "remove-header", nil, "Remove outbound header by name; repeatable")
	serveCmd.Flags().StringVar(&serveMockFile, "mock-file", "", "Load mock rules from this file (default: SNARE_MOCKS or ~/.snare/mocks.json)")
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
	maxCap := serveMaxCaptures
	if maxCap <= 0 {
		maxCap = 1000
	}
	store := capture.NewStore(maxCap, storeDir)

	rewrites, err := parseHostRewriteRules(serveRewriteHost)
	if err != nil {
		return err
	}
	addHeaders, err := parseHeaderPairs(serveAddHeader)
	if err != nil {
		return err
	}
	removeHeaders := normalizeHeaderNames(serveRemoveHeader)

	mockFilePath := serveMockFile
	if mockFilePath == "" {
		mockFilePath = config.MockFile()
	}
	mocks := mock.NewStore(mockFilePath)

	transport, err := proxy.ProxyTransport(true, serveUpstreamProxy)
	if err != nil {
		return err
	}
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
		Transport:     transport,
		Store:         store,
		Mocks:         mocks,
		HostCerts:     hostCerts,
		Log:           log,
		MitmEnable:    mitmEnable,
		HostRewrites:  rewrites,
		AddHeaders:    addHeaders,
		RemoveHeaders: removeHeaders,
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

func parseHostRewriteRules(items []string) ([]proxy.HostRewrite, error) {
	out := make([]proxy.HostRewrite, 0, len(items))
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --rewrite-host value %q (expected from=to)", item)
		}
		from := strings.TrimSpace(parts[0])
		to := strings.TrimSpace(parts[1])
		if from == "" || to == "" {
			return nil, fmt.Errorf("invalid --rewrite-host value %q (expected from=to)", item)
		}
		out = append(out, proxy.HostRewrite{From: from, To: to})
	}
	return out, nil
}

func parseHeaderPairs(items []string) ([]proxy.HeaderValue, error) {
	out := make([]proxy.HeaderValue, 0, len(items))
	for _, item := range items {
		parts := strings.SplitN(item, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --add-header value %q (expected Key: Value)", item)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid --add-header value %q (missing header name)", item)
		}
		out = append(out, proxy.HeaderValue{Key: key, Value: val})
	}
	return out, nil
}

func normalizeHeaderNames(items []string) []string {
	out := make([]string, 0, len(items))
	for _, key := range items {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, http.CanonicalHeaderKey(key))
	}
	return out
}
