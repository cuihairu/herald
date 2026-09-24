package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// fileFormat is the on-disk envelope of a rules file. Version lets future
// formats migrate without guessing.
type fileFormat struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

const fileFormatVersion = 1

// FileStore persists rules in a JSON file. Every write rewrites the whole
// file through a temp file + rename, so readers never observe a torn write.
// Rule counts are small (tens, not thousands), which makes read-modify-write
// the simplest correct choice; the Store interface keeps a SQLite/PostgreSQL
// backend swappable when state volume actually demands one.
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
func (s *FileStore) load() ([]Rule, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("rules: read %s: %w", s.path, err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("rules: parse %s: %w", s.path, err)
	}
	return f.Rules, nil
}

// save atomically replaces the file with the given rule list.
func (s *FileStore) save(rulesList []Rule) error {
	if rulesList == nil {
		rulesList = []Rule{}
	}
	// Rule has only marshalable fields, so encoding cannot fail.
	data, _ := json.MarshalIndent(fileFormat{Version: fileFormatVersion, Rules: rulesList}, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("rules: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil { // a directory target (or cross-device rename) surfaces here
		return fmt.Errorf("rules: replace %s: %w", s.path, err)
	}
	return nil
}

// List returns all rules in stored order.
func (s *FileStore) List(_ context.Context) ([]Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Get returns one rule by id.
func (s *FileStore) Get(_ context.Context, id string) (Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rulesList, err := s.load()
	if err != nil {
		return Rule{}, err
	}
	for _, r := range rulesList {
		if r.ID == id {
			return r, nil
		}
	}
	return Rule{}, ErrNotFound
}

// Put inserts or replaces a rule, preserving the position of existing ids.
func (s *FileStore) Put(_ context.Context, rule Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rulesList, err := s.load()
	if err != nil {
		return err
	}
	for i := range rulesList {
		if rulesList[i].ID == rule.ID {
			rulesList[i] = rule
			return s.save(rulesList)
		}
	}
	return s.save(append(rulesList, rule))
}

// Delete removes a rule; unknown ids report ErrNotFound.
func (s *FileStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rulesList, err := s.load()
	if err != nil {
		return err
	}
	for i := range rulesList {
		if rulesList[i].ID == id {
			rulesList = append(rulesList[:i], rulesList[i+1:]...)
			return s.save(rulesList)
		}
	}
	return ErrNotFound
}
