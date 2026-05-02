package cmd

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/muxover/snare/mock"
	"github.com/spf13/cobra"
)

var mockCmd = &cobra.Command{
	Use:   "mock",
	Short: "Manage mock rules",
	Long:  "Add, list, remove, and generate mock rules. Rules are matched in order; first match wins.",
}

var (
	mockAddMethod      string
	mockAddURL         string
	mockAddStatus      int
	mockAddBody        string
	mockAddContentType string
	mockAddHeader      []string
	mockAddName        string
)

var mockAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a mock rule",
	Long:  "Add a rule that intercepts matching requests and returns a fixed response. Use --method and --url to match; --status, --body, --content-type, --header to define the response.",
	RunE:  runMockAdd,
}

var mockListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all mock rules",
	RunE:  runMockList,
}

var mockRemoveCmd = &cobra.Command{
	Use:   "remove [id]",
	Short: "Remove a mock rule by ID or prefix",
	Args:  cobra.ExactArgs(1),
	RunE:  runMockRemove,
}

var mockFromCmd = &cobra.Command{
	Use:   "from [capture-id]",
	Short: "Generate a mock rule from a captured response",
	Long:  "Load a capture by ID and create a mock rule that replays its response for matching requests.",
	Args:  cobra.ExactArgs(1),
	RunE:  runMockFrom,
}

var mockClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove all mock rules",
	RunE:  runMockClear,
}

func init() {
	mockAddCmd.Flags().StringVar(&mockAddMethod, "method", "", "HTTP method to match (empty = any)")
	mockAddCmd.Flags().StringVar(&mockAddURL, "url", "", "URL substring to match (required)")
	mockAddCmd.Flags().IntVar(&mockAddStatus, "status", 200, "")
	mockAddCmd.Flags().StringVar(&mockAddBody, "body", "", "")
	mockAddCmd.Flags().StringVar(&mockAddContentType, "content-type", "application/json", "")
	mockAddCmd.Flags().StringArrayVar(&mockAddHeader, "header", nil, "Extra response header (Key: Value); repeatable")
	mockAddCmd.Flags().StringVar(&mockAddName, "name", "", "label for this rule")
	_ = mockAddCmd.MarkFlagRequired("url")

	mockCmd.AddCommand(mockAddCmd)
	mockCmd.AddCommand(mockListCmd)
	mockCmd.AddCommand(mockRemoveCmd)
	mockCmd.AddCommand(mockFromCmd)
	mockCmd.AddCommand(mockClearCmd)
}

func mockStore() *mock.Store {
	return mock.NewStore(config.MockFile())
}

func runMockAdd(cmd *cobra.Command, args []string) error {
	headers, err := parseHeaderPairs(mockAddHeader)
	if err != nil {
		return err
	}
	hmap := make(map[string]string, len(headers))
	for _, h := range headers {
		hmap[h.Key] = h.Value
	}
	rule := &mock.Rule{
		ID:          uuid.NewString(),
		Name:        mockAddName,
		Method:      strings.ToUpper(mockAddMethod),
		URLPattern:  mockAddURL,
		Status:      mockAddStatus,
		Body:        mockAddBody,
		ContentType: mockAddContentType,
		Headers:     hmap,
	}
	if err := mockStore().Add(rule); err != nil {
		return err
	}
	fmt.Printf("Added mock rule %s\n", rule.ID[:8])
	return nil
}

func runMockList(cmd *cobra.Command, args []string) error {
	rules := mockStore().Rules()
	if len(rules) == 0 {
		fmt.Println("No mock rules.")
		return nil
	}
	for _, r := range rules {
		method := r.Method
		if method == "" {
			method = "*"
		}
		name := ""
		if r.Name != "" {
			name = " (" + r.Name + ")"
		}
		short := r.ID
		if len(short) > 8 {
			short = short[:8]
		}
		fmt.Printf("%s  %-7s  %3d  %s%s\n", short, method, r.Status, r.URLPattern, name)
	}
	return nil
}

func runMockRemove(cmd *cobra.Command, args []string) error {
	removed, err := mockStore().Remove(args[0])
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("rule not found: %s", args[0])
	}
	fmt.Println("Removed.")
	return nil
}

func runMockFrom(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	c := store.GetByPrefix(args[0])
	if c == nil {
		return fmt.Errorf("capture not found: %s", args[0])
	}
	if c.Response == nil {
		return fmt.Errorf("capture has no response")
	}

	ct := c.Response.Headers.Get("Content-Type")
	rule := &mock.Rule{
		ID:          uuid.NewString(),
		Method:      c.Request.Method,
		URLPattern:  c.Request.URL,
		Status:      c.Response.StatusCode,
		Body:        string(c.Response.Body),
		ContentType: ct,
	}
	if err := mockStore().Add(rule); err != nil {
		return err
	}
	fmt.Printf("Added mock rule %s from capture %s\n", rule.ID[:8], args[0])
	return nil
}

func runMockClear(cmd *cobra.Command, args []string) error {
	if err := mockStore().Clear(); err != nil {
		return err
	}
	fmt.Println("All mock rules cleared.")
	return nil
}
