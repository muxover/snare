package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
	if c.GRPC != nil && len(c.GRPC.Frames) > 0 {
		fmt.Printf("\n=== gRPC — %s ===\n", c.GRPC.ServiceMethod)
		for _, f := range c.GRPC.Frames {
			dir := "→"
			if f.Direction == "response" {
				dir = "←"
			}
			preview := string(f.Data)
			if len(preview) > 256 {
				preview = preview[:256] + "…"
			}
			fmt.Printf("%s  %s\n", dir, preview)
		}
	}
	if c.WebSocket != nil && len(c.WebSocket.Frames) > 0 {
		fmt.Println("\n=== WebSocket frames ===")
		for _, f := range c.WebSocket.Frames {
			fmt.Printf("%s  %-4s  op=%d (%s)  %s\n",
				f.Timestamp.Format("15:04:05.000"),
				f.Direction,
				f.Opcode,
				wsOpcodeLabel(f.Opcode),
				wsPayloadPreview(f.Payload),
			)
		}
	}
	return nil
}

func wsOpcodeLabel(op int) string {
	switch op {
	case 0:
		return "cont"
	case 1:
		return "text"
	case 2:
		return "bin"
	case 8:
		return "close"
	case 9:
		return "ping"
	case 10:
		return "pong"
	default:
		return "?"
	}
}

func wsPayloadPreview(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const max = 2048
	s := string(b)
	if !utf8.Valid(b) {
		return fmt.Sprintf("[%d bytes binary]", len(b))
	}
	if len(s) > max {
		return s[:max] + "…"
	}
	return strings.ReplaceAll(s, "\n", "\\n")
}
