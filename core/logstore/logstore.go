package logstore

import (
	"sort"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
)

// LogStore stores task execution logs
type LogStore struct {
	mu    sync.RWMutex
	logs  []*TaskLog
	limit int
}

// TaskLog is a factual delivery-event record: what was sent, its result,
// how long it took and the error code. Status is the event outcome
// (success, failed, pending, shadow) — not the delivery lifecycle
// status. The accepted/queued/delivering/retrying/delivered/failed/dead
// lifecycle and retry progression live in the delivery store
// (core/deliverystore), which owns the state machine separately.
type TaskLog struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	PayloadKind string `json:"payload_kind"`
	Level       string `json:"level,omitempty"`
	// Status is the factual event outcome, not the delivery lifecycle:
	// "success" and "failed" are attempt results, "pending" means the
	// attempt is still open, "shadow" marks a rule-engine dry-run
	// observation. The dead/failed lifecycle split is never here — it
	// belongs to the delivery store.
	Status      string     `json:"status"` // success, failed, pending, shadow
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Duration    int64      `json:"duration,omitempty"` // milliseconds

	// Shadow-mode rule observation fields. Entries with Status "shadow"
	// never represent delivery: they record that a rule matched a
	// notification during dry-run, sampled per rule.
	RuleID    string    `json:"rule_id,omitempty"`
	WouldFire bool      `json:"would_fire,omitempty"`
	MatchedAt time.Time `json:"matched_at,omitempty"`
	// Channels is only set on shadow entries: the channels the rule
	// would have routed to had it been active.
	Channels []string `json:"channels,omitempty"`

	// Relation type and source for audit trail — snapped from the
	// audience relation that triggered this delivery.
	RelationType string `json:"relation_type,omitempty"`
	Source       string `json:"source,omitempty"`
	// AudienceID and Category are the §13.4 query dimensions, snapped
	// from the notification at plan time.
	AudienceID string `json:"audience_id,omitempty"`
	Category   string `json:"category,omitempty"`
}

// New creates a new log store
func New(limit int) *LogStore {
	if limit <= 0 {
		limit = 1000
	}
	return &LogStore{
		logs:  make([]*TaskLog, 0, limit),
		limit: limit,
	}
}

// Add adds a new log entry
func (s *LogStore) Add(log *TaskLog) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logs = append(s.logs, log)

	if len(s.logs) > s.limit {
		s.logs = s.logs[len(s.logs)-s.limit:]
	}
}

// AddIfAbsent adds the entry only when no log with the same ID exists yet,
// and reports whether it was added. Async re-enqueue calls Deliver once per
// attempt — the first attempt opens the row, later attempts of the same
// task keep it, so one delivery keeps exactly one row (which stays
// "pending" until the final attempt settles it).
func (s *LogStore) AddIfAbsent(log *TaskLog) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.logs {
		if existing.ID == log.ID {
			return false
		}
	}

	s.logs = append(s.logs, log)

	if len(s.logs) > s.limit {
		s.logs = s.logs[len(s.logs)-s.limit:]
	}
	return true
}

// UpdateStatus records the factual event outcome of an open attempt:
// "success" or "failed" plus the error and the completed time. This is
// the event's own result, not the delivery lifecycle status — the
// lifecycle goes to the delivery store, and the two do not share a
// write.
func (s *LogStore) UpdateStatus(id, status, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, log := range s.logs {
		if log.ID == id {
			log.Status = status
			log.Error = errMsg
			log.CompletedAt = &now
			if log.CreatedAt.Before(now) {
				log.Duration = now.Sub(log.CreatedAt).Milliseconds()
			}
			break
		}
	}
}

// Get retrieves logs with optional filtering
func (s *LogStore) Get(offset, limit int, filter *Filter) []*TaskLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filtered := s.filterLogs(filter)

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	total := len(filtered)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*TaskLog{}
	}

	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}

	return filtered[offset:end]
}

// Count returns the total count of filtered logs
func (s *LogStore) Count(filter *Filter) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.filterLogs(filter))
}

func (s *LogStore) filterLogs(filter *Filter) []*TaskLog {
	if filter == nil {
		return s.logs
	}

	result := make([]*TaskLog, 0, len(s.logs))

	for _, log := range s.logs {
		if filter.Status != "" && log.Status != filter.Status {
			continue
		}
		if filter.Provider != "" && log.Provider != filter.Provider {
			continue
		}
		if filter.Level != "" && log.Level != filter.Level {
			continue
		}
		if !filter.Since.IsZero() && log.CreatedAt.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && log.CreatedAt.After(filter.Until) {
			continue
		}
		if filter.AudienceID != "" && log.AudienceID != filter.AudienceID {
			continue
		}
		if filter.Category != "" && log.Category != filter.Category {
			continue
		}
		if filter.Source != "" && log.Source != filter.Source {
			continue
		}
		result = append(result, log)
	}

	return result
}

// GetByID retrieves a log entry by ID
func (s *LogStore) GetByID(id string) *TaskLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, log := range s.logs {
		if log.ID == id {
			return log
		}
	}
	return nil
}

// Clear removes all logs
func (s *LogStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logs = make([]*TaskLog, 0, s.limit)
}

// Stats returns statistics about the logs
func (s *LogStore) Stats() *Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &Stats{
		Total:      len(s.logs),
		ByLevel:    make(map[string]int),
		ByProvider: make(map[string]int),
		ByStatus:   make(map[string]int),
	}

	for _, log := range s.logs {
		if log.Level != "" {
			stats.ByLevel[log.Level]++
		}
		stats.ByProvider[log.Provider]++
		stats.ByStatus[log.Status]++
	}

	return stats
}

// OldestCreatedAt returns the oldest log entry's creation time, if any.
// The overview uses it to report the real coverage of its bounded
// in-memory window instead of claiming a full 24h.
func (s *LogStore) OldestCreatedAt() (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.logs) == 0 {
		return time.Time{}, false
	}
	oldest := s.logs[0].CreatedAt
	for _, log := range s.logs[1:] {
		if log.CreatedAt.Before(oldest) {
			oldest = log.CreatedAt
		}
	}
	return oldest, true
}

// NewTaskLog creates a new task log from a DeliveryTask
func NewTaskLog(task *core.DeliveryTask) *TaskLog {
	return &TaskLog{
		ID:           task.ID,
		Provider:     task.Provider,
		PayloadKind:  string(task.Payload.Kind),
		Level:        task.Level,
		Status:       "pending",
		CreatedAt:    task.CreatedAt,
		RelationType: task.RelationType,
		Source:       task.Source,
		AudienceID:   task.AudienceID,
		Category:     task.Category,
	}
}

// Filter is a log filter
type Filter struct {
	Status   string
	Provider string
	Level    string
	Since    time.Time
	Until    time.Time
	// AudienceID, Category and Source are the §13.4 query dimensions:
	// the app query face scopes deliveries to its namespace by source
	// and narrows by audience/category/status.
	AudienceID string
	Category   string
	Source     string
}

// Stats is log statistics
type Stats struct {
	Total      int            `json:"total"`
	ByLevel    map[string]int `json:"by_level"`
	ByProvider map[string]int `json:"by_provider"`
	ByStatus   map[string]int `json:"by_status"`
}

// TimeseriesPoint is one send-volume bucket: delivery attempts that fall
// in the bucket's time range, split by attempt outcome.
type TimeseriesPoint struct {
	Time    time.Time `json:"time"`
	Total   int       `json:"total"`
	Success int       `json:"success"`
	Failed  int       `json:"failed"`
}

// MaxTimeseriesPoints bounds the point count a single window can produce,
// so a request with a tiny bucket cannot force a multi-GB allocation.
const MaxTimeseriesPoints = 100000

// Timeseries aggregates the send volume over the window into fixed
// buckets of the given width, oldest first. Rule-engine observations
// (shadow, folded, inhibited, silenced, suppressed, pending-window) are
// not deliveries and never enter the series; real attempts count toward
// Total only. Entries outside [now-window, now] are ignored.
func (s *LogStore) Timeseries(window, bucket time.Duration) []TimeseriesPoint {
	return s.timeseriesAt(time.Now(), window, bucket)
}

// timeseriesAt is Timeseries with the clock injected for determinism.
func (s *LogStore) timeseriesAt(now time.Time, window, bucket time.Duration) []TimeseriesPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Ceil the bucket count so every bucket is exactly bucket wide: the
	// floor count would stretch the last bucket by the remainder, making
	// bucket volumes non-comparable. The series is anchored so the last
	// bucket ends at now; it may cover a few extra units before the
	// requested window, never after it.
	count := (int(window) + int(bucket) - 1) / int(bucket)
	if count < 1 {
		count = 1
	}
	if count > MaxTimeseriesPoints {
		count = MaxTimeseriesPoints
	}
	windowStart := now.Add(-window)
	start := now.Add(-time.Duration(count) * bucket)
	points := make([]TimeseriesPoint, count)
	for i := range points {
		points[i] = TimeseriesPoint{Time: start.Add(time.Duration(i) * bucket)}
	}

	for _, log := range s.logs {
		// Rule-engine observations never represent a delivery: shadow is
		// the classic dry-run marker and every other withheld outcome
		// (folded / inhibited / silenced / suppressed / pending-window)
		// carries a RuleID. Real delivery rows have neither.
		if log.Status == "shadow" || log.RuleID != "" {
			continue
		}
		if log.CreatedAt.Before(windowStart) || log.CreatedAt.After(now) {
			continue
		}
		idx := int(log.CreatedAt.Sub(start) / bucket)
		if idx >= count {
			idx = count - 1
		}
		points[idx].Total++
		switch log.Status {
		case "success":
			points[idx].Success++
		case "failed":
			points[idx].Failed++
		}
	}
	return points
}
