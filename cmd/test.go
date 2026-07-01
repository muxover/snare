package cmd

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	testProxy    string
	testFormat   string
	testParallel bool
)

type testSuite struct {
	Tests []testDef `yaml:"tests"`
}

type testDef struct {
	Name    string            `yaml:"name"`
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
	Expect  testExpect        `yaml:"expect"`
}

type testExpect struct {
	Status          int               `yaml:"status"`
	BodyContains    string            `yaml:"body_contains"`
	BodyNotContains string            `yaml:"body_not_contains"`
	Headers         map[string]string `yaml:"headers"`
}

type testResult struct {
	Name    string
	Passed  bool
	Message string
	Elapsed time.Duration
}

var testCmd = &cobra.Command{
	Use:   "test <suite.yaml>",
	Short: "Run a YAML test suite against a live server",
	Long:  "Load a YAML test suite and run each test. Exits 0 if all pass, 1 if any fail. Use --proxy to route requests through snare so all test traffic is captured.",
	Args:  cobra.ExactArgs(1),
	RunE:  runTest,
}

func init() {
	testCmd.Flags().StringVar(&testProxy, "proxy", "", "Route requests through this proxy URL (optional; use snare's proxy to capture test traffic)")
	testCmd.Flags().StringVar(&testFormat, "format", "text", "Output format: text, junit, tap")
	testCmd.Flags().BoolVar(&testParallel, "parallel", false, "Run tests concurrently")
}

func runTest(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("reading suite: %w", err)
	}
	var suite testSuite
	if err := yaml.Unmarshal(data, &suite); err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}
	if len(suite.Tests) == 0 {
		return fmt.Errorf("no tests found in %s", args[0])
	}

	var transport http.RoundTripper
	if testProxy != "" {
		pu, err := url.Parse(testProxy)
		if err != nil {
			return fmt.Errorf("invalid --proxy: %w", err)
		}
		transport = &http.Transport{Proxy: http.ProxyURL(pu)}
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}

	results := make([]testResult, len(suite.Tests))
	if testParallel {
		var wg sync.WaitGroup
		for i, t := range suite.Tests {
			wg.Add(1)
			go func(i int, t testDef) {
				defer wg.Done()
				results[i] = execTest(client, t)
			}(i, t)
		}
		wg.Wait()
	} else {
		for i, t := range suite.Tests {
			results[i] = execTest(client, t)
		}
	}

	switch testFormat {
	case "junit":
		printTestJUnit(results)
	case "tap":
		printTestTAP(results)
	default:
		printTestText(results)
	}

	for _, r := range results {
		if !r.Passed {
			return fmt.Errorf("test suite failed")
		}
	}
	return nil
}

func execTest(client *http.Client, t testDef) testResult {
	method := t.Method
	if method == "" {
		method = "GET"
	}
	start := time.Now()

	var bodyReader io.Reader
	if t.Body != "" {
		bodyReader = strings.NewReader(t.Body)
	}
	req, err := http.NewRequest(method, t.URL, bodyReader)
	if err != nil {
		return testResult{Name: t.Name, Message: "build request: " + err.Error(), Elapsed: time.Since(start)}
	}
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return testResult{Name: t.Name, Message: "request: " + err.Error(), Elapsed: elapsed}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if t.Expect.Status != 0 && resp.StatusCode != t.Expect.Status {
		return testResult{Name: t.Name, Message: fmt.Sprintf("status: want %d, got %d", t.Expect.Status, resp.StatusCode), Elapsed: elapsed}
	}
	if s := t.Expect.BodyContains; s != "" && !strings.Contains(string(body), s) {
		return testResult{Name: t.Name, Message: fmt.Sprintf("body does not contain %q", s), Elapsed: elapsed}
	}
	if s := t.Expect.BodyNotContains; s != "" && strings.Contains(string(body), s) {
		return testResult{Name: t.Name, Message: fmt.Sprintf("body contains %q (should not)", s), Elapsed: elapsed}
	}
	for k, want := range t.Expect.Headers {
		if got := resp.Header.Get(k); got != want {
			return testResult{Name: t.Name, Message: fmt.Sprintf("header %s: want %q, got %q", k, want, got), Elapsed: elapsed}
		}
	}
	return testResult{Name: t.Name, Passed: true, Elapsed: elapsed}
}

func printTestText(results []testResult) {
	passed, failed := 0, 0
	for _, r := range results {
		if r.Passed {
			fmt.Printf("  ok  %s (%s)\n", r.Name, r.Elapsed.Round(time.Millisecond))
			passed++
		} else {
			fmt.Printf("FAIL  %s — %s\n", r.Name, r.Message)
			failed++
		}
	}
	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
}

func printTestTAP(results []testResult) {
	fmt.Printf("TAP version 13\n1..%d\n", len(results))
	for i, r := range results {
		if r.Passed {
			fmt.Printf("ok %d - %s\n", i+1, r.Name)
		} else {
			fmt.Printf("not ok %d - %s\n  ---\n  message: %s\n  ...\n", i+1, r.Name, r.Message)
		}
	}
}

type jtTestSuites struct {
	XMLName xml.Name    `xml:"testsuites"`
	Suite   jtTestSuite `xml:"testsuite"`
}

type jtTestSuite struct {
	Name      string       `xml:"name,attr"`
	Tests     int          `xml:"tests,attr"`
	Failures  int          `xml:"failures,attr"`
	Time      string       `xml:"time,attr"`
	TestCases []jtTestCase `xml:"testcase"`
}

type jtTestCase struct {
	Name      string      `xml:"name,attr"`
	Classname string      `xml:"classname,attr"`
	Time      string      `xml:"time,attr"`
	Failure   *jtFailure  `xml:"failure,omitempty"`
}

type jtFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func printTestJUnit(results []testResult) {
	failures := 0
	var total time.Duration
	cases := make([]jtTestCase, len(results))
	for i, r := range results {
		total += r.Elapsed
		tc := jtTestCase{
			Name:      r.Name,
			Classname: "snare",
			Time:      fmt.Sprintf("%.3f", r.Elapsed.Seconds()),
		}
		if !r.Passed {
			tc.Failure = &jtFailure{Message: r.Message, Text: r.Message}
			failures++
		}
		cases[i] = tc
	}
	out := jtTestSuites{Suite: jtTestSuite{
		Name:      "snare test",
		Tests:     len(results),
		Failures:  failures,
		Time:      fmt.Sprintf("%.3f", total.Seconds()),
		TestCases: cases,
	}}
	data, _ := xml.MarshalIndent(out, "", "  ")
	fmt.Println(xml.Header + string(data))
}

