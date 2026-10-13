package audience

import (
	"context"
	"errors"
	"sync"
)

// Entry is one audience row in a store: the id plus the recipients it
// aggregates. The config seed and the runtime API both reduce to this
// shape, which is what the JSON file persists.
type Entry struct {
	ID         string   `json:"id"`
	Recipients []string `json:"recipients"`
}

// ErrNotFound is returned by Store.Delete for unknown ids.
var ErrNotFound = errors.New("audience: audience not found")

// Store persists audiences in list order.
type Store interface {
	List(ctx context.Context) ([]Entry, error)
	Put(ctx context.Context, entry Entry) error
	Delete(ctx context.Context, id string) error
}

// MemoryStore is an in-process Store implementation.
type MemoryStore struct {
	mu    sync.RWMutex
	order []string
	byID  map[string]Entry
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[string]Entry)}
}

// List returns all audiences in stored order.
func (s *MemoryStore) List(_ context.Context) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, len(s.order))
	for i, id := range s.order {
		out[i] = s.byID[id]
	}
	return out, nil
}

// Put inserts or replaces an audience, preserving the position of
// existing ids.
func (s *MemoryStore) Put(_ context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[entry.ID]; !ok {
		s.order = append(s.order, entry.ID)
	}
	s.byID[entry.ID] = entry
	return nil
}

// Delete removes an audience and closes the gap so list order stays
// dense; unknown ids report ErrNotFound.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	for i, existing := range s.order {
		if existing == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	delete(s.byID, id)
	return nil
}
