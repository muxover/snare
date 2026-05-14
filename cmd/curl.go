package cmd

import (
	"fmt"
	"strings"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var curlCmd = &cobra.Command{
	Use:   "curl <id>",
	Short: "Print a capture as a curl command",
	Long:  "Print a captured request formatted as a ready-to-run curl command.",
	Args:  cobra.ExactArgs(1),
	RunE:  runCurl,
}

var skipCurlHeaders = map[string]bool{
	"Content-Length":    true,
	"Transfer-Encoding": true,
	"Connection":        true,
	"Proxy-Connection":  true,
}

func runCurl(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	c := store.GetByPrefix(args[0])
	if c == nil {
		return fmt.Errorf("capture not found: %s", args[0])
	}
	fmt.Println(buildCurlCmd(c))
	return nil
}

func buildCurlCmd(c *capture.Capture) string {
	var b strings.Builder
	b.WriteString("curl")

	if c.Request.Method != "GET" {
		b.WriteString(" -X " + c.Request.Method)
	}

	b.WriteString(" '" + strings.ReplaceAll(c.Request.URL, "'", "'\\''") + "'")

	for k, vals := range c.Request.Headers {
		if skipCurlHeaders[k] {
			continue
		}
		for _, v := range vals {
			b.WriteString(" \\\n  -H '" + k + ": " + strings.ReplaceAll(v, "'", "'\\''") + "'")
		}
	}

	if len(c.Request.Body) > 0 {
		body := strings.ReplaceAll(string(c.Request.Body), "'", "'\\''")
		b.WriteString(" \\\n  --data '" + body + "'")
	}

	return b.String()
}
