package cmd

import (
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var listLast int

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent captures",
	Long:  "List captures from the store directory (same as used by serve). Reads from disk so it works even when the proxy is not running.",
	RunE:  runList,
}

func init() {
	listCmd.Flags().IntVarP(&listLast, "last", "n", 20, "Number of captures to show")
}

func runList(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	captures := store.ListFromDisk(listLast)
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
