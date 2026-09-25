package groups

import (
	"context"
	"errors"
	"sync"
)

// ErrNotFound is returned by Store.Get and Store.Delete for unknown ids.
var ErrNotFound = errors.New("groups: group not found")

// Store persists groups in list order.
type Store interface {
	List(ctx context.Context) ([]Group, error)
	Get(ctx context.Context, id string) (Group, error)
	Put(ctx context.Context, group Group) error
	Delete(ctx context.Context, id string) error
}

// MemoryStore is an in-process Store implementation.
type MemoryStore struct {
	mu     sync.RWMutex
	groups []Group
	byID   map[string]int
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[string]int)}
}

// NewMemoryStoreWith creates a MemoryStore pre-loaded with groups (in order).
func NewMemoryStoreWith(gs ...Group) *MemoryStore {
	s := NewMemoryStore()
	for _, g := range gs {
		_ = s.Put(context.Background(), g)
	}
	return s
}

// List returns all groups in stored order.
func (s *MemoryStore) List(_ context.Context) ([]Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Group, len(s.groups))
	copy(out, s.groups)
	return out, nil
}

// Get returns one group by id.
func (s *MemoryStore) Get(_ context.Context, id string) (Group, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := s.byID[id]
	if !ok {
		return Group{}, ErrNotFound
	}
	return s.groups[i], nil
}

// Put inserts or replaces a group, preserving the position of existing ids.
func (s *MemoryStore) Put(_ context.Context, group Group) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.byID[group.ID]; ok {
		s.groups[i] = group
		return nil
	}
	s.byID[group.ID] = len(s.groups)
	s.groups = append(s.groups, group)
	return nil
}

// Delete removes a group and closes the gap so list order stays dense.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.byID[id]
	if !ok {
		return ErrNotFound
	}
	s.groups = append(s.groups[:i], s.groups[i+1:]...)
	delete(s.byID, id)
	for id2, idx := range s.byID {
		if idx > i {
			s.byID[id2] = idx - 1
		}
	}
	return nil
}
