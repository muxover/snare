package capture

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
	"unicode/utf8"
)

// BodyBytes is request/response body that marshals as a JSON string when valid UTF-8,
// so decompressed text is readable in JSON files instead of base64.
type BodyBytes []byte

// MarshalJSON writes the body as a JSON string if valid UTF-8, otherwise as base64.
func (b BodyBytes) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte(`""`), nil
	}
	if utf8.Valid(b) {
		return json.Marshal(string(b))
	}
	return json.Marshal(base64.StdEncoding.EncodeToString(b))
}

// UnmarshalJSON reads body from a JSON string (plain text or base64 from older captures).
func (b *BodyBytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err == nil && len(decoded) < len(s) {
		*b = decoded
		return nil
	}
	*b = []byte(s)
	return nil
}

// Capture holds a single request/response pair.
type Capture struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Protocol  string            `json:"protocol,omitempty"`
	Request   RequestSnapshot   `json:"request"`
	Response  *ResponseSnapshot `json:"response,omitempty"`
	Duration  time.Duration     `json:"duration_ns,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// RequestSnapshot is the captured request.
type RequestSnapshot struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers http.Header `json:"headers"`
	Body    BodyBytes   `json:"body,omitempty"`
}

// ResponseSnapshot is the captured response.
type ResponseSnapshot struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	Body       BodyBytes   `json:"body,omitempty"`
}
