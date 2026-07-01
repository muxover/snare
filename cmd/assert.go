package cmd

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"
	"github.com/spf13/cobra"
)

var (
	assertMethod string
	assertStatus int
	assertURL    string
	assertBody   string
	assertMin    int
	assertMax    int
	assertFormat string
)

var assertCmd = &cobra.Command{
	Use:   "assert",
	Short: "Assert capture conditions; exits 1 if they are not met",
	Long:  "Filter captures using the same flags as 'list', then assert the matched count is within --min/--max bounds. Exits 0 on success, 1 on failure. Designed for use in CI pipelines.",
	RunE:  runAssert,
}

func init() {
	assertCmd.Flags().StringVar(&assertMethod, "method", "", "Filter by HTTP method")
	assertCmd.Flags().IntVar(&assertStatus, "status", 0, "Filter by response status code")
	assertCmd.Flags().StringVar(&assertURL, "url", "", "Filter by URL substring")
	assertCmd.Flags().StringVar(&assertBody, "body", "", "Filter by substring in request or response body")
	assertCmd.Flags().IntVar(&assertMin, "min", 1, "Minimum number of matching captures (inclusive)")
	assertCmd.Flags().IntVar(&assertMax, "max", -1, "Maximum number of matching captures (-1 = no limit)")
	assertCmd.Flags().StringVar(&assertFormat, "format", "text", "Output format: text or junit")
}

func runAssert(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	captures := store.AllFromDisk()
	matched := filterCaptures(captures, assertMethod, assertStatus, assertURL, "", assertBody, "", time.Time{}, time.Time{})
	n := len(matched)

	label := buildAssertLabel()
	passed := true
	var failMsg string

	if assertMin > 0 && n < assertMin {
		passed = false
		failMsg = fmt.Sprintf("assert failed: %d capture(s) match %q, want at least %d", n, label, assertMin)
	} else if assertMax >= 0 && n > assertMax {
		passed = false
		failMsg = fmt.Sprintf("assert failed: %d capture(s) match %q, want at most %d", n, label, assertMax)
	}

	if assertFormat == "junit" {
		printAssertJUnit(label, passed, failMsg)
	} else {
		if passed {
			fmt.Printf("ok: %d capture(s) match %q\n", n, label)
		}
	}

	if !passed {
		return fmt.Errorf("%s", failMsg)
	}
	return nil
}

func buildAssertLabel() string {
	var parts []string
	if assertMethod != "" {
		parts = append(parts, "method="+assertMethod)
	}
	if assertStatus != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", assertStatus))
	}
	if assertURL != "" {
		parts = append(parts, "url="+assertURL)
	}
	if assertBody != "" {
		parts = append(parts, "body="+assertBody)
	}
	if len(parts) == 0 {
		return "*"
	}
	return strings.Join(parts, " ")
}

type assertJUnitSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suite   assertJUnitSuite `xml:"testsuite"`
}

type assertJUnitSuite struct {
	Name      string           `xml:"name,attr"`
	Tests     int              `xml:"tests,attr"`
	Failures  int              `xml:"failures,attr"`
	Time      string           `xml:"time,attr"`
	TestCases []assertJUnitCase `xml:"testcase"`
}

type assertJUnitCase struct {
	Name      string         `xml:"name,attr"`
	Classname string         `xml:"classname,attr"`
	Time      string         `xml:"time,attr"`
	Failure   *assertFailure `xml:"failure,omitempty"`
}

type assertFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func printAssertJUnit(label string, passed bool, failMsg string) {
	failures := 0
	tc := assertJUnitCase{
		Name:      "assert " + label,
		Classname: "snare",
		Time:      "0.000",
	}
	if !passed {
		tc.Failure = &assertFailure{Message: failMsg, Text: failMsg}
		failures = 1
	}
	out := assertJUnitSuites{Suite: assertJUnitSuite{
		Name:      "snare assert",
		Tests:     1,
		Failures:  failures,
		Time:      "0.000",
		TestCases: []assertJUnitCase{tc},
	}}
	data, _ := xml.MarshalIndent(out, "", "  ")
	fmt.Println(xml.Header + string(data))
}
