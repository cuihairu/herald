package roster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// fileFormat is the on-disk envelope of a rosters file. Version lets future
// formats migrate without guessing.
type fileFormat struct {
	Version int      `json:"version"`
	Rosters []Roster `json:"rosters"`
}

const fileFormatVersion = 1

// FileStore persists rosters in a JSON file. Every write rewrites the whole
// file through a temp file + rename, so readers never observe a torn
// write. Roster counts are small (tens), which makes read-modify-write the
// simplest correct choice — the same trade-off the rules and groups
// FileStores make.
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
func (s *FileStore) load() ([]Roster, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("roster: read %s: %w", s.path, err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("roster: parse %s: %w", s.path, err)
	}
	return f.Rosters, nil
}

// save atomically replaces the file with the given roster list.
func (s *FileStore) save(rs []Roster) error {
	if rs == nil {
		rs = []Roster{}
	}
	// Roster has only marshalable fields, so encoding cannot fail.
	data, _ := json.MarshalIndent(fileFormat{Version: fileFormatVersion, Rosters: rs}, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("roster: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil { // a directory target (or cross-device rename) surfaces here
		return fmt.Errorf("roster: replace %s: %w", s.path, err)
	}
	return nil
}

// List returns all rosters in stored order.
func (s *FileStore) List(_ context.Context) ([]Roster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Get returns one roster by id.
func (s *FileStore) Get(_ context.Context, id string) (Roster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, err := s.load()
	if err != nil {
		return Roster{}, err
	}
	for _, r := range rs {
		if r.ID == id {
			return r, nil
		}
	}
	return Roster{}, ErrNotFound
}

// Put inserts or replaces a roster, preserving stored position.
func (s *FileStore) Put(_ context.Context, roster Roster) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, err := s.load()
	if err != nil {
		return err
	}
	for i, r := range rs {
		if r.ID == roster.ID {
			rs[i] = roster
			return s.save(rs)
		}
	}
	rs = append(rs, roster)
	return s.save(rs)
}

// Delete removes a roster and closes the gap so list order stays dense.
func (s *FileStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, err := s.load()
	if err != nil {
		return err
	}
	for i, r := range rs {
		if r.ID == id {
			rs = append(rs[:i], rs[i+1:]...)
			return s.save(rs)
		}
	}
	return ErrNotFound
}
