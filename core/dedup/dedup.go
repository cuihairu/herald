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
	// CategoryTiers / CategoryWindows are the §11.2 operator override
	// tables (raw strings as configured; Config.Validate refuses
	// unparseable tiers at startup — the gate consult falls back to the
	// category default only for callers that skipped validation).
	tiers   map[string]string
	windows map[string]time.Duration
}

// Config is the dedup configuration
type Config struct {
	Window time.Duration `yaml:"window"`
	// CategoryTiers re-grades a category's §11.2 frequency tier
	// ("once"|"throttle"|"always"). Invalid values refuse to start
	// (Config.Validate); missing categories keep the default table.
	CategoryTiers map[string]string `yaml:"category_tiers"`
	// CategoryWindows overrides the fold window per category (§11.2
	// 「节点告警 30 分钟一条」). Non-positive values are ignored.
	CategoryWindows map[string]time.Duration `yaml:"category_windows"`
}

// NewDedup creates a new dedup
func NewDedup(config *Config) *Dedup {
	window := 5 * time.Minute
	if config != nil && config.Window > 0 {
		window = config.Window
	}

	var tiers map[string]string
	var windows map[string]time.Duration
	if config != nil {
		tiers = config.CategoryTiers
		windows = config.CategoryWindows
	}
	return &Dedup{
		seen:    make(map[string]time.Time),
		window:  window,
		tiers:   tiers,
		windows: windows,
	}
}

// Gate derives the §11 gate from this dedup's window and override
// tables — the shape the notification pipeline consumes.
func (d *Dedup) Gate() *Gate {
	g := NewGate(d.window, nil)
	g.tierOverrides = d.tiers
	g.winOverrides = d.windows
	return g
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
