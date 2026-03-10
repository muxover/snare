package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var (
	listLast   int
	listMethod string
	listStatus int
	listURL    string
	listHost   string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent captures",
	Long:  "List captures from the store directory (same as used by serve). Reads from disk so it works even when the proxy is not running. Optional filters: --method, --status, --url, --host.",
	RunE:  runList,
}

func init() {
	listCmd.Flags().IntVarP(&listLast, "last", "n", 20, "Number of captures to show")
	listCmd.Flags().StringVar(&listMethod, "method", "", "Filter by HTTP method (e.g. GET, POST)")
	listCmd.Flags().IntVar(&listStatus, "status", 0, "Filter by response status code (e.g. 200, 404)")
	listCmd.Flags().StringVar(&listURL, "url", "", "Filter by URL substring")
	listCmd.Flags().StringVar(&listHost, "host", "", "Filter by host (URL host part)")
}

func runList(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	loadN := listLast
	if loadN <= 0 {
		loadN = 100
	}
	hasFilter := listMethod != "" || listStatus != 0 || listURL != "" || listHost != ""
	if hasFilter {
		loadN = 500
	}
	captures := store.ListFromDisk(loadN)
	if hasFilter {
		captures = filterCaptures(captures, listMethod, listStatus, listURL, listHost)
		if listLast > 0 && len(captures) > listLast {
			captures = captures[:listLast]
		}
	}
	if len(captures) == 0 {
		storeDir := config.StoreDir()
		fmt.Println("No captures found.")
		fmt.Println("  Reading from:", storeDir)
		fmt.Println("  1. Start snare: snare serve --bind 0.0.0.0")
		fmt.Println("  2. Send traffic through it (use HTTP first): curl -x http://127.0.0.1:8888 http://example.com")
		fmt.Println("  3. Run 'snare list' again.")
		return nil
	}
	for _, c := range captures {
		status := "-"
		if c.Response != nil {
			status = fmt.Sprintf("%d", c.Response.StatusCode)
		}
		if c.Error != "" {
			status = "err"
		}
		idShort := c.ID
		if len(idShort) > 8 {
			idShort = idShort[:8]
		}
		fmt.Printf("%s  %s  %s  %s  %s\n", idShort, c.Timestamp.Format("15:04:05"), c.Request.Method, status, c.Request.URL)
	}
	return nil
}

func filterCaptures(captures []*capture.Capture, method string, status int, urlSub, host string) []*capture.Capture {
	var out []*capture.Capture
	for _, c := range captures {
		if method != "" && c.Request.Method != method {
			continue
		}
		if status != 0 {
			if c.Response == nil || c.Response.StatusCode != status {
				continue
			}
		}
		if urlSub != "" && !strings.Contains(c.Request.URL, urlSub) {
			continue
		}
		if host != "" {
			u, err := url.Parse(c.Request.URL)
			if err != nil || !strings.Contains(u.Host, host) {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}
