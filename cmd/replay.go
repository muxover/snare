package cmd

import (
	"bytes"
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

var replayRepeat int
var replayURL string

var replayCmd = &cobra.Command{
	Use:   "replay [id]",
	Short: "Replay a captured request",
	Long:  "Re-send the captured request to the same (or override) URL. Does not use the proxy; sends directly. Use -u to override the URL, -n to repeat.",
	Args:  cobra.ExactArgs(1),
	RunE:  runReplay,
}

func init() {
	replayCmd.Flags().IntVarP(&replayRepeat, "repeat", "n", 1, "Number of times to repeat")
	replayCmd.Flags().StringVarP(&replayURL, "url", "u", "", "Override URL (optional)")
}

func runReplay(cmd *cobra.Command, args []string) error {
	id := args[0]
	store := capture.NewStore(0, config.StoreDir())
	c := store.GetByPrefix(id)
	if c == nil {
		return fmt.Errorf("capture not found: %s", id)
	}
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
