package intercept

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DecisionForward = "forward"
	DecisionDrop    = "drop"
)

type PendingRequest struct {
	ID         string      `json:"id"`
	Timestamp  time.Time   `json:"timestamp"`
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	Headers    http.Header `json:"headers"`
	Body       string      `json:"body,omitempty"`
	Decision   string      `json:"decision,omitempty"`
	ModMethod  string      `json:"mod_method,omitempty"`
	ModHeaders http.Header `json:"mod_headers,omitempty"`
	ModBody    string      `json:"mod_body,omitempty"`
}

type Queue struct {
	dir string
}

func NewQueue(dir string) *Queue {
	return &Queue{dir: dir}
}

func (q *Queue) Enqueue(pr *PendingRequest) error {
	if err := os.MkdirAll(q.dir, 0700); err != nil {
		return err
	}
	return q.write(pr)
}

func (q *Queue) Pending() ([]*PendingRequest, error) {
	entries, err := os.ReadDir(q.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*PendingRequest
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		pr, err := q.readFile(filepath.Join(q.dir, e.Name()))
		if err != nil || pr.Decision != "" {
			continue
		}
		out = append(out, pr)
	}
	return out, nil
}

func (q *Queue) GetByPrefix(prefix string) (*PendingRequest, error) {
	entries, err := os.ReadDir(q.dir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no pending requests")
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		if name == prefix || strings.HasPrefix(name, prefix) {
			return q.readFile(filepath.Join(q.dir, e.Name()))
		}
	}
	return nil, fmt.Errorf("pending request not found: %s", prefix)
}

func (q *Queue) Decide(id, decision string, mod *PendingRequest) error {
	pr, err := q.GetByPrefix(id)
	if err != nil {
		return err
	}
	pr.Decision = decision
	if mod != nil {
		pr.ModMethod = mod.ModMethod
		pr.ModHeaders = mod.ModHeaders
		pr.ModBody = mod.ModBody
	}
	return q.write(pr)
}

func (q *Queue) WaitDecision(id string, timeout time.Duration) (*PendingRequest, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pr, err := q.readFile(filepath.Join(q.dir, id+".json"))
		if err != nil {
			return nil, err
		}
		if pr.Decision != "" {
			return pr, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("intercept timeout for %s", id)
}

func (q *Queue) Remove(id string) error {
	return os.Remove(filepath.Join(q.dir, id+".json"))
}

func (q *Queue) write(pr *PendingRequest) error {
	data, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(q.dir, pr.ID+".json"), data, 0600)
}

func (q *Queue) readFile(path string) (*PendingRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pr PendingRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func MatchesPattern(req *http.Request, pattern string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	return strings.Contains(req.URL.String(), pattern)
}
