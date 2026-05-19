package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
)

// Dedup deduplicates events
type Dedup struct {
	mu     sync.RWMutex
	seen   map[string]time.Time
	window time.Duration
}

// Config is the dedup configuration
type Config struct {
	Window time.Duration `yaml:"window"` // dedup window
}

// NewDedup creates a new dedup
func NewDedup(config *Config) *Dedup {
	window := 5 * time.Minute
	if config != nil && config.Window > 0 {
		window = config.Window
	}

	return &Dedup{
		seen:   make(map[string]time.Time),
		window: window,
	}
}

// Check checks if an event should be deduplicated
func (d *Dedup) Check(event *core.Event) bool {
	key := d.key(event)

	d.mu.Lock()
	defer d.mu.Unlock()

	// Clean old entries
	d.clean()

	if t, ok := d.seen[key]; ok {
		if time.Since(t) < d.window {
			return true // duplicate
		}
	}

	d.seen[key] = time.Now()
	return false
}

// CheckTask checks if a task should be deduplicated
func (d *Dedup) CheckTask(task *core.Task) bool {
	key := d.taskKey(task)

	d.mu.Lock()
	defer d.mu.Unlock()

	d.clean()

	if t, ok := d.seen[key]; ok {
		if time.Since(t) < d.window {
			return true
		}
	}

	d.seen[key] = time.Now()
	return false
}

// key generates a dedup key for an event
func (d *Dedup) key(event *core.Event) string {
	h := sha256.New()
	h.Write([]byte(event.Type))
	for k, v := range event.Labels {
		h.Write([]byte(k))
		h.Write([]byte(v))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// taskKey generates a dedup key for a task
func (d *Dedup) taskKey(task *core.Task) string {
	h := sha256.New()
	h.Write([]byte(task.Provider))
	h.Write([]byte(task.Title))
	h.Write([]byte(task.Body))
	h.Write([]byte(task.Target))
	return hex.EncodeToString(h.Sum(nil))
}

// clean removes old entries
func (d *Dedup) clean() {
	now := time.Now()
	for k, t := range d.seen {
		if now.Sub(t) > d.window {
			delete(d.seen, k)
		}
	}
}
