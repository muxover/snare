package cmd

import (
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete one capture",
	Long:  "Delete a single capture by ID or prefix. The capture file is removed from the store directory.",
	Args:  cobra.ExactArgs(1),
	RunE:  runDelete,
}

func runDelete(cmd *cobra.Command, args []string) error {
	id := args[0]
	store := capture.NewStore(0, config.StoreDir())
	c := store.GetByPrefix(id)
	if c == nil {
		return fmt.Errorf("capture not found: %s", id)
	}
	if err := store.DeleteByID(c.ID); err != nil {
		return err
	}
	fmt.Printf("Deleted capture %s\n", c.ID)
	return nil
}
