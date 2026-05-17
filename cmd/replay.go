package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"

	"github.com/spf13/cobra"
)

var (
	replayRepeat int
	replayURL    string
	replayHeader []string
	replayMatch  string
	replayEdit   bool
	replayProxy  string
)

var replayCmd = &cobra.Command{
	Use:   "replay [id]",
	Short: "Replay a captured request",
	Long:  "Re-send captured requests through the snare proxy so the replay is captured and inspectable. Provide a capture ID, or use --match to replay all captures whose URL contains the given substring.",
	Args:  cobra.ArbitraryArgs,
	RunE:  runReplay,
}

func init() {
	replayCmd.Flags().IntVarP(&replayRepeat, "repeat", "n", 1, "")
	replayCmd.Flags().StringVarP(&replayURL, "url", "u", "", "Override URL (optional)")
	replayCmd.Flags().StringSliceVarP(&replayHeader, "header", "H", nil, "Add or override header (Key: Value); can be repeated")
	replayCmd.Flags().StringVar(&replayMatch, "match", "", "Replay all captures whose URL contains this substring")
	replayCmd.Flags().BoolVar(&replayEdit, "edit", false, "Open capture in $EDITOR before resending")
	replayCmd.Flags().StringVar(&replayProxy, "proxy", "http://127.0.0.1:8888", "Proxy URL to route replay through (set to empty to skip capturing)")
}

func runReplay(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	if replayRepeat < 1 {
		return fmt.Errorf("--repeat must be at least 1")
	}

	var targets []*capture.Capture
	if replayMatch != "" {
		if len(args) > 0 {
			return fmt.Errorf("use either capture id or --match, not both")
		}
		for _, c := range store.AllFromDisk() {
			if strings.Contains(c.Request.URL, replayMatch) {
				targets = append(targets, c)
			}
		}
		if len(targets) == 0 {
			return fmt.Errorf("no captures matched: %s", replayMatch)
		}
	} else {
		if len(args) != 1 {
			return fmt.Errorf("provide capture id or use --match")
		}
		id := args[0]
		c := store.GetByPrefix(id)
		if c == nil {
			return fmt.Errorf("capture not found: %s", id)
		}
		targets = []*capture.Capture{c}
	}

	for idx, c := range targets {
		if len(targets) > 1 {
			short := c.ID
			if len(short) > 8 {
				short = short[:8]
			}
			fmt.Printf("Replaying [%s] %s %s\n", short, c.Request.Method, c.Request.URL)
		}
		if replayEdit {
			edited, err := editCapture(c)
			if err != nil {
				return err
			}
			c = edited
		}
		if err := replayCaptureOnce(c); err != nil {
			return err
		}
		if idx < len(targets)-1 {
			fmt.Println()
		}
	}
	return nil
}

func editCapture(c *capture.Capture) (*capture.Capture, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		return nil, fmt.Errorf("$EDITOR is not set")
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "snare-edit-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	proc := exec.Command(editor, tmp.Name())
	proc.Stdin = os.Stdin
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr
	if err := proc.Run(); err != nil {
		return nil, fmt.Errorf("editor exited with error: %w", err)
	}

	edited, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil, err
	}

	var out capture.Capture
	if err := json.Unmarshal(edited, &out); err != nil {
		return nil, fmt.Errorf("edited JSON is invalid: %w", err)
	}
	return &out, nil
}

func replayCaptureOnce(c *capture.Capture) error {
	urlStr := c.Request.URL
	if replayURL != "" {
		urlStr = replayURL
	}
	for i := 0; i < replayRepeat; i++ {
		req, err := http.NewRequest(c.Request.Method, urlStr, bytes.NewReader([]byte(c.Request.Body)))
		if err != nil {
			return err
		}
		for k, v := range c.Request.Headers {
			for _, vv := range v {
				req.Header.Add(k, vv)
			}
		}
		for _, h := range replayHeader {
			if idx := strings.Index(h, ":"); idx > 0 {
				key := strings.TrimSpace(h[:idx])
				val := strings.TrimSpace(h[idx+1:])
				if key != "" {
					req.Header.Set(key, val)
				}
			}
		}
		var transport http.RoundTripper
		if replayProxy != "" {
			proxyURL, err := url.Parse(replayProxy)
			if err != nil {
				return fmt.Errorf("invalid --proxy URL: %w", err)
			}
			transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		}
		client := &http.Client{Transport: transport}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("Response %d: %d %s\n", i+1, resp.StatusCode, resp.Status)
		fmt.Println(string(body))
		if i < replayRepeat-1 {
			fmt.Println("---")
		}
	}
	return nil
}

func replayCapture(c *capture.Capture) error {
	return replayCaptureOnce(c)
}
