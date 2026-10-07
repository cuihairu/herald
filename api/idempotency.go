package api

import (
	"sync"

	"github.com/cuihairu/herald/core/service"
)

// defaultNotifyIdempotencyCap bounds the in-memory idempotency table:
// results are evicted oldest-first beyond it, so a key keeps its replay
// guarantee only within roughly this many distinct notifications. A
// restart clears the table entirely — the MVP keeps the table in memory
// per the plan (docs/design-audience-model.md §24), with no persistence.
const defaultNotifyIdempotencyCap = 1000

// notifyIdempotency backs the idempotency_key contract of
// POST /api/v1/notify: a request carrying the same key within the process
// lifetime is served the recorded result again instead of delivering a
// second time. Only successfully processed results are recorded (an
// all-channels-failed request returns 422 and must be retryable).
type notifyIdempotency struct {
	mu    sync.Mutex
	table map[string]service.ProcessResult
	keys  []string // FIFO eviction order
	cap   int
}

func newNotifyIdempotency(capacity int) *notifyIdempotency {
	if capacity <= 0 {
		capacity = defaultNotifyIdempotencyCap
	}
	return &notifyIdempotency{
		table: make(map[string]service.ProcessResult),
		cap:   capacity,
	}
}

// get returns the recorded result for the key, if present.
func (s *notifyIdempotency) get(key string) (service.ProcessResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.table[key]
	return res, ok
}

// put records the result under the key. Re-putting a known key keeps the
// existing record (the first result is the canonical one).
func (s *notifyIdempotency) put(key string, res service.ProcessResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.table[key]; exists {
		return
	}
	if len(s.table) >= s.cap {
		oldest := s.keys[0]
		s.keys = s.keys[1:]
		delete(s.table, oldest)
	}
	s.table[key] = res
	s.keys = append(s.keys, key)
}
