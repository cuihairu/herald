package rules

import "sync"

// DefaultShadowSampleInterval is the sampling policy applied to shadow-mode
// log records: the first hit of every rule is recorded in full, then one in
// every DefaultShadowSampleInterval hits, while the hit counter itself
// always advances so statistics stay exact.
const DefaultShadowSampleInterval = 100

// EventKind names one class of rule-engine observation. The value is also
// the delivery-log status the observation is written under, so the log
// filter and the sampler key read the same word.
//
// Kinds are counted and sampled independently, for two reasons. A rule
// drowning in `suppressed` or `folded` traffic must not spend the sampling
// budget of its shadow hits (first hit recorded, then every Nth — the
// shadow sample stream is the only preview of what activation would send).
// And "how often would this rule have fired" is only answerable if the
// withheld events are not counted as hits.
type EventKind string

const (
	// KindShadow is a shadow-mode match: recorded, never delivered.
	KindShadow EventKind = "shadow"
	// KindForPending is an event held back by a running "for" window.
	KindForPending EventKind = "pending"
	// KindGroupFolded is an event folded into an open group round.
	KindGroupFolded EventKind = "folded"
	// KindInhibited is an event dropped because an inhibiting rule matches.
	KindInhibited EventKind = "inhibited"
	// KindSilenced is an event withheld by the rule's silence window.
	KindSilenced EventKind = "silenced"
	// KindSuppressed is an event dropped by a suppress action or by the
	// default deny policy (the rule id is empty there — the configuration
	// decided).
	KindSuppressed EventKind = "suppressed"
)

// eventKey is the sampler bucket: one (kind, rule) observation stream.
type eventKey struct {
	kind   EventKind
	ruleID string
}

// ShadowSampler implements the per-rule sampling policy for shadow-mode
// observation: every (kind, rule) stream keeps its own budget. High-QPS
// notifications must not flood the delivery log with shadow entries;
// consumers read the counters for exact totals and the log stream for
// representative samples.
type ShadowSampler struct {
	mu       sync.Mutex
	counts   map[eventKey]uint64
	interval uint64
}

// NewShadowSampler creates a sampler with the given interval; 0 records
// every hit.
func NewShadowSampler(interval uint64) *ShadowSampler {
	return &ShadowSampler{counts: make(map[eventKey]uint64), interval: interval}
}

// NewDefaultShadowSampler creates a sampler with DefaultShadowSampleInterval.
func NewDefaultShadowSampler() *ShadowSampler {
	return NewShadowSampler(DefaultShadowSampleInterval)
}

// ShouldRecord advances the hit counter for (kind, ruleID) and reports
// whether this hit should be recorded as a full log entry.
func (s *ShadowSampler) ShouldRecord(kind EventKind, ruleID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := eventKey{kind: kind, ruleID: ruleID}
	s.counts[key]++
	if s.interval == 0 {
		return true
	}
	n := s.counts[key]
	return n == 1 || n%s.interval == 0
}

// Count returns the total number of hits observed for (kind, ruleID) so
// far. Sampling never hides a hit from the counter, only from the log.
func (s *ShadowSampler) Count(kind EventKind, ruleID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[eventKey{kind: kind, ruleID: ruleID}]
}
