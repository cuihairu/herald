package audit

import (
	"sort"
	"sync"
	"time"
)

// EventKind marks which side of the audience layer changed. Relation
// changes and contact-surface changes share one stream but are filtered
// apart at read time — the §12 rule "审计按类型分开记" applies to the
// relation type field on the event, not to separate streams. Delivery-side
// rows (dedup folds) land in the same store so one audience trail reads
// end to end.
type EventKind string

const (
	RelationSubscribe EventKind = "relation.subscribe"
	RelationEnroll    EventKind = "relation.enroll"
	RelationTerminate EventKind = "relation.terminate"
	SurfaceBind       EventKind = "surface.bind"
	SurfaceRebind     EventKind = "surface.rebind"
	SurfaceInvalidate EventKind = "surface.invalidate"
	// DeliveryDeduped records a notification suppressed by content dedup:
	// Detail carries the content fingerprint (dedup key), Category the
	// notification type. Answers「为什么这条没投」.
	DeliveryDeduped EventKind = "delivery.deduped"
)

// Event is one audit-trail row. RelationType and Source carry the §4
// snapshot (which kind of relation, from which source adapter) for
// relation events; surface events leave them empty.
type Event struct {
	Kind         EventKind `json:"kind"`
	AudienceID   string    `json:"audience_id"`
	Category     string    `json:"category,omitempty"`
	Channel      string    `json:"channel,omitempty"`
	RelationType string    `json:"relation_type,omitempty"`
	Source       string    `json:"source,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	At           time.Time `json:"at"`
}

// Recorder is the write end the audience registries report to. Nil-safe
// by convention: registries only call Record when a recorder is set.
type Recorder interface {
	Record(Event)
}

// Store is an in-memory audit trail with the same FIFO-cap shape as the
// other registries — the interface is the contract, storage can swap.
type Store struct {
	mu     sync.RWMutex
	events []Event
	limit  int
	now    func() time.Time
}

// New creates an empty store keeping at most limit events (default 1000).
func New(limit int) *Store {
	if limit <= 0 {
		limit = 1000
	}
	return &Store{limit: limit, now: time.Now}
}

// SetClock injects the event clock (deterministic tests).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// Record appends one event, stamping At when the caller left it zero.
func (s *Store) Record(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.At.IsZero() {
		e.At = s.now()
	}
	s.events = append(s.events, e)
	if len(s.events) > s.limit {
		s.events = s.events[len(s.events)-s.limit:]
	}
}

// List returns all retained events in chronological order.
func (s *Store) List() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// ListByRelationType returns only events of one relation type
// (subscription/enrollment) — the §4 separation at read time.
func (s *Store) ListByRelationType(t string) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Event{}
	for _, e := range s.events {
		if e.RelationType == t {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// ListByAudience returns one audience's full trail across kinds.
func (s *Store) ListByAudience(audienceID string) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Event{}
	for _, e := range s.events {
		if e.AudienceID == audienceID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// ListBySource returns one entry source's trail — the §13.4 app query
// face reads its namespace by the "app:<name>" source the dispatch face
// stamps — strictly after since (zero time reads the whole trail).
func (s *Store) ListBySource(source string, since time.Time) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Event{}
	for _, e := range s.events {
		if e.Source != source {
			continue
		}
		if !since.IsZero() && !e.At.After(since) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}
