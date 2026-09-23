package rules

import "sync"

// DefaultShadowSampleInterval is the sampling policy applied to shadow-mode
// log records: the first hit of every rule is recorded in full, then one in
// every DefaultShadowSampleInterval hits, while the hit counter itself
// always advances so statistics stay exact.
const DefaultShadowSampleInterval = 100

// ShadowSampler implements the per-rule sampling policy for shadow-mode
// observation. High-QPS notifications must not flood the delivery log with
// shadow entries; consumers read the counters for exact totals and the log
// stream for representative samples.
type ShadowSampler struct {
	mu       sync.Mutex
	counts   map[string]uint64
	interval uint64
}

// NewShadowSampler creates a sampler with the given interval; 0 records
// every hit.
func NewShadowSampler(interval uint64) *ShadowSampler {
	return &ShadowSampler{counts: make(map[string]uint64), interval: interval}
}

// NewDefaultShadowSampler creates a sampler with DefaultShadowSampleInterval.
func NewDefaultShadowSampler() *ShadowSampler {
	return NewShadowSampler(DefaultShadowSampleInterval)
}

// ShouldRecord advances the hit counter for ruleID and reports whether
// this hit should be recorded as a full log entry.
func (s *ShadowSampler) ShouldRecord(ruleID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[ruleID]++
	if s.interval == 0 {
		return true
	}
	n := s.counts[ruleID]
	return n == 1 || n%s.interval == 0
}

// Count returns the total number of hits observed for ruleID so far.
func (s *ShadowSampler) Count(ruleID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[ruleID]
}
