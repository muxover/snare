package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"

	"github.com/spf13/cobra"
)

var (
	grepInvert bool
	grepMethod string
	grepHost   string
)

var grepCmd = &cobra.Command{
	Use:   "grep <pattern>",
	Short: "Search capture bodies for a pattern",
	Long:  "Search request and response bodies across all captures using a regular expression. Prints one line per matching capture.",
	Args:  cobra.ExactArgs(1),
	RunE:  runGrep,
}

func init() {
	grepCmd.Flags().BoolVarP(&grepInvert, "invert", "v", false, "Print captures that do NOT match the pattern")
	grepCmd.Flags().StringVar(&grepMethod, "method", "", "Limit to this HTTP method")
	grepCmd.Flags().StringVar(&grepHost, "host", "", "Limit to this host")
}

func runGrep(cmd *cobra.Command, args []string) error {
	re, err := regexp.Compile(args[0])
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}

	store := capture.NewStore(0, config.StoreDir())
	all := store.AllFromDisk()

	count := 0
	for _, c := range all {
		if grepMethod != "" && !strings.EqualFold(c.Request.Method, grepMethod) {
			continue
		}
		if grepHost != "" {
			host := hostFromURL(c.Request.URL)
			if !strings.EqualFold(host, grepHost) {
				continue
			}
		}

		matched := bodyMatches(re, c)
		if grepInvert {
			matched = !matched
		}
		if matched {
			printCaptureLine(c)
			count++
		}
	}

	if count == 0 {
		fmt.Println("no matches")
	}
	return nil
}

func bodyMatches(re *regexp.Regexp, c *capture.Capture) bool {
	if re.Match(c.Request.Body) {
		return true
	}
	if c.Response != nil && re.Match(c.Response.Body) {
		return true
	}
	return false
}

func hostFromURL(rawURL string) string {
	if idx := strings.Index(rawURL, "://"); idx >= 0 {
		rest := rawURL[idx+3:]
		if end := strings.IndexAny(rest, "/?#"); end >= 0 {
			return rest[:end]
		}
		return rest
	}
	if idx := strings.IndexByte(rawURL, '/'); idx >= 0 {
		return rawURL[:idx]
	}
	return rawURL
}
