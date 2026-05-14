package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/muxover/snare/proxy"
	"github.com/muxover/snare/proxy/cert"
	"github.com/spf13/cobra"
)

var (
	recordOut     string
	recordPort    string
	recordBind    string
	recordNoMITM  bool
	recordVerbose bool
	recordMode    string
	recordTarget  string
)

var recordCmd = &cobra.Command{
	Use:   "record",
	Short: "Record traffic to a cassette file",
	Long:  "Start the proxy and save all captures to a cassette file for later offline playback with 'snare playback'.",
	RunE:  runRecord,
}

func init() {
	recordCmd.Flags().StringVarP(&recordOut, "out", "o", "cassette.json", "Cassette output file")
	recordCmd.Flags().StringVarP(&recordPort, "port", "p", "8888", "Port to listen on")
	recordCmd.Flags().StringVarP(&recordBind, "bind", "b", "127.0.0.1", "Address to bind")
	recordCmd.Flags().BoolVar(&recordNoMITM, "no-mitm", false, "Disable HTTPS MITM")
	recordCmd.Flags().BoolVarP(&recordVerbose, "verbose", "v", false, "Enable debug logging")
	recordCmd.Flags().StringVar(&recordMode, "mode", "forward", "Proxy mode: forward or reverse")
	recordCmd.Flags().StringVar(&recordTarget, "target", "", "Reverse proxy target URL (required for --mode reverse)")
}

type cassetteWriter struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

func newCassetteWriter(path string) (*cassetteWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	return &cassetteWriter{f: f, path: path}, nil
}

func (cw *cassetteWriter) write(c *capture.Capture) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	cw.f.Write(data)
	cw.f.Write([]byte("\n"))
}

func (cw *cassetteWriter) close() {
	cw.f.Close()
}

func runRecord(cmd *cobra.Command, args []string) error {
	port, err := parsePort(recordPort)
	if err != nil {
		return err
	}

	cw, err := newCassetteWriter(recordOut)
	if err != nil {
		return fmt.Errorf("cannot open cassette file: %w", err)
	}
	defer cw.close()

	store := capture.NewStore(1000, config.StoreDir())

	logLevel := slog.LevelInfo
	if recordVerbose {
		logLevel = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	transport, err := proxy.ProxyTransport(true, "")
	if err != nil {
		return err
	}

	var hostCerts *cert.HostCertCache
	mitmEnable := !recordNoMITM && recordMode == "forward"
	if mitmEnable {
		ca, key, caErr := cert.LoadOrCreateCA(config.CADir())
		if caErr != nil {
			log.Warn("CA load failed, MITM disabled", "err", caErr)
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
		Mode:       recordMode,
		OnCapture:  cw.write,
	}

	if recordMode == "reverse" {
		if recordTarget == "" {
			return fmt.Errorf("--target is required when --mode reverse")
		}
		u, err := url.Parse(recordTarget)
		if err != nil || u.Host == "" {
			return fmt.Errorf("invalid --target URL: %s", recordTarget)
		}
		handler.ReverseTarget = u
	}

	addr := recordBind + ":" + port
	srv, err := proxy.NewServer(addr, handler, log)
	if err != nil {
		return err
	}

	log.Info("recording proxy listening", "addr", addr, "cassette", recordOut)
	srv.Start()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
