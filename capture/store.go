package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const defaultMaxCaptures = 1000

type Store struct {
	mu       sync.RWMutex
	captures []*Capture
	max      int
	dir      string
}

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
		s.pruneDiskToMax()
	}
}

func (s *Store) DeleteByID(id string) error {
	if s.dir == "" {
		return fmt.Errorf("no store directory")
	}
	f := filepath.Join(s.dir, id+".json")
	if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
		return err
	}
	s.mu.Lock()
	for i := range s.captures {
		if s.captures[i].ID == id {
			s.captures = append(s.captures[:i], s.captures[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) pruneDiskToMax() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
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
	if len(files) <= s.max {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().Before(files[j].ModTime())
	})
	for i := 0; i < len(files)-s.max; i++ {
		_ = os.Remove(filepath.Join(s.dir, files[i].Name()))
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

func (s *Store) GetByPrefix(prefix string) *Capture {
	if c := s.Get(prefix); c != nil {
		return c
	}
	for _, c := range s.AllFromDisk() {
		if len(c.ID) >= len(prefix) && c.ID[:len(prefix)] == prefix {
			return c
		}
	}
	return nil
}

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

func (s *Store) All() []*Capture {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Capture, len(s.captures))
	copy(out, s.captures)
	return out
}

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
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().After(files[j].ModTime())
	})
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

func (s *Store) AllFromDisk() []*Capture {
	if s.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var out []*Capture
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()
		if len(id) > 5 {
			id = id[:len(id)-5]
		}
		c := s.loadFromDisk(id)
		if c != nil {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	return out
}
