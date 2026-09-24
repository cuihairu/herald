// Package incident keeps the incident ledger: the lifecycle record of one
// alert episode — opened when a rule routes its delivery, marked when a
// human acknowledges it, closed when the root cause recovers. An incident
// is addressable in both identity spaces the alert system uses: the
// engine's group key (recovery) and the caller's alert id shared with the
// ack store and the escalation manager (acknowledgement, escalation).
package incident

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Entry is one milestone on an incident's timeline (escalation fired,
// escalation delivery failed, ...). Ack and resolve are first-class fields,
// not entries: they are the states callers query.
type Entry struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail,omitempty"`
}

// Incident is the ledger record of one alert episode.
type Incident struct {
	ID      string `json:"id"`
	RuleID  string `json:"rule_id"`
	AlertID string `json:"alert_id"`
	// GroupKey is the engine's identity of the alert group (content hash
	// or group_by field values): the recovery side of the ledger.
	GroupKey string `json:"group_key,omitempty"`
	Title    string `json:"title,omitempty"`
	Level    string `json:"level,omitempty"`

	OpenedAt time.Time  `json:"opened_at"`
	AckedAt  *time.Time `json:"acked_at,omitempty"`
	AckedBy  string     `json:"acked_by,omitempty"`

	// ResolvedAt closes the episode: the rule's match stopped holding for
	// a fired group and the recovery summary went out.
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	// Duration is the observed episode length (opened → resolved), filled
	// on resolve. Milliseconds.
	Duration int64 `json:"duration,omitempty"`
	// Events is how many matching events the group saw while open,
	// reported by the recovery event.
	Events   uint64   `json:"events,omitempty"`
	Channels []string `json:"channels,omitempty"`

	Timeline []Entry `json:"timeline,omitempty"`
}

// Status classifies an incident for filtering.
type Status string

const (
	StatusOpen     Status = "open"
	StatusAcked    Status = "acked"
	StatusResolved Status = "resolved"
)

// Status derives the incident's current state.
func (i *Incident) Status() Status {
	switch {
	case i.ResolvedAt != nil:
		return StatusResolved
	case i.AckedAt != nil:
		return StatusAcked
	default:
		return StatusOpen
	}
}

// Opening describes a new (or continuing) episode at first delivery.
type Opening struct {
	RuleID   string
	GroupKey string
	AlertID  string
	Title    string
	Level    string
	Channels []string
}

// Store keeps incidents in memory (bounded: resolved episodes fall off
// first, open ones are never dropped silently). Implementations of the
// daemon start one per process; the ledger is a query view, not the
// source of truth for delivery.
type Store struct {
	mu        sync.Mutex
	incidents []*Incident
	limit     int
}

// New creates a store holding at most limit incidents; zero picks 1000.
func New(limit int) *Store {
	if limit <= 0 {
		limit = 1000
	}
	return &Store{incidents: make([]*Incident, 0, limit), limit: limit}
}

// Open records the start of an episode for the alert identity. It is
// idempotent while the episode runs: a repeated delivery of the same alert
// refreshes the context and returns the open incident. After a resolve,
// the same identity opens a new episode — regressions are new incidents,
// not revivals.
func (s *Store) Open(o Opening) *Incident {
	if o.AlertID == "" || o.RuleID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, inc := range s.incidents {
		if inc.RuleID == o.RuleID && inc.AlertID == o.AlertID && inc.ResolvedAt == nil {
			s.refreshLocked(inc, o)
			return inc
		}
	}
	inc := &Incident{
		ID:       uuid.New().String(),
		RuleID:   o.RuleID,
		AlertID:  o.AlertID,
		GroupKey: o.GroupKey,
		Title:    o.Title,
		Level:    o.Level,
		OpenedAt: time.Now(),
		Channels: o.Channels,
	}
	s.incidents = append(s.incidents, inc)
	s.evictLocked()
	return inc
}

func (s *Store) refreshLocked(inc *Incident, o Opening) {
	if o.Title != "" {
		inc.Title = o.Title
	}
	if o.Level != "" {
		inc.Level = o.Level
	}
	if o.GroupKey != "" {
		inc.GroupKey = o.GroupKey
	}
	if len(o.Channels) > 0 {
		inc.Channels = o.Channels
	}
}

// Ack marks the open episode of alertID acknowledged. It reports whether
// an open incident was found (an ack with no open episode is fine — the
// caller may acknowledge an alert herald never routed).
func (s *Store) Ack(alertID, ackedBy string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, inc := range s.incidents {
		if inc.AlertID == alertID && inc.ResolvedAt == nil {
			if inc.AckedAt == nil {
				now := time.Now()
				inc.AckedAt = &now
				inc.AckedBy = ackedBy
			}
			return true
		}
	}
	return false
}

// Resolve closes the open episode of (ruleID, groupKey) with the recovery
// facts and returns it (nil when the ledger tracks no such open episode).
func (s *Store) Resolve(ruleID, groupKey string, events uint64) *Incident {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, inc := range s.incidents {
		if inc.RuleID == ruleID && inc.GroupKey == groupKey && inc.ResolvedAt == nil {
			inc.ResolvedAt = &now
			inc.Events = events
			if now.After(inc.OpenedAt) {
				inc.Duration = now.Sub(inc.OpenedAt).Milliseconds()
			}
			return inc
		}
	}
	return nil
}

// Append adds a milestone to the open episode's timeline (escalation
// fired, escalation delivery failed, ...), addressed by the caller's
// alert id. Unknown episodes are ignored: the ledger records milestones
// of episodes it tracks.
func (s *Store) Append(ruleID, alertID, kind, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, inc := range s.incidents {
		if inc.RuleID == ruleID && inc.AlertID == alertID && inc.ResolvedAt == nil {
			inc.Timeline = append(inc.Timeline, Entry{At: time.Now(), Kind: kind, Detail: detail})
			return
		}
	}
}

// AppendTo adds a milestone to one specific episode by its id. Resolution-
// time milestones need this: the episode is already resolved when its
// recovery summary fails to deliver, and Append only reaches open ones.
func (s *Store) AppendTo(id, kind, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, inc := range s.incidents {
		if inc.ID == id {
			inc.Timeline = append(inc.Timeline, Entry{At: time.Now(), Kind: kind, Detail: detail})
			return
		}
	}
}

// Get returns one incident by id.
func (s *Store) Get(id string) *Incident {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, inc := range s.incidents {
		if inc.ID == id {
			return inc
		}
	}
	return nil
}

// List returns incidents newest-first, optionally filtered.
func (s *Store) List(filter *Filter) []*Incident {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Incident, 0, len(s.incidents))
	for i := len(s.incidents) - 1; i >= 0; i-- {
		inc := s.incidents[i]
		if filter != nil && !filter.matches(inc) {
			continue
		}
		out = append(out, inc)
	}
	return out
}

// Filter selects incidents for List.
type Filter struct {
	Status  Status
	RuleID  string
	AlertID string
}

func (f *Filter) matches(inc *Incident) bool {
	if f.Status != "" && inc.Status() != f.Status {
		return false
	}
	if f.RuleID != "" && inc.RuleID != f.RuleID {
		return false
	}
	if f.AlertID != "" && inc.AlertID != f.AlertID {
		return false
	}
	return true
}

// evictLocked drops the oldest resolved episodes when over the limit;
// open episodes survive: losing an open incident would hide a live alert.
func (s *Store) evictLocked() {
	over := len(s.incidents) - s.limit
	if over <= 0 {
		return
	}
	kept := make([]*Incident, 0, len(s.incidents)-over)
	for _, inc := range s.incidents {
		if over > 0 && inc.ResolvedAt != nil {
			over--
			continue
		}
		kept = append(kept, inc)
	}
	s.incidents = kept
}
