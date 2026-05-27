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

// TaskLog is a task execution log
type TaskLog struct {
	ID          string     `json:"id"`
	Provider    string     `json:"provider"`
	PayloadKind string     `json:"payload_kind"`
	Level       string     `json:"level,omitempty"`
	Status      string     `json:"status"` // success, failed, pending
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Duration    int64      `json:"duration,omitempty"` // milliseconds
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

// UpdateStatus updates the status of a log entry
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

// NewTaskLog creates a new task log from a DeliveryTask
func NewTaskLog(task *core.DeliveryTask) *TaskLog {
	return &TaskLog{
		ID:          task.ID,
		Provider:    task.Provider,
		PayloadKind: string(task.Payload.Kind),
		Level:       task.Level,
		Status:      "pending",
		CreatedAt:   task.CreatedAt,
	}
}

// Filter is a log filter
type Filter struct {
	Status   string
	Provider string
	Level    string
	Since    time.Time
	Until    time.Time
}

// Stats is log statistics
type Stats struct {
	Total      int            `json:"total"`
	ByLevel    map[string]int `json:"by_level"`
	ByProvider map[string]int `json:"by_provider"`
	ByStatus   map[string]int `json:"by_status"`
}
