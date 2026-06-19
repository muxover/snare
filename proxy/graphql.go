package proxy

import (
	"encoding/json"
	"strings"

	"github.com/muxover/snare/v2/capture"
)

// detectGraphQL inspects a request body and content type and returns a
// GraphQLCapture when the request carries a GraphQL operation. It returns nil
// for non-GraphQL requests.
func detectGraphQL(contentType string, body []byte) *capture.GraphQLCapture {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(ct, "application/graphql") {
		return &capture.GraphQLCapture{OperationType: queryOperationType(string(body))}
	}
	if !strings.HasPrefix(ct, "application/json") {
		return nil
	}
	var payload struct {
		Query         string          `json:"query"`
		OperationName string          `json:"operationName"`
		Variables     json.RawMessage `json:"variables"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	if payload.Query == "" {
		return nil
	}
	return &capture.GraphQLCapture{
		OperationName: payload.OperationName,
		OperationType: queryOperationType(payload.Query),
		Variables:     payload.Variables,
	}
}

func queryOperationType(query string) string {
	for _, line := range strings.Split(query, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "mutation"):
			return "mutation"
		case strings.HasPrefix(line, "subscription"):
			return "subscription"
		case strings.HasPrefix(line, "query"):
			return "query"
		case strings.HasPrefix(line, "{"):
			return "query"
		}
		return "query"
	}
	return ""
}
