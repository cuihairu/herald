package roster

import (
	"context"
	"errors"
	"sync"
)

// ErrNotFound is returned by Store.Get and Store.Delete for unknown ids.
var ErrNotFound = errors.New("roster: roster not found")

// Store persists rosters in list order.
type Store interface {
	List(ctx context.Context) ([]Roster, error)
	Get(ctx context.Context, id string) (Roster, error)
	Put(ctx context.Context, roster Roster) error
	Delete(ctx context.Context, id string) error
}

// MemoryStore is an in-process Store implementation.
type MemoryStore struct {
	mu     sync.RWMutex
	rosters []Roster
	byID   map[string]int
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[string]int)}
}

// NewMemoryStoreWith creates a MemoryStore pre-loaded with rosters (in order).
func NewMemoryStoreWith(rs ...Roster) *MemoryStore {
	s := NewMemoryStore()
	for _, r := range rs {
		_ = s.Put(context.Background(), r)
	}
	return s
}

// List returns all rosters in stored order.
func (s *MemoryStore) List(_ context.Context) ([]Roster, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Roster, len(s.rosters))
	copy(out, s.rosters)
	return out, nil
}

// Get returns one roster by id.
func (s *MemoryStore) Get(_ context.Context, id string) (Roster, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := s.byID[id]
	if !ok {
		return Roster{}, ErrNotFound
	}
	return s.rosters[i], nil
}

// Put inserts or replaces a roster, preserving the position of existing ids.
func (s *MemoryStore) Put(_ context.Context, roster Roster) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.byID[roster.ID]; ok {
		s.rosters[i] = roster
		return nil
	}
	s.byID[roster.ID] = len(s.rosters)
	s.rosters = append(s.rosters, roster)
	return nil
}

// Delete removes a roster and closes the gap so list order stays dense.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.byID, id)
	s.rosters = append(s.rosters[:i], s.rosters[i+1:]...)
	for id2, idx := range s.byID {
		if idx > i {
			s.byID[id2] = idx - 1
		}
	}
	return nil
}
