package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/spf13/cobra"
)

var (
	assertMethod string
	assertStatus int
	assertURL    string
	assertBody   string
	assertMin    int
	assertMax    int
)

var assertCmd = &cobra.Command{
	Use:   "assert",
	Short: "Assert capture conditions; exits 1 if they are not met",
	Long:  "Filter captures using the same flags as 'list', then assert the matched count is within --min/--max bounds. Exits 0 on success, 1 on failure. Designed for use in CI pipelines.",
	RunE:  runAssert,
}

func init() {
	assertCmd.Flags().StringVar(&assertMethod, "method", "", "Filter by HTTP method")
	assertCmd.Flags().IntVar(&assertStatus, "status", 0, "Filter by response status code")
	assertCmd.Flags().StringVar(&assertURL, "url", "", "Filter by URL substring")
	assertCmd.Flags().StringVar(&assertBody, "body", "", "Filter by substring in request or response body")
	assertCmd.Flags().IntVar(&assertMin, "min", 1, "Minimum number of matching captures (inclusive)")
	assertCmd.Flags().IntVar(&assertMax, "max", -1, "Maximum number of matching captures (-1 = no limit)")
}

func runAssert(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	captures := store.AllFromDisk()
	matched := filterCaptures(captures, assertMethod, assertStatus, assertURL, "", assertBody, time.Time{}, time.Time{})
	n := len(matched)

	conditions := buildAssertLabel()
	if assertMin > 0 && n < assertMin {
		return fmt.Errorf("assert failed: %d capture(s) match %q, want at least %d", n, conditions, assertMin)
	}
	if assertMax >= 0 && n > assertMax {
		return fmt.Errorf("assert failed: %d capture(s) match %q, want at most %d", n, conditions, assertMax)
	}
	fmt.Printf("ok: %d capture(s) match %q\n", n, conditions)
	return nil
}

func buildAssertLabel() string {
	var parts []string
	if assertMethod != "" {
		parts = append(parts, "method="+assertMethod)
	}
	if assertStatus != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", assertStatus))
	}
	if assertURL != "" {
		parts = append(parts, "url="+assertURL)
	}
	if assertBody != "" {
		parts = append(parts, "body="+assertBody)
	}
	if len(parts) == 0 {
		return "*"
	}
	return strings.Join(parts, " ")
}
