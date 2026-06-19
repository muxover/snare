package proxy

import (
	"bufio"
	"net/http"
	"strings"
	"time"

	"github.com/muxover/snare/v2/capture"
)

func isSSE(h http.Header) bool {
	ct := h.Get("Content-Type")
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "text/event-stream")
}

// streamSSE relays an event-stream body to the client line by line while
// accumulating parsed events into the capture. It returns when the upstream
// body is exhausted or the client disconnects.
func (h *Handler) streamSSE(rw http.ResponseWriter, resp *http.Response, c *capture.Capture) {
	for k, v := range resp.Header {
		for _, vv := range v {
			rw.Header().Add(k, vv)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	flusher, _ := rw.(http.Flusher)

	frames := parseSSEStream(resp.Body, func(line string) bool {
		if _, err := rw.Write([]byte(line + "\n")); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	})

	if len(frames) > 0 {
		c.SSE = &capture.SSECapture{Frames: frames}
	}
	if !h.isIgnored(c.Request.URL) {
		h.addCapture(c)
		h.Log.Info("captured (sse)", "url", c.Request.URL, "events", len(frames), "id", c.ID[:8])
	}
}

// parseSSEStream reads SSE lines, forwarding each via write, and groups
// id/event/data fields into frames at every blank line. If write returns
// false the relay stops.
func parseSSEStream(body interface{ Read([]byte) (int, error) }, write func(string) bool) []capture.SSEFrame {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)

	var frames []capture.SSEFrame
	var id, event string
	var data []string

	flush := func() {
		if len(data) == 0 && event == "" && id == "" {
			return
		}
		frames = append(frames, capture.SSEFrame{
			Timestamp: time.Now(),
			ID:        id,
			Event:     event,
			Data:      strings.Join(data, "\n"),
		})
		id, event, data = "", "", nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !write(line) {
			break
		}
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "id":
			id = value
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	flush()
	return frames
}
