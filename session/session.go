package session

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/muxover/snare/v2/capture"
)

type Entry struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end,omitempty"`
}

func FilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".snare", "sessions.json")
}

func Load() (map[string]Entry, error) {
	data, err := os.ReadFile(FilePath())
	if os.IsNotExist(err) {
		return make(map[string]Entry), nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]Entry
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func Save(m map[string]Entry) error {
	path := FilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func Captures(all []*capture.Capture, e Entry) []*capture.Capture {
	end := e.End
	if end.IsZero() {
		end = time.Now()
	}
	var out []*capture.Capture
	for _, c := range all {
		if !c.Timestamp.Before(e.Start) && !c.Timestamp.After(end) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out
}

func RequestPath(c *capture.Capture) string {
	u, err := url.Parse(c.Request.URL)
	if err != nil {
		return c.Request.URL
	}
	return u.Path
}

func ResponseStatus(c *capture.Capture) int {
	if c.Response == nil {
		return 0
	}
	return c.Response.StatusCode
}

func SortedNames(sessions map[string]Entry) []string {
	names := make([]string, 0, len(sessions))
	for n := range sessions {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
