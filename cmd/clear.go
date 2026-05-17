package cmd

import (
	"fmt"
	"time"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"

	"github.com/spf13/cobra"
)

var (
	clearMethod string
	clearStatus int
	clearURL    string
	clearHost   string
)

var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear captured requests",
	Long:  "Delete captures from the store. Without filters, all captures are removed. With filters, only matching captures are deleted.",
	RunE:  runClear,
}

func init() {
	clearCmd.Flags().StringVar(&clearMethod, "method", "", "Delete only captures with this HTTP method")
	clearCmd.Flags().IntVar(&clearStatus, "status", 0, "Delete only captures with this response status code")
	clearCmd.Flags().StringVar(&clearURL, "url", "", "Delete only captures whose URL contains this substring")
	clearCmd.Flags().StringVar(&clearHost, "host", "", "Delete only captures matching this host")
}

func runClear(cmd *cobra.Command, args []string) error {
	hasFilter := clearMethod != "" || clearStatus != 0 || clearURL != "" || clearHost != ""

	store := capture.NewStore(0, config.StoreDir())

	if !hasFilter {
		store.Clear(true)
		fmt.Println("Cleared all captures.")
		return nil
	}

	all := store.AllFromDisk()
	matches := filterCaptures(all, clearMethod, clearStatus, clearURL, clearHost, "", time.Time{}, time.Time{})
	if len(matches) == 0 {
		fmt.Println("No captures matched the filter.")
		return nil
	}
	for _, c := range matches {
		store.DeleteByID(c.ID)
	}
	fmt.Printf("Deleted %d capture(s).\n", len(matches))
	return nil
}
