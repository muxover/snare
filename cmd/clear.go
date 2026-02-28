package cmd

import (
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all captured requests",
	Long:  "Delete all captures from the store directory (all .json files in SNARE_STORE). Irreversible.",
	RunE:  runClear,
}

func runClear(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	store.Clear(true)
	fmt.Println("Cleared all captures.")
	return nil
}
