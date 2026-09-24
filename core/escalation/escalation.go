// Package escalation schedules ack-gated upgrade deliveries: when a rule
// with an escalation plan delivers, the manager arms a timer for the
// ack_timeout; if the alert (params "alert_id") is acknowledged in time the
// upgrade is cancelled, otherwise it fires on the plan's "to" channels.
//
// Unlike the P2 stateful semantics this cannot stay purely event-driven —
// the whole point is to act when nobody comes. Timers live in process and
// pending records are persisted to a JSON file, so a restart re-arms the
// remaining timers and escalates what already ran out (still honoring an
// ack that arrived while the process was down).
package escalation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cuihairu/herald/core/ack"
)

// Pending is one armed upgrade: scheduled when the rule's delivery went
// out, cancelled by an ack for the same alert id, or fired after Timeout.
type Pending struct {
	RuleID  string   `json:"rule_id"`
	AlertID string   `json:"alert_id"`
	To      []string `json:"to"`
	// Timeout is how long after CreatedAt the upgrade fires without an ack.
	Timeout time.Duration `json:"timeout"`
	// CreatedAt is when the delivery that armed this upgrade happened.
	CreatedAt time.Time `json:"created_at"`
	// Type/Level/Title carry the triggering notification's context so the
	// upgrade notification can say what is being escalated.
	Type  string `json:"type,omitempty"`
	Level string `json:"level,omitempty"`
	Title string `json:"title,omitempty"`
}

// Notifier delivers the upgrade when a pending escalation fires. Called on
// the timer goroutine; implementations must not block for long.
type Notifier interface {
	Escalate(p Pending)
}

const fileFormatVersion = 1

type fileFormat struct {
	Version  int       `json:"version"`
	Pendings []Pending `json:"pendings"`
}

// Manager owns the pending escalation table. Safe for concurrent use.
type Manager struct {
	mu       sync.Mutex
	pendings map[string]Pending // key: pendingKey(ruleID, alertID)
	timers   map[string]*time.Timer
	acks     ack.Store
	notify   Notifier
	// path persists pending records across restarts; empty keeps them in
	// memory only (a restart then silently drops pending upgrades).
	path string
	// now is swappable in tests.
	now func() time.Time
}

// pendingKey is the map key for one (rule, alert) upgrade.
func pendingKey(ruleID, alertID string) string {
	return ruleID + "\x1f" + alertID
}

// NewManager creates a manager consulting acks before firing and reporting
// fired upgrades to notify. When path is non-empty, pending records are
// persisted there and restored by Restore after a restart.
func NewManager(acks ack.Store, notify Notifier, path string) *Manager {
	return &Manager{
		pendings: make(map[string]Pending),
		timers:   make(map[string]*time.Timer),
		acks:     acks,
		notify:   notify,
		path:     path,
		now:      time.Now,
	}
}

// SetNow swaps the clock; tests only.
func (m *Manager) SetNow(now func() time.Time) {
	m.now = now
}

// SetNotifier replaces the escalation target. NewManager takes an initial
// notifier; this lets wiring attach the real one after the fact (the
// delivery service is constructed by the same layer that owns the server).
func (m *Manager) SetNotifier(n Notifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notify = n
}

// Schedule arms (or re-arms) the upgrade for (p.RuleID, p.AlertID). A
// repeated delivery of the same alert resets the window: the timeout then
// counts from the latest delivery, matching the "nobody looked at the
// latest one either" reading of escalation.
func (m *Manager) Schedule(ctx context.Context, p Pending) error {
	if p.AlertID == "" {
		return fmt.Errorf("escalation: pending alert id is required")
	}
	if p.RuleID == "" {
		return fmt.Errorf("escalation: pending rule id is required")
	}
	if p.Timeout <= 0 {
		return fmt.Errorf("escalation: pending timeout must be positive")
	}
	m.mu.Lock()
	m.stopTimerLocked(pendingKey(p.RuleID, p.AlertID))
	m.pendings[pendingKey(p.RuleID, p.AlertID)] = p
	m.armLocked(p)
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	return nil
}

// Cancel drops every pending upgrade for alertID (an ack arrived in time).
func (m *Manager) Cancel(ctx context.Context, alertID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := false
	for key, p := range m.pendings {
		if p.AlertID != alertID {
			continue
		}
		m.stopTimerLocked(key)
		delete(m.pendings, key)
		removed = true
	}
	if !removed {
		return nil
	}
	return m.saveLocked()
}

// Restore re-arms pending records loaded from the persistence file after a
// restart: records whose window already ran out are judged immediately
// (still honoring an ack from the down period), the rest get timers for
// the remaining time.
func (m *Manager) Restore(ctx context.Context) error {
	if m.path == "" {
		return nil
	}
	records, err := m.load()
	if err != nil {
		return err
	}
	now := m.now()
	notify := m.currentNotifier()
	for _, p := range records {
		remaining := p.Timeout - now.Sub(p.CreatedAt)
		if remaining <= 0 {
			// The window ran out while the process was down.
			m.judge(p, notify)
			continue
		}
		m.mu.Lock()
		key := pendingKey(p.RuleID, p.AlertID)
		m.stopTimerLocked(key)
		m.pendings[key] = p
		m.timers[key] = time.AfterFunc(remaining, func() { m.fire(key) })
		m.mu.Unlock()
	}
	return nil
}

// Close stops all timers. Pending records stay persisted: a later restart
// restores and judges them. Safe to call more than once.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, timer := range m.timers {
		timer.Stop()
		delete(m.timers, key)
	}
	m.pendings = make(map[string]Pending)
	return nil
}

// fire judges one pending escalation at its deadline; the timer goroutine.
func (m *Manager) fire(key string) {
	m.mu.Lock()
	p, ok := m.pendings[key]
	if !ok {
		// Cancelled between the timer firing and taking the lock.
		m.mu.Unlock()
		return
	}
	delete(m.pendings, key)
	if m.timers[key] != nil {
		delete(m.timers, key)
	}
	notify := m.notify
	if err := m.saveLocked(); err != nil {
		// Losing the persistence line after the decision is tolerable —
		// the in-memory table is already consistent.
		_ = err
	}
	m.mu.Unlock()
	m.judge(p, notify)
}

// currentNotifier reads the notifier under the lock.
func (m *Manager) currentNotifier() Notifier {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.notify
}

// judge decides one escalation outside the lock: acknowledged in time
// means no upgrade, otherwise the upgrade goes out once.
func (m *Manager) judge(p Pending, notify Notifier) {
	if m.acks != nil {
		rec, err := m.acks.Get(context.Background(), p.AlertID)
		if err == nil && rec != nil {
			// Acknowledged (possibly while the process was down).
			return
		}
	}
	if notify != nil {
		notify.Escalate(p)
	}
}

// armLocked schedules the fire timer for the pending stored under its key.
func (m *Manager) armLocked(p Pending) {
	remaining := p.Timeout - m.now().Sub(p.CreatedAt)
	if remaining < 0 {
		remaining = 0
	}
	key := pendingKey(p.RuleID, p.AlertID)
	m.timers[key] = time.AfterFunc(remaining, func() { m.fire(key) })
}

// stopTimerLocked stops and drops the timer under key. Callers hold mu.
func (m *Manager) stopTimerLocked(key string) {
	if timer, ok := m.timers[key]; ok {
		timer.Stop()
		delete(m.timers, key)
	}
}

// load reads the pending records; a missing file counts as empty.
func (m *Manager) load() ([]Pending, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("escalation: read %s: %w", m.path, err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("escalation: parse %s: %w", m.path, err)
	}
	if f.Version != fileFormatVersion {
		return nil, fmt.Errorf("escalation: %s: unknown format version %d", m.path, f.Version)
	}
	return f.Pendings, nil
}

// saveLocked atomically replaces the persistence file with the live
// pending table. Callers hold mu.
func (m *Manager) saveLocked() error {
	if m.path == "" {
		return nil
	}
	if m.pendings == nil {
		m.pendings = make(map[string]Pending)
	}
	list := make([]Pending, 0, len(m.pendings))
	for _, p := range m.pendings {
		list = append(list, p)
	}
	data, err := json.MarshalIndent(fileFormat{Version: fileFormatVersion, Pendings: list}, "", "  ")
	if err != nil { // coverage: unreachable — Pending has only marshalable fields
		return fmt.Errorf("escalation: encode: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("escalation: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, m.path); err != nil { // coverage: same directory, so never cross-device
		return fmt.Errorf("escalation: replace %s: %w", m.path, err)
	}
	return nil
}
