package dedup

import (
	"sync"
	"time"
)

// Dedup deduplicates notifications by stable key
type Dedup struct {
	mu     sync.RWMutex
	seen   map[string]time.Time
	window time.Duration
}

// Config is the dedup configuration
type Config struct {
	Window time.Duration `yaml:"window"`
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

// Check checks if a key should be deduplicated.
// The caller is responsible for generating a stable, content-derived key.
func (d *Dedup) Check(key string) bool {
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

func (d *Dedup) clean() {
	now := time.Now()
	for k, t := range d.seen {
		if now.Sub(t) > d.window {
			delete(d.seen, k)
		}
	}
}
