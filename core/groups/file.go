package groups

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// fileFormat is the on-disk envelope of a groups file. Version lets future
// formats migrate without guessing.
type fileFormat struct {
	Version int     `json:"version"`
	Groups  []Group `json:"groups"`
}

const fileFormatVersion = 1

// FileStore persists groups in a JSON file. Every write rewrites the whole
// file through a temp file + rename, so readers never observe a torn
// write. Group counts are small (tens), which makes read-modify-write the
// simplest correct choice — the same trade-off the rules FileStore makes.
type FileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore creates a FileStore backed by path. A missing file starts
// empty; a malformed one aborts construction so configuration errors are
// caught at startup, not on first write.
func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path}
	if _, err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads the whole file; a missing file counts as empty.
func (s *FileStore) load() ([]Group, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("groups: read %s: %w", s.path, err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("groups: parse %s: %w", s.path, err)
	}
	return f.Groups, nil
}

// save atomically replaces the file with the given group list.
func (s *FileStore) save(gs []Group) error {
	if gs == nil {
		gs = []Group{}
	}
	// Group has only marshalable fields, so encoding cannot fail.
	data, _ := json.MarshalIndent(fileFormat{Version: fileFormatVersion, Groups: gs}, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("groups: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil { // a directory target (or cross-device rename) surfaces here
		return fmt.Errorf("groups: replace %s: %w", s.path, err)
	}
	return nil
}

// List returns all groups in stored order.
func (s *FileStore) List(_ context.Context) ([]Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Get returns one group by id.
func (s *FileStore) Get(_ context.Context, id string) (Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gs, err := s.load()
	if err != nil {
		return Group{}, err
	}
	for _, g := range gs {
		if g.ID == id {
			return g, nil
		}
	}
	return Group{}, ErrNotFound
}

// Put inserts or replaces a group, preserving the position of existing ids.
func (s *FileStore) Put(_ context.Context, group Group) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	gs, err := s.load()
	if err != nil {
		return err
	}
	for i := range gs {
		if gs[i].ID == group.ID {
			gs[i] = group
			return s.save(gs)
		}
	}
	return s.save(append(gs, group))
}

// Delete removes a group; unknown ids report ErrNotFound.
func (s *FileStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	gs, err := s.load()
	if err != nil {
		return err
	}
	for i := range gs {
		if gs[i].ID == id {
			gs = append(gs[:i], gs[i+1:]...)
			return s.save(gs)
		}
	}
	return ErrNotFound
}
