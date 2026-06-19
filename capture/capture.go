package capture

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
	"unicode/utf8"
)

type BodyBytes []byte

func (b BodyBytes) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte(`""`), nil
	}
	if utf8.Valid(b) {
		return json.Marshal(string(b))
	}
	return json.Marshal(base64.StdEncoding.EncodeToString(b))
}

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

type Capture struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Protocol  string            `json:"protocol,omitempty"`
	Request   RequestSnapshot   `json:"request"`
	Response  *ResponseSnapshot `json:"response,omitempty"`
	Duration  time.Duration     `json:"duration_ns,omitempty"`
	Error     string            `json:"error,omitempty"`
	WebSocket *WebSocketCapture `json:"websocket,omitempty"`
	GRPC      *GRPCCapture      `json:"grpc,omitempty"`
	SSE       *SSECapture       `json:"sse,omitempty"`
	GraphQL   *GraphQLCapture   `json:"graphql,omitempty"`
}

type GRPCCapture struct {
	ServiceMethod   string          `json:"method,omitempty"`
	Frames          []GRPCFrame     `json:"frames,omitempty"`
	DecodedRequest  json.RawMessage `json:"decoded_request,omitempty"`
	DecodedResponse json.RawMessage `json:"decoded_response,omitempty"`
}

type SSECapture struct {
	Frames []SSEFrame `json:"frames,omitempty"`
}

type SSEFrame struct {
	Timestamp time.Time `json:"timestamp"`
	ID        string    `json:"id,omitempty"`
	Event     string    `json:"event,omitempty"`
	Data      string    `json:"data"`
}

type GraphQLCapture struct {
	OperationName string          `json:"operation_name,omitempty"`
	OperationType string          `json:"operation_type,omitempty"`
	Variables     json.RawMessage `json:"variables,omitempty"`
}

type GRPCFrame struct {
	Direction  string    `json:"direction"`
	Compressed bool      `json:"compressed,omitempty"`
	Data       BodyBytes `json:"data"`
}

type WebSocketCapture struct {
	Frames []WSFrame `json:"frames,omitempty"`
}

type WSFrame struct {
	Timestamp time.Time `json:"timestamp"`
	Direction string    `json:"direction"`
	Opcode    int       `json:"opcode"`
	Payload   BodyBytes `json:"payload,omitempty"`
}

type RequestSnapshot struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers http.Header `json:"headers"`
	Body    BodyBytes   `json:"body,omitempty"`
}

type ResponseSnapshot struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	Body       BodyBytes   `json:"body,omitempty"`
}
