package mock

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Rule struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	Method      string            `json:"method,omitempty"`
	URLPattern  string            `json:"url_pattern"`
	Status      int               `json:"status"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
}

func (r *Rule) Matches(req *http.Request) bool {
	if r.Method != "" && !strings.EqualFold(req.Method, r.Method) {
		return false
	}
	return strings.Contains(req.URL.String(), r.URLPattern)
}

type Store struct {
	mu   sync.RWMutex
	path string
	rules []*Rule
}

func NewStore(path string) *Store {
	s := &Store{path: path}
	_ = s.load()
	return s
}

func (s *Store) Rules() []*Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Rule, len(s.rules))
	copy(out, s.rules)
	return out
}

func (s *Store) Add(r *Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = append(s.rules, r)
	return s.save()
}

func (s *Store) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rules {
		if r.ID == id || strings.HasPrefix(r.ID, id) {
			s.rules = append(s.rules[:i], s.rules[i+1:]...)
			return true, s.save()
		}
	}
	return false, nil
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = nil
	return s.save()
}

func (s *Store) Match(req *http.Request) *Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rules {
		if r.Matches(req) {
			return r
		}
	}
	return nil
}

func (s *Store) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.rules)
}

func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.rules, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}
