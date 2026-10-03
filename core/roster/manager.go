package roster

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Manager owns the live roster table. Writes validate, persist (when a
// store is attached) and refresh the live table in place — the silence
// gate reads only the in-memory snapshot through Covers, never the disk.
// Rosters are data, not judgements: there is no shadow mode.
type Manager struct {
	mu      sync.RWMutex
	store   Store // nil = memory only
	rosters []*Roster
}

// NewManager creates a Manager backed by store (nil keeps rosters in
// memory only). Call Reload once after creation when a store is attached.
func NewManager(store Store) *Manager {
	return &Manager{store: store}
}

// Reload re-reads every roster from the store into the live table. A
// roster that fails validation aborts the reload keeping the previous
// table serving, naming the offender.
func (m *Manager) Reload(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	stored, err := m.store.List(ctx)
	if err != nil {
		return fmt.Errorf("roster: store list: %w", err)
	}
	table := make([]*Roster, 0, len(stored))
	for i := range stored {
		r := stored[i]
		r.Normalize()
		if err := r.Validate(); err != nil {
			return fmt.Errorf("roster: reload stopped at roster %q: %w", r.ID, err)
		}
		table = append(table, &r)
	}
	m.mu.Lock()
	m.rosters = table
	m.mu.Unlock()
	return nil
}

// Put validates then persists a roster and refreshes the live table in
// place (existing ids keep their position, new ids go last).
func (m *Manager) Put(ctx context.Context, r *Roster) error {
	r.Normalize()
	if err := r.Validate(); err != nil {
		return err
	}
	if m.store != nil {
		if err := m.store.Put(ctx, *r); err != nil {
			return fmt.Errorf("roster: store put: %w", err)
		}
	}
	stored := *r
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.rosters {
		if existing.ID == r.ID {
			m.rosters[i] = &stored
			return nil
		}
	}
	m.rosters = append(m.rosters, &stored)
	return nil
}

// Delete removes a roster from the store and the live table. Silences
// referencing it are not rewritten — they fail open (the gate stops
// silencing), which is the safe direction: missing schedule data must
// never keep an alert quiet.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if m.store != nil {
		if err := m.store.Delete(ctx, id); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.rosters {
		if existing.ID == id {
			m.rosters = append(m.rosters[:i], m.rosters[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// Get returns a roster by id from the live table (the evaluation truth).
func (m *Manager) Get(ctx context.Context, id string) (Roster, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.rosters {
		if r.ID == id {
			return *r, nil
		}
	}
	return Roster{}, ErrNotFound
}

// List returns the live table in stored order.
func (m *Manager) List(ctx context.Context) ([]Roster, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Roster, len(m.rosters))
	for i, r := range m.rosters {
		out[i] = *r
	}
	return out, nil
}

// Covers reports whether the named roster's schedule covers t — the read
// face the silence gate consults. An unknown roster (never pushed, or
// deleted) covers nothing: no schedule data means the gate stays open.
// A nil Manager (no rosters configured — reachable as a typed nil inside
// the engine's RosterSource) covers nothing too, for the same reason.
func (m *Manager) Covers(id string, t time.Time) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.rosters {
		if r.ID == id {
			return r.Covers(t)
		}
	}
	return false
}
