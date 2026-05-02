package cmd

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var (
	replayRepeat int
	replayURL    string
	replayHeader []string
	replayMatch  string
)

var replayCmd = &cobra.Command{
	Use:   "replay [id]",
	Short: "Replay a captured request",
	Long:  "Re-send captured requests directly (not through the proxy). Provide a capture ID, or use --match to replay all captures whose URL contains the given substring.",
	Args:  cobra.ArbitraryArgs,
	RunE:  runReplay,
}

func init() {
	replayCmd.Flags().IntVarP(&replayRepeat, "repeat", "n", 1, "")
	replayCmd.Flags().StringVarP(&replayURL, "url", "u", "", "Override URL (optional)")
	replayCmd.Flags().StringSliceVarP(&replayHeader, "header", "H", nil, "Add or override header (Key: Value); can be repeated")
	replayCmd.Flags().StringVar(&replayMatch, "match", "", "Replay all captures whose URL contains this substring")
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
		if err := replayCapture(c); err != nil {
			return err
		}
		if idx < len(targets)-1 {
			fmt.Println()
		}
	}
	return nil
}

func replayCapture(c *capture.Capture) error {
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
		client := &http.Client{}
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
