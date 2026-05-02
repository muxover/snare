package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import [file.har]",
	Short: "Import HAR captures into the store",
	Long:  "Parse a HAR file and write each entry into the capture store. Imported captures are available to list, show, diff, replay, and export immediately.",
	Args:  cobra.ExactArgs(1),
	RunE:  runImport,
}

type harDoc struct {
	Log struct {
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harEntry struct {
	StartedDateTime string `json:"startedDateTime"`
	Time            int64  `json:"time"`
	Request         struct {
		Method   string      `json:"method"`
		URL      string      `json:"url"`
		Headers  []harHeader `json:"headers"`
		PostData struct {
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
		} `json:"postData"`
	} `json:"request"`
	Response struct {
		Status  int         `json:"status"`
		Headers []harHeader `json:"headers"`
		Content struct {
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
		} `json:"content"`
	} `json:"response"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func runImport(cmd *cobra.Command, args []string) error {
	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var doc harDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("invalid HAR file: %w", err)
	}
	if len(doc.Log.Entries) == 0 {
		return fmt.Errorf("no HAR entries found")
	}

	store := capture.NewStore(0, config.StoreDir())
	imported := 0
	for _, e := range doc.Log.Entries {
		if e.Request.URL == "" || e.Request.Method == "" {
			continue
		}
		ts := time.Now()
		if e.StartedDateTime != "" {
			if t, err := time.Parse(time.RFC3339Nano, e.StartedDateTime); err == nil {
				ts = t
			} else if t, err := time.Parse(time.RFC3339, e.StartedDateTime); err == nil {
				ts = t
			}
		}
		reqBody := decodeHARText(e.Request.PostData.Text, e.Request.PostData.Encoding)
		respBody := decodeHARText(e.Response.Content.Text, e.Response.Content.Encoding)
		c := &capture.Capture{
			ID:        uuid.NewString(),
			Timestamp: ts,
			Protocol:  "har",
			Request: capture.RequestSnapshot{
				Method:  e.Request.Method,
				URL:     e.Request.URL,
				Headers: harHeadersToHTTP(e.Request.Headers),
				Body:    capture.BodyBytes(reqBody),
			},
			Response: &capture.ResponseSnapshot{
				StatusCode: e.Response.Status,
				Headers:    harHeadersToHTTP(e.Response.Headers),
				Body:       capture.BodyBytes(respBody),
			},
			Duration: time.Duration(e.Time) * time.Millisecond,
		}
		store.Add(c)
		imported++
	}
	if imported == 0 {
		return fmt.Errorf("no valid entries imported")
	}
	fmt.Printf("Imported %d capture(s)\n", imported)
	return nil
}

func harHeadersToHTTP(in []harHeader) http.Header {
	out := make(http.Header)
	for _, h := range in {
		key := strings.TrimSpace(h.Name)
		if key == "" {
			continue
		}
		out[key] = append(out[key], h.Value)
	}
	return out
}

func decodeHARText(text, encoding string) []byte {
	if text == "" {
		return nil
	}
	if strings.EqualFold(encoding, "base64") {
		if dec, err := base64.StdEncoding.DecodeString(text); err == nil {
			return dec
		}
	}
	return []byte(text)
}
