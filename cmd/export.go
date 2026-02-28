package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var exportFormat string
var exportLast int

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export captures to HAR or JSON",
	Long:  "Export the last N captures to a single file. Format: json (default) or har. Output: export.json or export.har.",
	RunE:  runExport,
}

func init() {
	exportCmd.Flags().StringVarP(&exportFormat, "format", "f", "json", "Format: json or har")
	exportCmd.Flags().IntVarP(&exportLast, "last", "n", 50, "Number of captures to export")
}

func runExport(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	captures := store.ListFromDisk(exportLast)
	if len(captures) == 0 {
		fmt.Println("No captures to export.")
		return nil
	}
	out := "export." + exportFormat
	if exportFormat == "har" {
		har := buildHAR(captures)
		data, err := json.MarshalIndent(har, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0644)
	}
	data, err := json.MarshalIndent(captures, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, data, 0644)
}

func buildHAR(captures []*capture.Capture) map[string]interface{} {
	entries := make([]map[string]interface{}, 0, len(captures))
	for _, c := range captures {
		ent := map[string]interface{}{
			"startedDateTime": c.Timestamp.Format(time.RFC3339),
			"time":            c.Duration.Milliseconds(),
			"request": map[string]interface{}{
				"method":   c.Request.Method,
				"url":      c.Request.URL,
				"headers":  headersToHAR(c.Request.Headers),
				"bodySize": len(c.Request.Body),
			},
		}
		if c.Response != nil {
			ent["response"] = map[string]interface{}{
				"status":   c.Response.StatusCode,
				"headers":  headersToHAR(c.Response.Headers),
				"bodySize": len(c.Response.Body),
			}
		}
		entries = append(entries, ent)
	}
	return map[string]interface{}{
		"log": map[string]interface{}{
			"version": "1.2",
			"creator": map[string]interface{}{"name": "snare", "version": "1.0"},
			"entries": entries,
		},
	}
}

func headersToHAR(h map[string][]string) []map[string]string {
	var out []map[string]string
	for k, v := range h {
		for _, vv := range v {
			out = append(out, map[string]string{"name": k, "value": vv})
		}
	}
	return out
}
