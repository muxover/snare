package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff [id-a] [id-b]",
	Short: "Compare two captures",
	Long:  "Load two captures by ID or prefix and show request/response differences.",
	Args:  cobra.ExactArgs(2),
	RunE:  runDiff,
}

func runDiff(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	a := store.GetByPrefix(args[0])
	b := store.GetByPrefix(args[1])
	if a == nil {
		return fmt.Errorf("capture not found: %s", args[0])
	}
	if b == nil {
		return fmt.Errorf("capture not found: %s", args[1])
	}

	short := func(id string) string {
		if len(id) > 8 {
			return id[:8]
		}
		return id
	}
	fmt.Printf("[%s] vs [%s]\n", short(a.ID), short(b.ID))
	fmt.Println()

	fmt.Println("Request")
	fmt.Printf("  [%s] %s %s\n", short(a.ID), a.Request.Method, a.Request.URL)
	fmt.Printf("  [%s] %s %s\n", short(b.ID), b.Request.Method, b.Request.URL)
	if a.Request.Method != b.Request.Method || a.Request.URL != b.Request.URL {
		fmt.Println("  (request line differs)")
	}
	fmt.Println()

	fmt.Println("Duration")
	fmt.Printf("  [%s] %s\n", short(a.ID), formatListLatency(a.Duration))
	fmt.Printf("  [%s] %s\n", short(b.ID), formatListLatency(b.Duration))
	fmt.Println()

	fmt.Println("Response")
	printRespShort := func(label string, c *capture.Capture) {
		if c.Error != "" {
			fmt.Printf("  [%s] error: %s\n", label, c.Error)
			return
		}
		if c.Response == nil {
			fmt.Printf("  [%s] (no response)\n", label)
			return
		}
		fmt.Printf("  [%s] %d\n", label, c.Response.StatusCode)
	}
	printRespShort(short(a.ID), a)
	printRespShort(short(b.ID), b)
	fmt.Println()

	diffHeaders("Request headers", a.Request.Headers, b.Request.Headers, short(a.ID), short(b.ID))
	if a.Response != nil && b.Response != nil {
		diffHeaders("Response headers", a.Response.Headers, b.Response.Headers, short(a.ID), short(b.ID))
	}

	diffBodies("Request body", a.Request.Body, b.Request.Body, short(a.ID), short(b.ID))
	if a.Response != nil && b.Response != nil {
		diffBodies("Response body", a.Response.Body, b.Response.Body, short(a.ID), short(b.ID))
	}
	return nil
}

func diffHeaders(title string, ha, hb http.Header, la, lb string) {
	if headersEqual(ha, hb) {
		return
	}
	fmt.Println(title + " (differ)")
	printHeaderDiff(ha, hb, la, lb)
	fmt.Println()
}

func headersEqual(ha, hb http.Header) bool {
	if len(ha) != len(hb) {
		return false
	}
	for k, va := range ha {
		vb := hb[k]
		if len(va) != len(vb) {
			return false
		}
		for i := range va {
			if va[i] != vb[i] {
				return false
			}
		}
	}
	return true
}

func printHeaderDiff(ha, hb http.Header, la, lb string) {
	keys := map[string]struct{}{}
	for k := range ha {
		keys[k] = struct{}{}
	}
	for k := range hb {
		keys[k] = struct{}{}
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		ca := strings.Join(ha.Values(k), ", ")
		cb := strings.Join(hb.Values(k), ", ")
		if ca == cb {
			continue
		}
		fmt.Printf("  %s:\n", k)
		fmt.Printf("    [%s] %s\n", la, ca)
		fmt.Printf("    [%s] %s\n", lb, cb)
	}
}

func diffBodies(title string, ba, bb capture.BodyBytes, la, lb string) {
	if bytes.Equal(ba, bb) {
		return
	}
	fmt.Println(title + " (differ)")
	printBodySide(la, ba)
	printBodySide(lb, bb)
	fmt.Println()
}

func printBodySide(label string, b []byte) {
	fmt.Printf("[%s] body (%d bytes)\n", label, len(b))
	if len(b) == 0 {
		fmt.Println("(empty)")
		return
	}
	s := string(b)
	if strings.IndexByte(s, '\x00') >= 0 {
		fmt.Println("(binary)")
		return
	}
	fmt.Println(s)
}
