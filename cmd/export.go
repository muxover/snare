package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"

	"github.com/spf13/cobra"
)

var exportFormat string
var exportLast int

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export captures to HAR, JSON, Postman collection, or bundle",
	Long:  "Export the last N captures to a single file. Format: json (default), har, postman, or bundle.",
	RunE:  runExport,
}

func init() {
	exportCmd.Flags().StringVarP(&exportFormat, "format", "f", "json", "Format: json, har, postman, or bundle")
	exportCmd.Flags().IntVarP(&exportLast, "last", "n", 50, "")
}

func runExport(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	captures := store.ListFromDisk(exportLast)
	if len(captures) == 0 {
		fmt.Println("No captures to export.")
		return nil
	}
	switch exportFormat {
	case "bundle":
		bundlePackOut = "export.snare"
		bundlePackSession = ""
		bundlePackIDs = ""
		return runBundlePack(nil, nil)
	case "har":
		out := "export.har"
		har := buildHAR(captures)
		data, err := json.MarshalIndent(har, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0644)
	case "postman":
		out := "export.postman_collection.json"
		col := buildPostman(captures)
		data, err := json.MarshalIndent(col, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0644)
	default:
		out := "export.json"
		data, err := json.MarshalIndent(captures, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0644)
	}
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
		if c.WebSocket != nil && len(c.WebSocket.Frames) > 0 {
			ent["_webSocketMessages"] = websocketMessagesHAR(c.WebSocket.Frames)
		}
		entries = append(entries, ent)
	}
	return map[string]interface{}{
		"log": map[string]interface{}{
			"version": "1.2",
			"creator": map[string]interface{}{"name": "snare", "version": Version},
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

func websocketMessagesHAR(frames []capture.WSFrame) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(frames))
	for _, f := range frames {
		typ := "send"
		if f.Direction == "s2c" {
			typ = "receive"
		}
		out = append(out, map[string]interface{}{
			"type":   typ,
			"time":   f.Timestamp.UnixMilli(),
			"opcode": f.Opcode,
			"data":   harWSDataString(f.Payload),
		})
	}
	return out
}

func harWSDataString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func buildPostman(captures []*capture.Capture) map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(captures))
	for _, c := range captures {
		u, err := url.Parse(c.Request.URL)
		if err != nil {
			continue
		}
		pathParts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")

		rawURL := map[string]interface{}{
			"raw":      c.Request.URL,
			"protocol": u.Scheme,
			"host":     strings.Split(u.Host, "."),
			"path":     pathParts,
		}
		if u.RawQuery != "" {
			var qparams []map[string]string
			for k, vals := range u.Query() {
				for _, v := range vals {
					qparams = append(qparams, map[string]string{"key": k, "value": v})
				}
			}
			rawURL["query"] = qparams
		}

		var headers []map[string]string
		for k, vals := range c.Request.Headers {
			for _, v := range vals {
				headers = append(headers, map[string]string{"key": k, "value": v})
			}
		}

		request := map[string]interface{}{
			"method": c.Request.Method,
			"url":    rawURL,
			"header": headers,
		}
		if len(c.Request.Body) > 0 {
			ct := c.Request.Headers.Get("Content-Type")
			mode := "raw"
			if strings.Contains(ct, "form") {
				mode = "urlencoded"
			}
			request["body"] = map[string]interface{}{
				"mode": mode,
				"raw":  string(c.Request.Body),
			}
		}

		name := c.Request.Method + " " + u.Path
		items = append(items, map[string]interface{}{
			"name":    name,
			"request": request,
		})
	}

	return map[string]interface{}{
		"info": map[string]interface{}{
			"name":   "snare export",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		"item": items,
	}
}
