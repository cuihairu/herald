package groups

import (
	"context"
	"fmt"
	"sync"
)

// Manager owns the live group table. Writes validate, persist (when a
// store is attached) and refresh the live table in place — the delivery
// hot path reads only the in-memory snapshot through Resolver, never the
// disk. Groups are data, not judgements: there is no shadow mode.
type Manager struct {
	mu     sync.RWMutex
	store  Store // nil = memory only
	groups []*Group
}

// NewManager creates a Manager backed by store (nil keeps groups in
// memory only). Call Reload once after creation when a store is attached.
func NewManager(store Store) *Manager {
	return &Manager{store: store}
}

// Reload re-reads every group from the store into the live table. A group
// that fails validation aborts the reload keeping the previous table
// serving, naming the offender.
func (m *Manager) Reload(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	stored, err := m.store.List(ctx)
	if err != nil {
		return fmt.Errorf("groups: store list: %w", err)
	}
	table := make([]*Group, 0, len(stored))
	for i := range stored {
		g := stored[i]
		g.Normalize()
		if err := g.Validate(); err != nil {
			return fmt.Errorf("groups: reload stopped at group %q: %w", g.ID, err)
		}
		table = append(table, &g)
	}
	m.mu.Lock()
	m.groups = table
	m.mu.Unlock()
	return nil
}

// Put validates then persists a group and refreshes the live table in
// place (existing ids keep their position, new ids go last).
func (m *Manager) Put(ctx context.Context, g *Group) error {
	g.Normalize()
	if err := g.Validate(); err != nil {
		return err
	}
	if m.store != nil {
		if err := m.store.Put(ctx, *g); err != nil {
			return fmt.Errorf("groups: store put: %w", err)
		}
	}
	stored := *g
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.groups {
		if existing.ID == g.ID {
			m.groups[i] = &stored
			return nil
		}
	}
	m.groups = append(m.groups, &stored)
	return nil
}

// Delete removes a group from the store and the live table. References to
// it are not cleaned up — a rule still naming the group fails loudly at
// delivery time (unknown group), which beats silently rewriting rules.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if m.store != nil {
		if err := m.store.Delete(ctx, id); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.groups {
		if existing.ID == id {
			m.groups = append(m.groups[:i], m.groups[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// Get returns a group by id from the live table (the delivery truth).
func (m *Manager) Get(ctx context.Context, id string) (Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, g := range m.groups {
		if g.ID == id {
			return *g, nil
		}
	}
	return Group{}, ErrNotFound
}

// List returns the live table in stored order.
func (m *Manager) List(ctx context.Context) ([]Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Group, len(m.groups))
	for i, g := range m.groups {
		out[i] = *g
	}
	return out, nil
}

// Resolver returns the read-only view the delivery path uses.
func (m *Manager) Resolver() *Resolver {
	return &Resolver{manager: m}
}

// Resolver expands group references against the manager's live snapshot.
// It is safe for concurrent use; returned members are copies.
type Resolver struct {
	manager *Manager
}

// ExpandGroup returns the members of a group by id (without the
// "group:" prefix). The boolean reports whether the group exists.
func (r *Resolver) ExpandGroup(id string) ([]Member, bool) {
	r.manager.mu.RLock()
	defer r.manager.mu.RUnlock()
	for _, g := range r.manager.groups {
		if g.ID != id {
			continue
		}
		members := make([]Member, len(g.Members))
		copy(members, g.Members)
		return members, true
	}
	return nil, false
}
