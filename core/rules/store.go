package rules

import (
	"context"
	"errors"
	"sync"
)

// ErrNotFound is returned by Store.Get and Store.Delete for unknown ids.
var ErrNotFound = errors.New("rules: rule not found")

// Store persists rules in list order; the order is the evaluation
// priority (first matching active rule wins).
type Store interface {
	List(ctx context.Context) ([]Rule, error)
	Get(ctx context.Context, id string) (Rule, error)
	Put(ctx context.Context, rule Rule) error
	Delete(ctx context.Context, id string) error
}

// MemoryStore is an in-process Store implementation; the SQLite backend
// lands in a later phase behind the same interface.
type MemoryStore struct {
	mu    sync.RWMutex
	rules []Rule
	byID  map[string]int
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[string]int)}
}

// NewMemoryStoreWith creates a MemoryStore pre-loaded with rules (in order).
func NewMemoryStoreWith(rules ...Rule) *MemoryStore {
	s := NewMemoryStore()
	for _, r := range rules {
		_ = s.Put(context.Background(), r)
	}
	return s
}

// List returns all rules in stored order.
func (s *MemoryStore) List(_ context.Context) ([]Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Rule, len(s.rules))
	copy(out, s.rules)
	return out, nil
}

// Get returns one rule by id.
func (s *MemoryStore) Get(_ context.Context, id string) (Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := s.byID[id]
	if !ok {
		return Rule{}, ErrNotFound
	}
	return s.rules[i], nil
}

// Put inserts or replaces a rule, preserving the position of existing ids.
func (s *MemoryStore) Put(_ context.Context, rule Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.byID[rule.ID]; ok {
		s.rules[i] = rule
		return nil
	}
	s.byID[rule.ID] = len(s.rules)
	s.rules = append(s.rules, rule)
	return nil
}

// Delete removes a rule and closes the gap so list order stays dense.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	s.rules = append(s.rules[:i], s.rules[i+1:]...)
	delete(s.byID, id)
	for id2, idx := range s.byID {
		if idx > i {
			s.byID[id2] = idx - 1
		}
	}
	return nil
}
