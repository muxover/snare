package cmd

import (
	"fmt"
	"net/url"
	"strings"
	"time"

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
	listSince  string
	listUntil  string
	listBody   string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent captures",
	Long:  "List captures from the store directory (same as used by serve). Reads from disk so it works even when the proxy is not running. Filters: --method, --status, --url, --host, --since, --until, --body.",
	RunE:  runList,
}

func init() {
	listCmd.Flags().IntVarP(&listLast, "last", "n", 20, "")
	listCmd.Flags().StringVar(&listMethod, "method", "", "Filter by HTTP method (e.g. GET, POST)")
	listCmd.Flags().IntVar(&listStatus, "status", 0, "Filter by response status code (e.g. 200, 404)")
	listCmd.Flags().StringVar(&listURL, "url", "", "Filter by URL substring")
	listCmd.Flags().StringVar(&listHost, "host", "", "Filter by host (URL host part)")
	listCmd.Flags().StringVar(&listSince, "since", "", "Include captures at or after this time (RFC3339 or 2006-01-02)")
	listCmd.Flags().StringVar(&listUntil, "until", "", "Include captures at or before this time (RFC3339 or 2006-01-02)")
	listCmd.Flags().StringVar(&listBody, "body", "", "Filter by substring in request or response body")
}

func runList(cmd *cobra.Command, args []string) error {
	since, err := parseSinceFlag(listSince)
	if err != nil {
		return err
	}
	until, err := parseUntilFlag(listUntil)
	if err != nil {
		return err
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return fmt.Errorf("--until must be on or after --since")
	}

	store := capture.NewStore(0, config.StoreDir())

	hasFilter := listMethod != "" || listStatus != 0 || listURL != "" || listHost != "" ||
		listSince != "" || listUntil != "" || listBody != ""
	scanAll := listSince != "" || listUntil != "" || listBody != ""

	var captures []*capture.Capture
	if scanAll {
		captures = store.AllFromDisk()
	} else {
		loadN := listLast
		if loadN <= 0 {
			loadN = 100
		}
		if hasFilter {
			loadN = 500
		}
		captures = store.ListFromDisk(loadN)
	}

	captures = filterCaptures(captures, listMethod, listStatus, listURL, listHost, listBody, since, until)
	if listLast > 0 && len(captures) > listLast {
		captures = captures[:listLast]
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
		printCaptureLine(c)
	}
	return nil
}

func filterCaptures(captures []*capture.Capture, method string, status int, urlSub, host, bodySub string, since, until time.Time) []*capture.Capture {
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
		if !since.IsZero() && c.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && c.Timestamp.After(until) {
			continue
		}
		if bodySub != "" {
			inReq := strings.Contains(string(c.Request.Body), bodySub)
			inResp := c.Response != nil && strings.Contains(string(c.Response.Body), bodySub)
			if !inReq && !inResp {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}
