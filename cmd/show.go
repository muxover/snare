package cmd

import (
	"fmt"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show full request/response for a capture",
	Long:  "Print the full request (method, URL, headers, body) and response (status, headers, body) for a capture. ID can be the full UUID or a unique prefix (e.g. first 8 characters).",
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	id := args[0]
	store := capture.NewStore(0, config.StoreDir())
	c := store.GetByPrefix(id)
	if c == nil {
		return fmt.Errorf("capture not found: %s", id)
	}
	fmt.Println("=== Request ===")
	fmt.Printf("%s %s\n", c.Request.Method, c.Request.URL)
	for k, v := range c.Request.Headers {
		for _, vv := range v {
			fmt.Printf("%s: %s\n", k, vv)
		}
	}
	if len(c.Request.Body) > 0 {
		fmt.Println()
		fmt.Println(string(c.Request.Body))
	}
	fmt.Println("\n=== Response ===")
	if c.Response != nil {
		fmt.Printf("Status: %d\n", c.Response.StatusCode)
		for k, v := range c.Response.Headers {
			for _, vv := range v {
				fmt.Printf("%s: %s\n", k, vv)
			}
		}
		if len(c.Response.Body) > 0 {
			fmt.Println()
			fmt.Println(string(c.Response.Body))
		}
	} else if c.Error != "" {
		fmt.Println("Error:", c.Error)
	}
	fmt.Printf("\nDuration: %s\n", c.Duration)
	return nil
}
