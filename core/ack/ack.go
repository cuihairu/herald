// Package ack records alert acknowledgements. The id is the caller's alert
// identity (any string the caller uses consistently — e.g. the incident or
// alert id it put in the notification params): the ack store only keys on
// it. Escalation rules (P3) read this store to decide whether a pending
// escalation should still fire.
package ack

import (
	"context"
	"sync"
	"time"
)

// Record is one acknowledgement. AckedAt is set by the store when the ack
// is first recorded; repeated acks for the same id keep the first record.
type Record struct {
	AlertID string `json:"alert_id"`
	// AckedBy is a free-form identity of who acknowledged (user, handle).
	AckedBy string    `json:"acked_by,omitempty"`
	AckedAt time.Time `json:"acked_at"`
	// Source names the entry point: "api" today, card callbacks later.
	Source string `json:"source"`
}

// Store persists acknowledgements keyed by alert id. Implementations must
// be safe for concurrent use.
type Store interface {
	// Ack records the acknowledgement under alertID. Recording an id that
	// is already acknowledged keeps the existing record (first ack wins —
	// the acknowledgement time is a fact, not a counter) and returns it.
	Ack(ctx context.Context, alertID string, ackedBy string, source string) (*Record, error)
	// Get returns the record under alertID, or (nil, nil) when the alert
	// is not acknowledged.
	Get(ctx context.Context, alertID string) (*Record, error)
	// Delete removes the acknowledgement; deleting an absent id is a no-op.
	Delete(ctx context.Context, alertID string) error
	// Close releases underlying resources. Safe to call more than once.
	Close() error
}

// MemoryStore is the in-process ack store: a mutex-guarded map. Records
// live for the process lifetime — acks survive as long as the process
// does; a shared backend lands with the escalation work that needs it.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
}

// NewMemoryStore creates an empty in-memory ack store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]Record)}
}

// Ack records the acknowledgement; the first one for an id wins.
func (m *MemoryStore) Ack(_ context.Context, alertID string, ackedBy string, source string) (*Record, error) {
	if alertID == "" {
		return nil, ErrEmptyID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.records[alertID]; ok {
		return &rec, nil
	}
	rec := Record{
		AlertID: alertID,
		AckedBy: ackedBy,
		AckedAt: time.Now(),
		Source:  source,
	}
	m.records[alertID] = rec
	return &rec, nil
}

// Get returns the record under alertID, or (nil, nil) when absent.
func (m *MemoryStore) Get(_ context.Context, alertID string) (*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.records[alertID]; ok {
		return &rec, nil
	}
	return nil, nil
}

// Delete removes the acknowledgement; absent ids are a no-op.
func (m *MemoryStore) Delete(_ context.Context, alertID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, alertID)
	return nil
}

// Close resets the store to empty.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = make(map[string]Record)
	return nil
}
