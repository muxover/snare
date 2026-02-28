package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"os"

	"github.com/spf13/cobra"
)

var saveOutput string
var saveAll bool
var saveLastN int

var saveCmd = &cobra.Command{
	Use:   "save [id]",
	Short: "Save capture(s) to a JSON file",
	Long:  "Save one capture by ID to a JSON file, or use --all to save the last N captures to a single file. Output path is set with -o.",
	Args:  cobra.MinimumNArgs(0),
	RunE:  runSave,
}

func init() {
	saveCmd.Flags().StringVarP(&saveOutput, "output", "o", "", "Output file")
	saveCmd.Flags().BoolVar(&saveAll, "all", false, "Save all captures")
	saveCmd.Flags().IntVarP(&saveLastN, "last", "n", 10, "With --all, save last N captures")
}

func runSave(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	if saveAll {
		captures := store.ListFromDisk(saveLastN)
		if len(captures) == 0 {
			fmt.Println("No captures to save.")
			return nil
		}
		out := saveOutput
		if out == "" {
			out = "captures.json"
		}
		data, err := json.MarshalIndent(captures, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0644)
	}
	if len(args) == 0 {
		return fmt.Errorf("specify capture id or use --all")
	}
	id := args[0]
	c := store.GetByPrefix(id)
	if c == nil {
		return fmt.Errorf("capture not found: %s", id)
	}
	out := saveOutput
	if out == "" {
		out = c.ID + ".json"
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, data, 0644)
}
