package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/muxover/snare/v2/capture"
	"github.com/spf13/cobra"
)

var (
	playbackPort string
	playbackBind string
)

var playbackCmd = &cobra.Command{
	Use:   "playback <cassette>",
	Short: "Serve recorded responses from a cassette file",
	Long:  "Start an HTTP server that replays responses recorded by 'snare record'. Requests are matched by method and path.",
	Args:  cobra.ExactArgs(1),
	RunE:  runPlayback,
}

func init() {
	playbackCmd.Flags().StringVarP(&playbackPort, "port", "p", "8888", "Port to listen on")
	playbackCmd.Flags().StringVarP(&playbackBind, "bind", "b", "127.0.0.1", "Address to bind")
}

func runPlayback(cmd *cobra.Command, args []string) error {
	cassette, err := loadCassette(args[0])
	if err != nil {
		return err
	}
	if len(cassette) == 0 {
		return fmt.Errorf("cassette is empty: %s", args[0])
	}

	port, err := parsePort(playbackPort)
	if err != nil {
		return err
	}

	fmt.Printf("Loaded %d entries from %s\n", len(cassette), args[0])

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		entry := matchCassette(cassette, r.Method, r.URL)
		if entry == nil {
			http.Error(w, "no cassette match for "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		if entry.Response == nil {
			http.Error(w, "cassette entry has no response", http.StatusBadGateway)
			return
		}
		for k, vals := range entry.Response.Headers {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(entry.Response.StatusCode)
		_, _ = w.Write(entry.Response.Body)
	})

	addr := playbackBind + ":" + port
	srv := &http.Server{Addr: addr, Handler: mux}

	fmt.Printf("Playback server listening on http://%s\n", addr)
	fmt.Println("Press Ctrl+C to stop.")

	go func() { _ = srv.ListenAndServe() }()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func loadCassette(path string) ([]*capture.Capture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open cassette: %w", err)
	}
	defer f.Close()

	var out []*capture.Capture
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var c capture.Capture
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("invalid cassette line: %w", err)
		}
		out = append(out, &c)
	}
	return out, sc.Err()
}

func matchCassette(cassette []*capture.Capture, method string, u *url.URL) *capture.Capture {
	path := u.Path

	// Exact method + full URL
	for _, c := range cassette {
		if strings.EqualFold(c.Request.Method, method) && c.Request.URL == u.String() {
			return c
		}
	}
	// Method + path
	for _, c := range cassette {
		cu, err := url.Parse(c.Request.URL)
		if err != nil {
			continue
		}
		if strings.EqualFold(c.Request.Method, method) && cu.Path == path {
			return c
		}
	}
	// Path only (any method)
	for _, c := range cassette {
		cu, err := url.Parse(c.Request.URL)
		if err != nil {
			continue
		}
		if cu.Path == path {
			return c
		}
	}
	return nil
}
