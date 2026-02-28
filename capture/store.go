package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const defaultMaxCaptures = 1000

// Store holds captures in memory and optionally on disk.
type Store struct {
	mu       sync.RWMutex
	captures []*Capture
	max      int
	dir      string
}

// NewStore creates a store with optional persistence directory.
func NewStore(maxCaptures int, persistDir string) *Store {
	if maxCaptures <= 0 {
		maxCaptures = defaultMaxCaptures
	}
	return &Store{
		captures: make([]*Capture, 0, maxCaptures),
		max:      maxCaptures,
		dir:      persistDir,
	}
}

// Add appends a capture and trims to max size.
func (s *Store) Add(c *Capture) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captures = append(s.captures, c)
	if len(s.captures) > s.max {
		s.captures = s.captures[len(s.captures)-s.max:]
	}
	if s.dir != "" {
		if err := s.persistOne(c); err != nil {
			fmt.Fprintf(os.Stderr, "[snare] failed to save capture: %v\n", err)
		}
	}
}

func (s *Store) persistOne(c *Capture) error {
	f := filepath.Join(s.dir, c.ID+".json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f, data, 0600)
}

// List returns the last n captures (newest first).
func (s *Store) List(n int) []*Capture {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || n > len(s.captures) {
		n = len(s.captures)
	}
	out := make([]*Capture, n)
	for i := 0; i < n; i++ {
		out[i] = s.captures[len(s.captures)-1-i]
	}
	return out
}

// Get returns a capture by ID.
func (s *Store) Get(id string) *Capture {
	s.mu.RLock()
	for i := range s.captures {
		if s.captures[i].ID == id {
			c := s.captures[i]
			s.mu.RUnlock()
			return c
		}
	}
	s.mu.RUnlock()
	if s.dir != "" {
		return s.loadFromDisk(id)
	}
	return nil
}

func (s *Store) loadFromDisk(id string) *Capture {
	data, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return nil
	}
	var c Capture
	if json.Unmarshal(data, &c) != nil {
		return nil
	}
	return &c
}

// GetByPrefix returns a capture by ID or by prefix.
func (s *Store) GetByPrefix(prefix string) *Capture {
	if c := s.Get(prefix); c != nil {
		return c
	}
	for _, c := range s.ListFromDisk(500) {
		if len(c.ID) >= len(prefix) && c.ID[:len(prefix)] == prefix {
			return c
		}
	}
	return nil
}

// Clear removes all in-memory and optionally on-disk captures.
func (s *Store) Clear(deleteFiles bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captures = s.captures[:0]
	if deleteFiles && s.dir != "" {
		_ = filepath.Walk(s.dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if filepath.Ext(path) == ".json" {
				_ = os.Remove(path)
			}
			return nil
		})
	}
}

// All returns a copy of all captures.
func (s *Store) All() []*Capture {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Capture, len(s.captures))
	copy(out, s.captures)
	return out
}

// ListFromDisk returns up to n most recent captures from the persistence directory.
func (s *Store) ListFromDisk(n int) []*Capture {
	if s.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var files []os.FileInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, info)
	}
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].ModTime().After(files[i].ModTime()) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
	if n > 0 && len(files) > n {
		files = files[:n]
	}
	var out []*Capture
	for _, f := range files {
		id := f.Name()
		if len(id) > 5 {
			id = id[:len(id)-5]
		}
		c := s.loadFromDisk(id)
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}
