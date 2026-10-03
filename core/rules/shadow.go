package rules

import (
	"sync"
	"time"
)

// DefaultShadowSampleInterval is the sampling policy applied to shadow-mode
// log records: the first hit of every rule is recorded in full, then one in
// every DefaultShadowSampleInterval hits, while the hit counter itself
// always advances so statistics stay exact.
const DefaultShadowSampleInterval = 100

// DefaultShadowSampleKeep is how many latest shadow-hit samples each rule
// keeps in the sampler: the rule-detail read surface shows the recent hit
// stream even when the delivery log's ring buffer has long rotated past it.
const DefaultShadowSampleKeep = 20

// shadowBucketKeep bounds the hourly counting buckets: windows are served
// up to 7 days, so buckets older than that (plus a one-hour slack) are
// dropped on write. The slack keeps a bucket alive while a 7d query at the
// top of an hour can still see the bucket it starts in.
const shadowBucketKeep = 7*24*time.Hour + time.Hour

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

// shadowBucket is one hour of hits in a (kind, rule) stream, the
// granularity trailing windows are answered from. Per-hit timestamps would
// make windows exact but unbounded in memory under high QPS; hours are the
// standard trade (a 24h/7d window is answered by summing ≤ 24/169
// buckets).
type shadowBucket struct {
	hour int64 // unix seconds at the hour's start
	n    uint64
}

// ShadowSample is one recorded shadow hit: the evidence stream a rule's
// detail view serves, independent of the delivery log's sampling and ring
// rotation.
type ShadowSample struct {
	At       time.Time `json:"at"`
	Type     string    `json:"type,omitempty"`
	Level    string    `json:"level,omitempty"`
	Title    string    `json:"title,omitempty"`
	Channels []string  `json:"channels,omitempty"`
}

// ShadowStats is the per-rule read model the rules API serves: the exact
// total behind the sampled log entries, trailing-window counts and the
// latest samples. Process-local observation state — restart resets it.
type ShadowStats struct {
	Total   uint64         `json:"total"`
	Last24h uint64         `json:"last_24h"`
	Last7d  uint64         `json:"last_7d"`
	Samples []ShadowSample `json:"samples,omitempty"`
}

// ShadowSampler implements the per-rule sampling policy for shadow-mode
// observation: every (kind, rule) stream keeps its own budget. High-QPS
// notifications must not flood the delivery log with shadow entries;
// consumers read the counters for exact totals and the log stream for
// representative samples. On top of the counters it keeps hourly buckets
// (for trailing 24h/7d windows) and a ring of the latest shadow-hit
// samples per rule (for the rule-detail read surface).
type ShadowSampler struct {
	mu         sync.Mutex
	counts     map[eventKey]uint64
	buckets    map[eventKey][]shadowBucket
	samples    map[string][]ShadowSample
	sampleKeep int
	interval   uint64
	// now is the clock shared by counters, buckets and samples; swapped
	// in tests.
	now func() time.Time
}

// NewShadowSampler creates a sampler with the given interval; 0 records
// every hit.
func NewShadowSampler(interval uint64) *ShadowSampler {
	return &ShadowSampler{
		counts:     make(map[eventKey]uint64),
		buckets:    make(map[eventKey][]shadowBucket),
		samples:    make(map[string][]ShadowSample),
		sampleKeep: DefaultShadowSampleKeep,
		interval:   interval,
		now:        time.Now,
	}
}

// NewDefaultShadowSampler creates a sampler with DefaultShadowSampleInterval.
func NewDefaultShadowSampler() *ShadowSampler {
	return NewShadowSampler(DefaultShadowSampleInterval)
}

// ShouldRecord advances the hit counter for (kind, ruleID) and reports
// whether this hit should be recorded as a full log entry. The same call
// folds the hit into the hourly bucket series so trailing windows stay
// exact to the hour without a second write path.
func (s *ShadowSampler) ShouldRecord(kind EventKind, ruleID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := eventKey{kind: kind, ruleID: ruleID}
	s.counts[key]++
	s.advance(key, s.now().Unix()/3600)
	if s.interval == 0 {
		return true
	}
	n := s.counts[key]
	return n == 1 || n%s.interval == 0
}

// advance folds one hit into the bucket series and drops buckets older
// than the keep window. Callers hold mu.
func (s *ShadowSampler) advance(key eventKey, hour int64) {
	buckets := s.buckets[key]
	if n := len(buckets); n > 0 && buckets[n-1].hour == hour {
		buckets[n-1].n++
	} else {
		buckets = append(buckets, shadowBucket{hour: hour, n: 1})
	}
	cutoff := hour - int64(shadowBucketKeep/time.Hour)
	drop := 0
	for drop < len(buckets) && buckets[drop].hour < cutoff {
		drop++
	}
	if drop > 0 {
		buckets = buckets[drop:]
	}
	s.buckets[key] = buckets
}

// AddSample appends a shadow-hit sample to the rule's ring (latest last,
// capped at the keep size). The timestamp comes from the sampler clock so
// samples, counters and buckets share one time base.
func (s *ShadowSampler) AddSample(ruleID string, sample ShadowSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sample.At = s.now()
	list := append(s.samples[ruleID], sample)
	if overflow := len(list) - s.sampleKeep; overflow > 0 {
		list = list[overflow:]
	}
	s.samples[ruleID] = list
}

// Count returns the total number of hits observed for (kind, ruleID) so
// far. Sampling never hides a hit from the counter, only from the log.
func (s *ShadowSampler) Count(kind EventKind, ruleID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[eventKey{kind: kind, ruleID: ruleID}]
}

// WindowCount returns the hits recorded for (kind, ruleID) within the
// trailing window, folded to whole hours: every bucket whose hour starts
// inside the window counts in full (the tumbling-hour approximation — a
// query at 14:30 with a 24h window sums the buckets from yesterday 14:00
// on). Windows under an hour read the current bucket.
func (s *ShadowSampler) WindowCount(kind EventKind, ruleID string, window time.Duration) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	hours := int64(window / time.Hour)
	if hours < 1 {
		hours = 1
	}
	nowHour := s.now().Unix() / 3600
	return s.windowCount(eventKey{kind: kind, ruleID: ruleID}, nowHour-hours+1)
}

// windowCount sums the buckets at or after the from hour. Callers hold mu.
func (s *ShadowSampler) windowCount(key eventKey, from int64) uint64 {
	var total uint64
	for _, b := range s.buckets[key] {
		if b.hour >= from {
			total += b.n
		}
	}
	return total
}

// Stats returns the shadow-mode statistics of one rule: the exact total,
// the trailing 24h/7d windows and the latest samples (newest first, a
// defensive copy — callers may keep it around). Only shadow hits count,
// like the total; withheld-for-other-reasons kinds keep their own counts.
func (s *ShadowSampler) Stats(ruleID string) ShadowStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowHour := s.now().Unix() / 3600
	key := eventKey{kind: KindShadow, ruleID: ruleID}
	stats := ShadowStats{
		Total:   s.counts[key],
		Last24h: s.windowCount(key, nowHour-24+1),
		Last7d:  s.windowCount(key, nowHour-7*24+1),
	}
	if list := s.samples[ruleID]; len(list) > 0 {
		stats.Samples = make([]ShadowSample, len(list))
		for i := range list {
			stats.Samples[len(list)-1-i] = list[i]
		}
	}
	return stats
}
