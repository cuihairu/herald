package deliverystore

import (
	"sort"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
)

// Store owns the delivery state machine: the accepted/queued/delivering/
// retrying/delivered/failed/dead lifecycle and retry progression of every
// task. It is keyed by task ID and carries its own retention, separate
// from the log store — the log records factual events (send/result/
// duration/error) and never sees these transitions.
type Store struct {
	mu    sync.RWMutex
	items map[string]*Delivery
	// order tracks insertion order for FIFO retention: the oldest task
	// drops first when the limit is exceeded.
	order []string
	limit int
}

// Delivery is the authoritative state-machine record of one delivery task.
// Status carries the core.DeliveryStatus vocabulary; RetryCount,
// MaxAttempts and NextRetryAt carry the retry progression. The log layer
// keeps its own factual outcome for the same attempt and is not derived
// from these fields.
type Delivery struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Level    string `json:"level,omitempty"`
	// Status is the delivery lifecycle status (core.DeliveryStatus wire
	// values). Empty decodes as queued, matching the task decode rule.
	Status      string     `json:"status"`
	RetryCount  int        `json:"retry_count"`
	MaxAttempts int        `json:"max_attempts,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`

	// Query dimensions and identity, snapped from the task so the
	// read face answers without re-parsing payloads.
	AudienceID   string `json:"audience_id,omitempty"`
	Category     string `json:"category,omitempty"`
	Source       string `json:"source,omitempty"`
	RelationType string `json:"relation_type,omitempty"`
	EventID      string `json:"event_id,omitempty"`
	AlertID      string `json:"alert_id,omitempty"`
}

// New creates a new delivery store.
func New(limit int) *Store {
	if limit <= 0 {
		limit = 10000
	}
	return &Store{
		items: make(map[string]*Delivery),
		limit: limit,
	}
}

// Put upserts the state-machine record for a task. The first write
// opens the row and starts its retention clock; later writes update the
// same row in place, so one task keeps exactly one row for its whole
// lifecycle.
func (s *Store) Put(d *Delivery) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = time.Now()
	}
	if _, ok := s.items[d.ID]; !ok {
		s.order = append(s.order, d.ID)
	}
	s.items[d.ID] = d

	for len(s.order) > s.limit {
		drop := s.order[0]
		s.order = s.order[1:]
		delete(s.items, drop)
	}
}

// Get retrieves the delivery record by ID.
func (s *Store) Get(id string) (*Delivery, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	d, ok := s.items[id]
	return d, ok
}

// List returns deliveries with optional filtering, newest first.
func (s *Store) List(offset, limit int, filter *Filter) []*Delivery {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filtered := s.filterItems(filter)

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})

	total := len(filtered)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*Delivery{}
	}

	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}

	return filtered[offset:end]
}

// Count returns the total count of filtered deliveries.
func (s *Store) Count(filter *Filter) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.filterItems(filter))
}

func (s *Store) filterItems(filter *Filter) []*Delivery {
	if filter == nil {
		return s.allItems()
	}

	result := make([]*Delivery, 0, len(s.items))
	for _, d := range s.items {
		if filter.Status != "" && d.Status != filter.Status {
			continue
		}
		if filter.Provider != "" && d.Provider != filter.Provider {
			continue
		}
		if filter.Level != "" && d.Level != filter.Level {
			continue
		}
		if !filter.Since.IsZero() && d.CreatedAt.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && d.CreatedAt.After(filter.Until) {
			continue
		}
		if filter.AudienceID != "" && d.AudienceID != filter.AudienceID {
			continue
		}
		if filter.Category != "" && d.Category != filter.Category {
			continue
		}
		if filter.Source != "" && d.Source != filter.Source {
			continue
		}
		result = append(result, d)
	}
	return result
}

func (s *Store) allItems() []*Delivery {
	result := make([]*Delivery, 0, len(s.items))
	for _, d := range s.items {
		result = append(result, d)
	}
	return result
}

// Clear removes all deliveries.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = make(map[string]*Delivery)
	s.order = nil
}

// Stats returns statistics about the delivery state machine.
func (s *Store) Stats() *Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &Stats{
		Total:      len(s.items),
		ByStatus:   make(map[string]int),
		ByProvider: make(map[string]int),
		ByLevel:    make(map[string]int),
	}

	for _, d := range s.items {
		stats.ByStatus[d.Status]++
		stats.ByProvider[d.Provider]++
		if d.Level != "" {
			stats.ByLevel[d.Level]++
		}
	}

	return stats
}

// NewDelivery snapshots a task's state-machine fields into a store
// record. The runtime calls it on every transition so the store stays
// the authoritative lifecycle view.
func NewDelivery(task *core.DeliveryTask) *Delivery {
	status := string(task.Status)
	if status == "" {
		status = string(core.StatusQueued)
	}
	return &Delivery{
		ID:           task.ID,
		Provider:     task.Provider,
		Level:        task.Level,
		Status:       status,
		RetryCount:   task.RetryCount,
		MaxAttempts:  task.MaxAttempts,
		LastError:    task.LastError,
		CreatedAt:    task.CreatedAt,
		NextRetryAt:  task.NextRetryAt,
		AudienceID:   task.AudienceID,
		Category:     task.Category,
		Source:       task.Source,
		RelationType: task.RelationType,
		EventID:      task.EventID,
		AlertID:      task.AlertID,
	}
}

// Filter is a delivery filter.
type Filter struct {
	Status     string
	Provider   string
	Level      string
	Since      time.Time
	Until      time.Time
	AudienceID string
	Category   string
	Source     string
}

// Stats is delivery statistics.
type Stats struct {
	Total      int            `json:"total"`
	ByStatus   map[string]int `json:"by_status"`
	ByProvider map[string]int `json:"by_provider"`
	ByLevel    map[string]int `json:"by_level"`
}
