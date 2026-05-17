package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"

	"github.com/spf13/cobra"
)

var (
	openapiOut    string
	openapiTitle  string
	openapiServer string
)

var openapiCmd = &cobra.Command{
	Use:   "openapi",
	Short: "Generate an OpenAPI spec from captured traffic",
	Long:  "Analyse captured requests and responses and emit an OpenAPI 3.0 JSON spec. Paths with numeric or UUID segments are parameterised automatically.",
	RunE:  runOpenAPI,
}

func init() {
	openapiCmd.Flags().StringVarP(&openapiOut, "out", "o", "openapi.json", "Output file")
	openapiCmd.Flags().StringVar(&openapiTitle, "title", "snare captured API", "API title")
	openapiCmd.Flags().StringVar(&openapiServer, "server", "", "Override server URL (default: inferred from captures)")
}

var (
	reUUID    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reNumeric = regexp.MustCompile(`^\d+$`)
)

func normalizePath(path string) (normalized string, params []string) {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if reUUID.MatchString(p) || reNumeric.MatchString(p) {
			params = append(params, "id")
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/"), params
}

type oaOperation struct {
	contentType string
	reqExample  string
	respStatus  int
	respCT      string
	respExample string
}

func runOpenAPI(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	all := store.AllFromDisk()
	if len(all) == 0 {
		return fmt.Errorf("no captures found")
	}

	type pathMethodKey struct {
		path   string
		method string
	}
	ops := make(map[pathMethodKey]*oaOperation)
	serverURL := openapiServer
	servers := make(map[string]struct{})

	for _, c := range all {
		if c.Request.URL == "" {
			continue
		}
		u, err := url.Parse(c.Request.URL)
		if err != nil {
			continue
		}
		base := u.Scheme + "://" + u.Host
		servers[base] = struct{}{}

		normPath, _ := normalizePath(u.Path)
		key := pathMethodKey{path: normPath, method: strings.ToLower(c.Request.Method)}
		if _, exists := ops[key]; exists {
			continue
		}
		op := &oaOperation{}
		if len(c.Request.Body) > 0 {
			op.contentType = c.Request.Headers.Get("Content-Type")
			op.reqExample = string(c.Request.Body)
		}
		if c.Response != nil {
			op.respStatus = c.Response.StatusCode
			op.respCT = c.Response.Headers.Get("Content-Type")
			op.respExample = string(c.Response.Body)
		}
		ops[key] = op
	}

	if serverURL == "" {
		for s := range servers {
			serverURL = s
			break
		}
	}

	paths := make(map[string]map[string]interface{})
	for key, op := range ops {
		if paths[key.path] == nil {
			paths[key.path] = make(map[string]interface{})
		}
		operation := buildOAOperation(key.method, op)
		paths[key.path][key.method] = operation
	}

	sortedPaths := make([]string, 0, len(paths))
	for p := range paths {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	orderedPaths := make(map[string]interface{}, len(paths))
	for _, p := range sortedPaths {
		orderedPaths[p] = paths[p]
	}

	spec := map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":   openapiTitle,
			"version": "0.1.0",
			"x-generated-by": map[string]interface{}{
				"tool": "snare",
				"date": time.Now().Format("2006-01-02"),
			},
		},
		"servers": []map[string]interface{}{
			{"url": serverURL},
		},
		"paths": orderedPaths,
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(openapiOut, data, 0644); err != nil {
		return err
	}
	fmt.Printf("Wrote %s (%d paths from %d captures)\n", openapiOut, len(paths), len(all))
	return nil
}

func buildOAOperation(method string, op *oaOperation) map[string]interface{} {
	operation := map[string]interface{}{
		"responses": map[string]interface{}{},
	}

	if op.reqExample != "" {
		ct := op.contentType
		if ct == "" {
			ct = "application/json"
		}
		mediaType := map[string]interface{}{}
		if isJSON(op.reqExample) {
			var raw interface{}
			if json.Unmarshal([]byte(op.reqExample), &raw) == nil {
				mediaType["example"] = raw
			}
		} else {
			mediaType["example"] = op.reqExample
		}
		operation["requestBody"] = map[string]interface{}{
			"content": map[string]interface{}{
				ct: mediaType,
			},
		}
	}

	responses := map[string]interface{}{}
	if op.respStatus > 0 {
		status := fmt.Sprintf("%d", op.respStatus)
		respObj := map[string]interface{}{
			"description": http.StatusText(op.respStatus),
		}
		if op.respExample != "" {
			ct := op.respCT
			if ct == "" {
				ct = "application/json"
			}
			mediaType := map[string]interface{}{}
			if isJSON(op.respExample) {
				var raw interface{}
				if json.Unmarshal([]byte(op.respExample), &raw) == nil {
					mediaType["example"] = raw
				}
			} else {
				mediaType["example"] = op.respExample
			}
			respObj["content"] = map[string]interface{}{ct: mediaType}
		}
		responses[status] = respObj
	} else {
		responses["200"] = map[string]interface{}{"description": "OK"}
	}
	operation["responses"] = responses
	return operation
}

func isJSON(s string) bool {
	s = strings.TrimSpace(s)
	return (strings.HasPrefix(s, "{") || strings.HasPrefix(s, "["))
}
