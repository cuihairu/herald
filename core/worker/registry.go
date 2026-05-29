package worker

import (
	"fmt"
	"sync"
	"time"
)

// Mode distinguishes local (goroutine) from remote (separate process) workers
type Mode string

const (
	Local  Mode = "local"
	Remote Mode = "remote"
)

// Info holds metadata about a registered worker
type Info struct {
	ID            string    `json:"id"`
	Mode          Mode      `json:"mode"`
	Capabilities  []string  `json:"capabilities"`
	Status        string    `json:"status"` // online, offline
	ConnectedAt   time.Time `json:"connected_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// Registry tracks all registered workers (both local and remote)
type Registry struct {
	mu      sync.RWMutex
	workers map[string]*Info
}

// NewRegistry creates a new worker registry
func NewRegistry() *Registry {
	return &Registry{
		workers: make(map[string]*Info),
	}
}

// Register adds or updates a worker in the registry
func (r *Registry) Register(info *Info) error {
	if info.ID == "" {
		return fmt.Errorf("worker ID is required")
	}
	if info.ConnectedAt.IsZero() {
		info.ConnectedAt = time.Now()
	}
	info.LastHeartbeat = time.Now()
	info.Status = "online"

	r.mu.Lock()
	r.workers[info.ID] = info
	r.mu.Unlock()
	return nil
}

// Deregister removes a worker from the registry
func (r *Registry) Deregister(workerID string) {
	r.mu.Lock()
	delete(r.workers, workerID)
	r.mu.Unlock()
}

// Heartbeat updates the last heartbeat time for a worker
func (r *Registry) Heartbeat(workerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, ok := r.workers[workerID]
	if !ok {
		return fmt.Errorf("worker not found: %s", workerID)
	}
	w.LastHeartbeat = time.Now()
	return nil
}

// Get returns a copy of info for a specific worker
func (r *Registry) Get(workerID string) (Info, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.workers[workerID]
	if !ok {
		return Info{}, fmt.Errorf("worker not found: %s", workerID)
	}
	return cloneInfo(w), nil
}

// List returns copies of all registered workers
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Info, 0, len(r.workers))
	for _, w := range r.workers {
		result = append(result, cloneInfo(w))
	}
	return result
}

// ListByMode returns copies of workers filtered by mode
func (r *Registry) ListByMode(mode Mode) []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Info
	for _, w := range r.workers {
		if w.Mode == mode {
			result = append(result, cloneInfo(w))
		}
	}
	return result
}

// cloneInfo returns a deep copy of Info
func cloneInfo(w *Info) Info {
	caps := make([]string, len(w.Capabilities))
	copy(caps, w.Capabilities)
	return Info{
		ID:            w.ID,
		Mode:          w.Mode,
		Capabilities:  caps,
		Status:        w.Status,
		ConnectedAt:   w.ConnectedAt,
		LastHeartbeat: w.LastHeartbeat,
	}
}

// RemoveStale removes remote workers that haven't sent a heartbeat within the threshold
func (r *Registry) RemoveStale(threshold time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, w := range r.workers {
		if w.Mode == Remote && now.Sub(w.LastHeartbeat) > threshold {
			w.Status = "offline"
			delete(r.workers, id)
		}
	}
}

// Count returns the total number of registered workers
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.workers)
}
