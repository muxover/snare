package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"
	sess "github.com/muxover/snare/v2/session"

	"github.com/spf13/cobra"
)

var (
	diffGolden       string
	diffCheck        string
	diffSession      string
	diffStrict       bool
	diffIgnoreFields string
)

var diffCmd = &cobra.Command{
	Use:   "diff [id-a] [id-b]",
	Short: "Compare two captures, or record/check a golden baseline",
	Long: `Compare two captures by ID, or manage golden baselines for regression testing:

  snare diff <id-a> <id-b>                  Compare two captures.
  snare diff --golden <name>                Record current session as a golden baseline.
  snare diff --check <name>                 Compare current session against a golden; exits 1 on regression.
  snare diff --check <name> --strict        Also compare response bodies.
  snare diff --check <name> --ignore-fields id,timestamp  Skip these JSON keys in body comparison.`,
	Args: cobra.ArbitraryArgs,
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().StringVar(&diffGolden, "golden", "", "Record current session captures as a named golden baseline")
	diffCmd.Flags().StringVar(&diffCheck, "check", "", "Compare current session against a named golden baseline")
	diffCmd.Flags().StringVar(&diffSession, "session", "", "Session to use for --golden/--check (default: most recent)")
	diffCmd.Flags().BoolVar(&diffStrict, "strict", false, "Also compare response bodies when using --check")
	diffCmd.Flags().StringVar(&diffIgnoreFields, "ignore-fields", "", "Comma-separated response body JSON keys to ignore in --strict comparison")
}

func runDiff(cmd *cobra.Command, args []string) error {
	if diffGolden != "" {
		return recordGolden(diffGolden)
	}
	if diffCheck != "" {
		return checkGolden(diffCheck)
	}

	if len(args) != 2 {
		return fmt.Errorf("provide two capture IDs, or use --golden / --check")
	}
	return runCaptureDiff(args[0], args[1])
}

func runCaptureDiff(idA, idB string) error {
	store := capture.NewStore(0, config.StoreDir())
	a := store.GetByPrefix(idA)
	b := store.GetByPrefix(idB)
	if a == nil {
		return fmt.Errorf("capture not found: %s", idA)
	}
	if b == nil {
		return fmt.Errorf("capture not found: %s", idB)
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

type goldenEntry struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Body   string `json:"body,omitempty"`
}

func goldenPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".snare", "golden", name+".json")
}

func sessionCaptures() ([]*capture.Capture, error) {
	store := capture.NewStore(0, config.StoreDir())
	all := store.AllFromDisk()

	if diffSession != "" {
		sessions, err := sess.Load()
		if err != nil {
			return nil, err
		}
		e, ok := sessions[diffSession]
		if !ok {
			return nil, fmt.Errorf("unknown session %q", diffSession)
		}
		return sess.Captures(all, e), nil
	}

	// most recent completed session
	sessions, err := sess.Load()
	if err != nil {
		return nil, err
	}
	var bestName string
	for n, e := range sessions {
		if e.End.IsZero() {
			continue
		}
		if bestName == "" || e.End.After(sessions[bestName].End) {
			bestName = n
		}
	}
	if bestName == "" {
		return nil, fmt.Errorf("no completed session found; use --session <name> or run 'snare session start/end'")
	}
	return sess.Captures(all, sessions[bestName]), nil
}

func recordGolden(name string) error {
	captures, err := sessionCaptures()
	if err != nil {
		return err
	}

	entries := make([]goldenEntry, 0, len(captures))
	for _, c := range captures {
		e := goldenEntry{
			Method: c.Request.Method,
			Path:   sess.RequestPath(c),
			Status: sess.ResponseStatus(c),
		}
		if diffStrict && c.Response != nil {
			e.Body = string(c.Response.Body)
		}
		entries = append(entries, e)
	}

	path := goldenPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	fmt.Printf("Golden %q recorded: %d request(s) → %s\n", name, len(entries), path)
	return nil
}

func checkGolden(name string) error {
	data, err := os.ReadFile(goldenPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no golden %q — run 'snare diff --golden %s' first", name, name)
		}
		return err
	}
	var golden []goldenEntry
	if err := json.Unmarshal(data, &golden); err != nil {
		return fmt.Errorf("reading golden: %w", err)
	}

	captures, err := sessionCaptures()
	if err != nil {
		return err
	}

	ignoreSet := map[string]bool{}
	if diffIgnoreFields != "" {
		for _, f := range strings.Split(diffIgnoreFields, ",") {
			ignoreSet[strings.TrimSpace(f)] = true
		}
	}

	diffs := 0
	n := len(golden)
	if len(captures) > n {
		n = len(captures)
	}
	for i := 0; i < n; i++ {
		var g *goldenEntry
		var c *capture.Capture
		if i < len(golden) {
			g = &golden[i]
		}
		if i < len(captures) {
			c = captures[i]
		}

		if g == nil {
			fmt.Printf("[%d] new request not in golden: %s %s\n", i+1, c.Request.Method, sess.RequestPath(c))
			diffs++
			continue
		}
		if c == nil {
			fmt.Printf("[%d] golden request missing: %s %s\n", i+1, g.Method, g.Path)
			diffs++
			continue
		}

		lineG := fmt.Sprintf("%s %s %d", g.Method, g.Path, g.Status)
		lineC := fmt.Sprintf("%s %s %d", c.Request.Method, sess.RequestPath(c), sess.ResponseStatus(c))
		if lineG != lineC {
			fmt.Printf("[%d] %s → %s\n", i+1, lineG, lineC)
			diffs++
			continue
		}

		if diffStrict && g.Body != "" && c.Response != nil {
			gotBody := string(c.Response.Body)
			if !jsonBodyEqual(g.Body, gotBody, ignoreSet) {
				fmt.Printf("[%d] %s %s — response body differs\n", i+1, g.Method, g.Path)
				diffs++
			}
		}
	}

	if diffs == 0 {
		fmt.Printf("Golden %q matches (%d request(s)).\n", name, len(golden))
		return nil
	}
	fmt.Printf("\n%d regression(s) found vs golden %q.\n", diffs, name)
	return fmt.Errorf("golden check failed")
}

func jsonBodyEqual(a, b string, ignore map[string]bool) bool {
	if len(ignore) == 0 {
		return a == b
	}
	var ma, mb map[string]interface{}
	if json.Unmarshal([]byte(a), &ma) != nil || json.Unmarshal([]byte(b), &mb) != nil {
		return a == b
	}
	for k := range ignore {
		delete(ma, k)
		delete(mb, k)
	}
	da, _ := json.Marshal(ma)
	db, _ := json.Marshal(mb)
	return string(da) == string(db)
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
