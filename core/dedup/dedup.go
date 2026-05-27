package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// Dedup deduplicates notifications
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

// Check checks if a notification should be deduplicated
func (d *Dedup) Check(id string, channels []string, params map[string]any) bool {
	key := d.key(id, channels, params)

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

func (d *Dedup) key(id string, channels []string, params map[string]any) string {
	h := sha256.New()
	h.Write([]byte(id))
	for _, c := range channels {
		h.Write([]byte(c))
	}
	if params != nil {
		b, _ := json.Marshal(params)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (d *Dedup) clean() {
	now := time.Now()
	for k, t := range d.seen {
		if now.Sub(t) > d.window {
			delete(d.seen, k)
		}
	}
}
