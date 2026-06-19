package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"
	"github.com/muxover/snare/v2/intercept"
	"github.com/muxover/snare/v2/mock"
	"github.com/muxover/snare/v2/proxy"
	"github.com/muxover/snare/v2/proxy/cert"
	sess "github.com/muxover/snare/v2/session"
	"github.com/muxover/snare/v2/web"
	"github.com/spf13/cobra"
)

var (
	servePort             string
	serveBind             string
	serveNoMITM           bool
	serveStoreDir         string
	serveVerbose          bool
	serveMaxCaptures      int
	serveUpstreamProxy    string
	serveRewriteHost      []string
	serveAddHeader        []string
	serveRemoveHeader     []string
	serveMockFile         string
	serveIntercept        string
	serveInterceptTimeout time.Duration
	serveOnCapture        string
	serveMode             string
	serveTarget           string
	serveIgnore           []string
	serveMapRemote        []string
	serveRewriteBody      []string
	serveNoStore          bool
	serveMaxBodySize      int64
	serveDelay            time.Duration
	serveChaos            float64
	serveBrowser          bool
	serveShadow           []string
	servePlugins          []string
	serveWeb              bool
	serveWebPort          string
	serveNoConfig         bool
	serveProtoFiles       []string
	serveNoH3             bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the proxy server",
	Long:  "Start the HTTP/HTTPS proxy. In forward mode (default) point HTTP_PROXY/HTTPS_PROXY at it. In reverse mode use --target to proxy a backend directly.",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVarP(&servePort, "port", "p", "8888", "Port to listen on (1-65535)")
	serveCmd.Flags().StringVarP(&serveBind, "bind", "b", "127.0.0.1", "Address to bind (use 0.0.0.0 for all interfaces)")
	serveCmd.Flags().BoolVar(&serveNoMITM, "no-mitm", false, "Disable HTTPS MITM; CONNECT is tunneled only")
	serveCmd.Flags().StringVar(&serveStoreDir, "store-dir", "", "Directory to save captures (default: SNARE_STORE or ~/.snare/captures)")
	serveCmd.Flags().BoolVarP(&serveVerbose, "verbose", "v", false, "Enable debug logging")
	serveCmd.Flags().IntVar(&serveMaxCaptures, "max-captures", 1000, "Maximum number of captures to keep; oldest pruned")
	serveCmd.Flags().StringVar(&serveUpstreamProxy, "upstream-proxy", "", "Forward outbound traffic through this proxy URL (http://host:port)")
	serveCmd.Flags().StringArrayVar(&serveRewriteHost, "rewrite-host", nil, "Rewrite outbound host: from=to (repeatable)")
	serveCmd.Flags().StringArrayVar(&serveAddHeader, "add-header", nil, "Add or override outbound header: Key: Value (repeatable)")
	serveCmd.Flags().StringArrayVar(&serveRemoveHeader, "remove-header", nil, "Remove outbound header by name (repeatable)")
	serveCmd.Flags().StringVar(&serveMockFile, "mock-file", "", "Load mock rules from this file")
	serveCmd.Flags().StringVar(&serveIntercept, "intercept", "", "Intercept requests matching this URL pattern (use * for all)")
	serveCmd.Flags().DurationVar(&serveInterceptTimeout, "intercept-timeout", 5*time.Minute, "Auto-drop intercepted requests after this duration")
	serveCmd.Flags().StringVar(&serveOnCapture, "on-capture", "", "Run this shell command for each new capture; capture JSON written to stdin")
	serveCmd.Flags().StringVar(&serveMode, "mode", "forward", "Proxy mode: forward or reverse")
	serveCmd.Flags().StringVar(&serveTarget, "target", "", "Reverse proxy target URL (required when --mode reverse)")
	serveCmd.Flags().StringArrayVar(&serveIgnore, "ignore", nil, "Skip capturing requests whose URL contains this substring (repeatable)")
	serveCmd.Flags().StringArrayVar(&serveMapRemote, "map-remote", nil, "Redirect host to a different base URL: source-host=http://target (repeatable)")
	serveCmd.Flags().StringArrayVar(&serveRewriteBody, "rewrite-body", nil, "Rewrite response bodies: regex=replacement (repeatable)")
	serveCmd.Flags().BoolVar(&serveNoStore, "no-store", false, "Disable disk persistence (captures held in memory only)")
	serveCmd.Flags().Int64Var(&serveMaxBodySize, "max-body-size", 0, "Truncate captured bodies at this byte limit (0 = no limit)")
	serveCmd.Flags().DurationVar(&serveDelay, "delay", 0, "Add artificial latency to every response (e.g. 200ms, 1s)")
	serveCmd.Flags().Float64Var(&serveChaos, "chaos", 0, "Randomly drop this percentage of requests (0-100)")
	serveCmd.Flags().BoolVar(&serveBrowser, "browser", false, "Launch system browser with proxy pre-configured after start")
	serveCmd.Flags().StringArrayVar(&serveShadow, "shadow", nil, "Mirror traffic to this URL silently (repeatable)")
	serveCmd.Flags().StringArrayVar(&servePlugins, "plugin", nil, "Run plugin command per capture; capture JSON on stdin (repeatable)")
	serveCmd.Flags().BoolVar(&serveWeb, "web", false, "Start web dashboard")
	serveCmd.Flags().StringVar(&serveWebPort, "web-port", "8080", "Port for web dashboard (default: 8080)")
	serveCmd.Flags().BoolVar(&serveNoConfig, "no-config", false, "Ignore ~/.snare/config.yaml")
	serveCmd.Flags().StringArrayVar(&serveProtoFiles, "proto", nil, "Protobuf definition file for gRPC decoding; decoded fields shown instead of raw bytes (repeatable)")
	serveCmd.Flags().BoolVar(&serveNoH3, "no-h3", false, "Disable HTTP/3 (QUIC) server in reverse proxy mode")
}

func runServe(cmd *cobra.Command, args []string) error {
	if !serveNoConfig {
		if err := applyFileConfig(cmd); err != nil {
			return fmt.Errorf("config file: %w", err)
		}
	}

	port, err := parsePort(servePort)
	if err != nil {
		return err
	}
	servePort = port

	if serveMode != "forward" && serveMode != "reverse" {
		return fmt.Errorf("--mode must be forward or reverse, got %q", serveMode)
	}

	var reverseTarget *url.URL
	if serveMode == "reverse" {
		if serveTarget == "" {
			return fmt.Errorf("--target is required when --mode reverse")
		}
		reverseTarget, err = url.Parse(serveTarget)
		if err != nil || reverseTarget.Host == "" {
			return fmt.Errorf("invalid --target URL: %s", serveTarget)
		}
	}

	storeDir := serveStoreDir
	if !serveNoStore {
		if storeDir == "" {
			storeDir = config.StoreDir()
		}
		if err := os.MkdirAll(storeDir, 0700); err != nil {
			return err
		}
	} else {
		storeDir = ""
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

	mapRemotes, err := parseMapRemoteRules(serveMapRemote)
	if err != nil {
		return err
	}

	bodyRewrites, err := parseBodyRewrites(serveRewriteBody)
	if err != nil {
		return err
	}

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

	var interceptQueue *intercept.Queue
	if serveIntercept != "" {
		interceptQueue = intercept.NewQueue(config.InterceptDir())
		log.Info("intercept enabled", "match", serveIntercept, "dir", config.InterceptDir())
	}

	var hostCerts *cert.HostCertCache
	mitmEnable := !serveNoMITM && serveMode == "forward"
	if mitmEnable {
		ca, key, caErr := cert.LoadOrCreateCA(config.CADir())
		if caErr != nil {
			log.Warn("CA load failed, MITM disabled", "err", caErr)
			mitmEnable = false
		} else {
			hostCerts = cert.NewHostCertCache(ca, key)
		}
	}

	shadows, err := parseShadowURLs(serveShadow)
	if err != nil {
		return err
	}

	var webSrv *web.Server
	var onCapture func(*capture.Capture)
	baseOnCapture := buildOnCapture(serveOnCapture)
	if serveWeb {
		webSrv = &web.Server{Store: store, Mocks: mocks, Transport: transport, Intercept: interceptQueue, CADir: config.CADir(), WebPort: serveWebPort, ProxyAddr: serveBind + ":" + servePort}
		onCapture = func(c *capture.Capture) {
			if baseOnCapture != nil {
				baseOnCapture(c)
			}
			webSrv.NotifyCapture(c)
		}
	} else {
		onCapture = baseOnCapture
	}

	var protoDecoder *proxy.ProtoDecoder
	if len(serveProtoFiles) > 0 {
		pd, pdErr := proxy.NewProtoDecoder(serveProtoFiles)
		if pdErr != nil {
			log.Warn("proto decode disabled", "err", pdErr)
		} else {
			protoDecoder = pd
		}
	}

	handler := &proxy.Handler{
		Transport:        transport,
		Store:            store,
		Mocks:            mocks,
		Intercept:        interceptQueue,
		InterceptMatch:   serveIntercept,
		InterceptTimeout: serveInterceptTimeout,
		HostCerts:        hostCerts,
		Log:              log,
		MitmEnable:       mitmEnable,
		HostRewrites:     rewrites,
		AddHeaders:       addHeaders,
		RemoveHeaders:    removeHeaders,
		OnCapture:        onCapture,
		IgnorePatterns:   serveIgnore,
		MapRemotes:       mapRemotes,
		BodyRewrites:     bodyRewrites,
		MaxBodySize:      serveMaxBodySize,
		Mode:             serveMode,
		ReverseTarget:    reverseTarget,
		Delay:            serveDelay,
		ChaosRate:        serveChaos,
		Shadows:          shadows,
		Plugins:          servePlugins,
		ProtoDecoder:     protoDecoder,
	}

	addr := serveBind + ":" + servePort
	srv, err := proxy.NewServer(addr, handler, log)
	if err != nil {
		return err
	}

	if serveMode == "reverse" {
		log.Info("reverse proxy listening", "addr", addr, "target", serveTarget)
		if !serveNoH3 && hostCerts != nil && reverseTarget != nil {
			hostname := reverseTarget.Hostname()
			if hostname == "" {
				hostname = "localhost"
			}
			rawCert, privKey, certErr := hostCerts.GetCertificate(hostname)
			if certErr == nil {
				tlsCfg := &tls.Config{
					Certificates: []tls.Certificate{{
						Certificate: [][]byte{rawCert.Raw},
						PrivateKey:  privKey,
					}},
				}
				srv.StartH3(handler, tlsCfg, log)
			} else {
				log.Warn("H3: cert generation failed, HTTP/3 disabled", "err", certErr)
			}
		}
	} else {
		log.Info("proxy listening", "addr", addr, "mitm", mitmEnable)
		log.Info("set HTTP_PROXY and HTTPS_PROXY to http://" + addr)
	}
	if storeDir != "" {
		log.Info("captures saved to", "dir", storeDir)
	}
	if serveBind == "0.0.0.0" {
		for _, ip := range localIPs() {
			log.Info("external clients can use", "url", "http://"+ip+":"+servePort)
		}
	}
	srv.Start()

	dashURL := ""
	if serveWeb {
		webPort, err := parsePort(serveWebPort)
		if err != nil {
			return fmt.Errorf("--web-port: %w", err)
		}
		webAddr := serveBind + ":" + webPort
		webHTTP := &http.Server{Addr: webAddr, Handler: webSrv.Handler()}
		go func() { _ = webHTTP.ListenAndServe() }()
		dashURL = "http://127.0.0.1:" + webPort
		log.Info("web dashboard", "addr", dashURL)
		go openURL(dashURL)
	}

	if serveBrowser {
		target := dashURL
		if target == "" {
			target = "about:blank"
		}
		go openBrowser("http://"+addr, target, log)
	}

	runSession := "run-" + time.Now().Format("2006-01-02T15:04:05")
	if sessions, err := sess.Load(); err == nil {
		sessions[runSession] = sess.Entry{Start: time.Now()}
		_ = sess.Save(sessions)
		log.Info("run session started", "name", runSession)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("shutting down ...")

	if sessions, err := sess.Load(); err == nil {
		if e, ok := sessions[runSession]; ok {
			e.End = time.Now()
			sessions[runSession] = e
			_ = sess.Save(sessions)
		}
	}

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

func parseMapRemoteRules(items []string) ([]proxy.MapRemoteRule, error) {
	out := make([]proxy.MapRemoteRule, 0, len(items))
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --map-remote value %q (expected source-host=http://target)", item)
		}
		src := strings.TrimSpace(parts[0])
		tgt := strings.TrimSpace(parts[1])
		if src == "" || tgt == "" {
			return nil, fmt.Errorf("invalid --map-remote value %q", item)
		}
		u, err := url.Parse(tgt)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("invalid --map-remote target URL %q: %v", tgt, err)
		}
		out = append(out, proxy.MapRemoteRule{SourceHost: src, TargetBase: u})
	}
	return out, nil
}

func parseBodyRewrites(items []string) ([]proxy.BodyRewrite, error) {
	out := make([]proxy.BodyRewrite, 0, len(items))
	for _, item := range items {
		idx := strings.Index(item, "=")
		if idx < 1 {
			return nil, fmt.Errorf("invalid --rewrite-body value %q (expected regex=replacement)", item)
		}
		pattern := item[:idx]
		replacement := item[idx+1:]
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid --rewrite-body pattern %q: %v", pattern, err)
		}
		out = append(out, proxy.BodyRewrite{Pattern: re, Replacement: []byte(replacement)})
	}
	return out, nil
}

func buildOnCapture(cmd string) func(*capture.Capture) {
	if cmd == "" {
		return nil
	}
	return func(c *capture.Capture) {
		data, err := json.Marshal(c)
		if err != nil {
			return
		}
		data = append(data, '\n')
		var proc *exec.Cmd
		if runtime.GOOS == "windows" {
			proc = exec.Command("cmd", "/C", cmd)
		} else {
			proc = exec.Command("sh", "-c", cmd)
		}
		proc.Stdin = bytes.NewReader(data)
		proc.Stdout = os.Stderr
		proc.Stderr = os.Stderr
		_ = proc.Run()
	}
}

func openURL(url string) {
	time.Sleep(400 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/C", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func openBrowser(proxyAddr, targetURL string, log *slog.Logger) {
	time.Sleep(300 * time.Millisecond)
	proxyFlag := "--proxy-server=" + proxyAddr
	var candidates [][]string
	switch runtime.GOOS {
	case "windows":
		candidates = [][]string{
			{"cmd", "/C", "start", "chrome", proxyFlag, targetURL},
			{"cmd", "/C", "start", "msedge", proxyFlag, targetURL},
		}
	case "darwin":
		candidates = [][]string{
			{"open", "-a", "Google Chrome", "--args", proxyFlag, targetURL},
			{"open", "-a", "Microsoft Edge", "--args", proxyFlag, targetURL},
			{"open", "-a", "Chromium", "--args", proxyFlag, targetURL},
		}
	default:
		candidates = [][]string{
			{"google-chrome", proxyFlag, targetURL},
			{"chromium-browser", proxyFlag, targetURL},
			{"chromium", proxyFlag, targetURL},
			{"microsoft-edge", proxyFlag, targetURL},
		}
	}
	for _, args := range candidates {
		cmd := exec.Command(args[0], args[1:]...)
		if cmd.Start() == nil {
			log.Info("browser launched with proxy", "proxy", proxyAddr)
			return
		}
	}
	log.Warn("could not find a supported browser; set proxy manually", "proxy", proxyAddr)
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

func parseShadowURLs(items []string) ([]*url.URL, error) {
	out := make([]*url.URL, 0, len(items))
	for _, s := range items {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("invalid --shadow URL %q", s)
		}
		out = append(out, u)
	}
	return out, nil
}

func applyFileConfig(cmd *cobra.Command) error {
	cfg, err := config.LoadFileConfig()
	if err != nil {
		return err
	}
	set := func(name, val string) {
		if val != "" && !cmd.Flags().Changed(name) {
			_ = cmd.Flags().Set(name, val)
		}
	}
	setBool := func(name string, val bool) {
		if val && !cmd.Flags().Changed(name) {
			_ = cmd.Flags().Set(name, "true")
		}
	}
	setSlice := func(name string, vals []string) {
		if len(vals) > 0 && !cmd.Flags().Changed(name) {
			for _, v := range vals {
				_ = cmd.Flags().Set(name, v)
			}
		}
	}
	set("port", cfg.Port)
	set("bind", cfg.Bind)
	set("mode", cfg.Mode)
	set("target", cfg.Target)
	set("upstream-proxy", cfg.UpstreamProxy)
	set("mock-file", cfg.MockFile)
	set("intercept", cfg.Intercept)
	set("intercept-timeout", cfg.InterceptTimeout)
	set("on-capture", cfg.OnCapture)
	set("delay", cfg.Delay)
	set("web-port", cfg.WebPort)
	setBool("no-mitm", cfg.NoMITM)
	setBool("verbose", cfg.Verbose)
	setBool("web", cfg.Web)
	if cfg.MaxCaptures > 0 && !cmd.Flags().Changed("max-captures") {
		_ = cmd.Flags().Set("max-captures", fmt.Sprint(cfg.MaxCaptures))
	}
	if cfg.MaxBodySize > 0 && !cmd.Flags().Changed("max-body-size") {
		_ = cmd.Flags().Set("max-body-size", fmt.Sprint(cfg.MaxBodySize))
	}
	if cfg.Chaos > 0 && !cmd.Flags().Changed("chaos") {
		_ = cmd.Flags().Set("chaos", fmt.Sprintf("%g", cfg.Chaos))
	}
	setSlice("rewrite-host", cfg.RewriteHost)
	setSlice("add-header", cfg.AddHeader)
	setSlice("remove-header", cfg.RemoveHeader)
	setSlice("ignore", cfg.Ignore)
	setSlice("map-remote", cfg.MapRemote)
	setSlice("rewrite-body", cfg.RewriteBody)
	setSlice("shadow", cfg.Shadow)
	setSlice("plugin", cfg.Plugins)
	return nil
}
