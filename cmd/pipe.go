package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/spf13/cobra"
)

var (
	pipeFollow bool
	pipeMethod string
	pipeStatus int
	pipeURL    string
)

var pipeCmd = &cobra.Command{
	Use:   "pipe",
	Short: "Stream captures as newline-delimited JSON",
	Long:  "Output all captures from the store as NDJSON (one JSON object per line). Pipe to jq, grep, or any tool that reads JSON. Use --follow to stream new captures as they arrive.",
	RunE:  runPipe,
}

func init() {
	pipeCmd.Flags().BoolVarP(&pipeFollow, "follow", "f", false, "Poll for new captures and stream them as they arrive")
	pipeCmd.Flags().StringVar(&pipeMethod, "method", "", "Filter by HTTP method")
	pipeCmd.Flags().IntVar(&pipeStatus, "status", 0, "Filter by response status code")
	pipeCmd.Flags().StringVar(&pipeURL, "url", "", "Filter by URL substring")
}

func runPipe(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	enc := json.NewEncoder(os.Stdout)

	seen := map[string]bool{}

	emit := func(c *capture.Capture) error {
		if pipeMethod != "" && c.Request.Method != pipeMethod {
			return nil
		}
		if pipeStatus != 0 && (c.Response == nil || c.Response.StatusCode != pipeStatus) {
			return nil
		}
		if pipeURL != "" && !strings.Contains(c.Request.URL, pipeURL) {
			return nil
		}
		return enc.Encode(c)
	}

	captures := store.AllFromDisk()
	for i := len(captures) - 1; i >= 0; i-- {
		c := captures[i]
		seen[c.ID] = true
		if err := emit(c); err != nil {
			return err
		}
	}

	if !pipeFollow {
		return nil
	}

	for {
		time.Sleep(500 * time.Millisecond)
		fresh := store.AllFromDisk()
		for i := len(fresh) - 1; i >= 0; i-- {
			c := fresh[i]
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			if err := emit(c); err != nil {
				return err
			}
		}
	}
}

