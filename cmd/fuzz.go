package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"
	"github.com/spf13/cobra"
)

var (
	fuzzCount         int
	fuzzProxy         string
	fuzzMutateBody    bool
	fuzzMutateHeaders bool
	fuzzMutatePath    bool
	fuzzMutateMethod  bool
)

var fuzzCmd = &cobra.Command{
	Use:   "fuzz <id>",
	Short: "Send mutated variants of a captured request",
	Long:  "Take a captured request and send N mutated variants to surface obvious input-handling gaps. Not a security fuzzer — a developer sanity check.",
	Args:  cobra.ExactArgs(1),
	RunE:  runFuzz,
}

func init() {
	fuzzCmd.Flags().IntVar(&fuzzCount, "count", 20, "Maximum number of variants to send")
	fuzzCmd.Flags().StringVar(&fuzzProxy, "proxy", "", "Route fuzz requests through this proxy URL so they are captured")
	fuzzCmd.Flags().BoolVar(&fuzzMutateBody, "mutate-body", true, "Mutate JSON request body fields")
	fuzzCmd.Flags().BoolVar(&fuzzMutateHeaders, "mutate-headers", true, "Mutate request headers")
	fuzzCmd.Flags().BoolVar(&fuzzMutatePath, "mutate-path", true, "Mutate URL path segments")
	fuzzCmd.Flags().BoolVar(&fuzzMutateMethod, "mutate-method", true, "Cycle through HTTP methods")
}

type fuzzVariant struct {
	label   string
	method  string
	rawURL  string
	headers http.Header
	body    []byte
}

func runFuzz(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	c := store.GetByPrefix(args[0])
	if c == nil {
		return fmt.Errorf("capture not found: %s", args[0])
	}

	variants := buildFuzzVariants(c)
	if len(variants) > fuzzCount {
		variants = variants[:fuzzCount]
	}

	var transport http.RoundTripper
	if fuzzProxy != "" {
		pu, err := url.Parse(fuzzProxy)
		if err != nil {
			return fmt.Errorf("invalid --proxy: %w", err)
		}
		transport = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	origStatus := 0
	if c.Response != nil {
		origStatus = c.Response.StatusCode
	}

	fmt.Printf("Fuzzing %s %s — %d variants\n\n", c.Request.Method, c.Request.URL, len(variants))
	fmt.Printf("%-4s  %-8s  %-7s  %-40s  %s\n", "#", "Method", "Status", "Mutation", "Note")
	fmt.Println(strings.Repeat("─", 80))

	for i, v := range variants {
		status, note := sendFuzzVariant(client, v)
		flag := ""
		if status >= 500 {
			flag = " ⚠ 5xx"
		} else if origStatus > 0 && statusClass(status) != statusClass(origStatus) {
			flag = fmt.Sprintf(" (orig %d)", origStatus)
		}
		fmt.Printf("%-4d  %-8s  %-7d  %-40s  %s%s\n", i+1, v.method, status, truncate(v.label, 40), note, flag)
	}
	return nil
}

func buildFuzzVariants(c *capture.Capture) []fuzzVariant {
	var variants []fuzzVariant

	if fuzzMutateMethod {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE"} {
			if m == c.Request.Method {
				continue
			}
			variants = append(variants, fuzzVariant{
				label:   "method=" + m,
				method:  m,
				rawURL:  c.Request.URL,
				headers: c.Request.Headers.Clone(),
				body:    []byte(c.Request.Body),
			})
		}
	}

	if fuzzMutatePath {
		for _, v := range pathMutants(c.Request.URL) {
			variants = append(variants, fuzzVariant{
				label:   "path: " + v.label,
				method:  c.Request.Method,
				rawURL:  v.url,
				headers: c.Request.Headers.Clone(),
				body:    []byte(c.Request.Body),
			})
		}
	}

	if fuzzMutateHeaders {
		for _, h := range headerMutants(c.Request.Headers) {
			variants = append(variants, fuzzVariant{
				label:   "headers: " + h.label,
				method:  c.Request.Method,
				rawURL:  c.Request.URL,
				headers: h.headers,
				body:    []byte(c.Request.Body),
			})
		}
	}

	if fuzzMutateBody && len(c.Request.Body) > 0 {
		for _, b := range bodyMutants([]byte(c.Request.Body)) {
			h := c.Request.Headers.Clone()
			variants = append(variants, fuzzVariant{
				label:   "body: " + b.label,
				method:  c.Request.Method,
				rawURL:  c.Request.URL,
				headers: h,
				body:    b.body,
			})
		}
	}

	return variants
}

type pathMutant struct {
	label string
	url   string
}

func pathMutants(rawURL string) []pathMutant {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	orig := u.Path
	var out []pathMutant

	apply := func(label, path string) {
		cp := *u
		cp.Path = path
		out = append(out, pathMutant{label: label, url: cp.String()})
	}

	apply("trailing slash", orig+"/")
	apply("traversal /../", orig+"/../")
	apply("traversal /../../", orig+"/../../")

	parts := strings.Split(strings.TrimSuffix(orig, "/"), "/")
	if len(parts) > 1 {
		apply("parent path", strings.Join(parts[:len(parts)-1], "/"))
	}

	apply("/admin", orig+"/admin")
	apply("/undefined", orig+"/undefined")

	return out
}

type headerMutant struct {
	label   string
	headers http.Header
}

func headerMutants(orig http.Header) []headerMutant {
	var out []headerMutant

	del := func(label, key string) {
		if orig.Get(key) == "" {
			return
		}
		h := orig.Clone()
		h.Del(key)
		out = append(out, headerMutant{label: "remove " + key, headers: h})
	}
	del("no Content-Type", "Content-Type")
	del("no Authorization", "Authorization")
	del("no Cookie", "Cookie")

	h := orig.Clone()
	h.Set("X-Fuzz-Overflow", strings.Repeat("A", 8192))
	out = append(out, headerMutant{label: "X-Fuzz-Overflow 8k", headers: h})

	if orig.Get("Content-Type") == "application/json" {
		h = orig.Clone()
		h.Set("Content-Type", "text/plain")
		out = append(out, headerMutant{label: "Content-Type: text/plain", headers: h})
	}

	return out
}

type bodyMutant struct {
	label string
	body  []byte
}

func bodyMutants(body []byte) []bodyMutant {
	var out []bodyMutant

	out = append(out, bodyMutant{label: "empty object", body: []byte("{}")})
	out = append(out, bodyMutant{label: "null", body: []byte("null")})
	out = append(out, bodyMutant{label: "empty string", body: []byte(`""`)})
	out = append(out, bodyMutant{label: "array", body: []byte("[]")})

	var m map[string]interface{}
	if json.Unmarshal(body, &m) != nil {
		return out
	}

	cloneMap := func() map[string]interface{} {
		cp := make(map[string]interface{}, len(m))
		for k, v := range m {
			cp[k] = v
		}
		return cp
	}
	marshal := func(label string, v map[string]interface{}) {
		if b, err := json.Marshal(v); err == nil {
			out = append(out, bodyMutant{label: label, body: b})
		}
	}

	for k, v := range m {
		switch v.(type) {
		case string:
			c := cloneMap(); c[k] = ""; marshal("field "+k+"=empty", c)
			c = cloneMap(); c[k] = strings.Repeat("x", 4096); marshal("field "+k+"=4k string", c)
		case float64:
			for _, n := range []float64{0, -1, 2147483647, -2147483648} {
				c := cloneMap(); c[k] = n; marshal(fmt.Sprintf("field %s=%v", k, n), c)
			}
		}
		c := cloneMap(); c[k] = nil; marshal("field "+k+"=null", c)
		c = cloneMap(); delete(c, k); marshal("remove field "+k, c)
	}

	return out
}

func sendFuzzVariant(client *http.Client, v fuzzVariant) (int, string) {
	var bodyReader io.Reader
	if len(v.body) > 0 {
		bodyReader = bytes.NewReader(v.body)
	}
	req, err := http.NewRequest(v.method, v.rawURL, bodyReader)
	if err != nil {
		return 0, "build error: " + err.Error()
	}
	for k, vals := range v.headers {
		for _, val := range vals {
			req.Header.Add(k, val)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "err: " + truncate(err.Error(), 30)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode, ""
}

func statusClass(code int) int {
	return code / 100
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
