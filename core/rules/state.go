package rules

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RuleState is the durable state of one rule's in-progress "for" window for
// a single alert group (see ForGroupKey). It lives in a StateStore so that
// process restarts and multi-instance deployments see the same duration
// judgement.
type RuleState struct {
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     uint64    `json:"count"`
	// Fired marks that the window already elapsed once for this group and
	// the alert went out; further hits stay silent until the rule's match
	// stops holding and the state is reset.
	Fired bool `json:"fired"`
}

// StateStore persists rule evaluation state keyed by the full state key
// (see stateKey). Implementations must be safe for concurrent use.
type StateStore interface {
	// Get returns the state stored under key, or (nil, nil) when absent.
	Get(ctx context.Context, key string) (*RuleState, error)
	// Put stores state under key. A positive ttl expires the entry;
	// ttl <= 0 stores it without expiry.
	Put(ctx context.Context, key string, s *RuleState, ttl time.Duration) error
	// Delete removes the entry under key; deleting an absent key is a no-op.
	Delete(ctx context.Context, key string) error
	// DeleteRule removes every state entry belonging to ruleID (any group).
	DeleteRule(ctx context.Context, ruleID string) error
	// Close releases underlying resources. Safe to call more than once.
	Close() error
}

// stateKey builds the durable key for one rule's group state, following the
// design document: rule:{rule_id}:state:{group_hash}.
func stateKey(ruleID, groupKey string) string {
	return "rule:" + ruleID + ":state:" + groupKey
}

// MemoryStateStore is the in-process StateStore: a mutex-guarded map with
// lazy expiry. It is the default for single-instance deployments; state is
// lost on restart, which only re-arms pending "for" windows.
type MemoryStateStore struct {
	mu     sync.Mutex
	states map[string]memoryStateEntry
}

type memoryStateEntry struct {
	state    RuleState
	expireAt time.Time
	hasTTL   bool
}

// NewMemoryStateStore creates an empty in-memory state store.
func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{states: make(map[string]memoryStateEntry)}
}

// Get returns the live state under key, or (nil, nil) when absent or expired.
func (m *MemoryStateStore) Get(_ context.Context, key string) (*RuleState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.states[key]
	if !ok {
		return nil, nil
	}
	if entry.hasTTL && !time.Now().Before(entry.expireAt) {
		delete(m.states, key)
		return nil, nil
	}
	state := entry.state
	return &state, nil
}

// Put stores state under key, replacing any previous entry.
func (m *MemoryStateStore) Put(_ context.Context, key string, s *RuleState, ttl time.Duration) error {
	if s == nil {
		return fmt.Errorf("rules: memory state store: put nil state")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := memoryStateEntry{state: *s}
	if ttl > 0 {
		entry.hasTTL = true
		entry.expireAt = time.Now().Add(ttl)
	}
	m.states[key] = entry
	return nil
}

// Delete removes the entry under key; absent keys are a no-op.
func (m *MemoryStateStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, key)
	return nil
}

// DeleteRule removes every entry whose key belongs to ruleID. The prefix
// covers both state families — for windows (rule:{id}:state:*) and group
// rounds (rule:{id}:group:*) — so a rule change drops them together.
func (m *MemoryStateStore) DeleteRule(_ context.Context, ruleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := "rule:" + ruleID + ":"
	for key := range m.states {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(m.states, key)
		}
	}
	return nil
}

// Close resets the store to empty.
func (m *MemoryStateStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = make(map[string]memoryStateEntry)
	return nil
}
