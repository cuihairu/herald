package audience

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// fileFormat is the on-disk envelope of an audiences file. Version lets
// future formats migrate without guessing.
type fileFormat struct {
	Version   int     `json:"version"`
	Audiences []Entry `json:"audiences"`
}

const audienceFileFormatVersion = 1

// FileStore persists audiences in a JSON file. Every write rewrites the
// whole file through a temp file + rename, so readers never observe a
// torn write. Audience counts are small (tens), which makes
// read-modify-write the simplest correct choice — the same trade-off the
// groups and rules FileStores make.
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
func (s *FileStore) load() ([]Entry, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("audience: read %s: %w", s.path, err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("audience: parse %s: %w", s.path, err)
	}
	return f.Audiences, nil
}

// save atomically replaces the file with the given audience list.
func (s *FileStore) save(entries []Entry) error {
	if entries == nil {
		entries = []Entry{}
	}
	// Entry has only marshalable fields, so encoding cannot fail.
	data, _ := json.MarshalIndent(fileFormat{Version: audienceFileFormatVersion, Audiences: entries}, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("audience: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil { // a directory target (or cross-device rename) surfaces here
		return fmt.Errorf("audience: replace %s: %w", s.path, err)
	}
	return nil
}

// List returns all audiences in stored order.
func (s *FileStore) List(_ context.Context) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Put inserts or replaces an audience, preserving the position of
// existing ids.
func (s *FileStore) Put(_ context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].ID == entry.ID {
			entries[i] = entry
			return s.save(entries)
		}
	}
	return s.save(append(entries, entry))
}

// Delete removes an audience; unknown ids report ErrNotFound.
func (s *FileStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].ID == id {
			entries = append(entries[:i], entries[i+1:]...)
			return s.save(entries)
		}
	}
	return ErrNotFound
}
