package apps

import (
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/dedup"
)

// AppPolicies is the namespace's §13.2 policy override set: per-app
// tables the dispatch face resolves delivery strategy from, riding on
// top of the operator's global config. Zero fields fall through to the
// global tables — an app overrides only what it sets.
type AppPolicies struct {
	// ChannelIntensity re-grades channels inside this namespace (§6.2
	// 渠道→强度). A channel graded to L5 here still needs the global
	// allow_phone gate — the safety rule is not per-app escapable.
	ChannelIntensity map[string]audience.Intensity
	// ModeByCategory fixes the §6.3 delivery mode per category.
	ModeByCategory map[string]audience.Mode
	// AckTimeout is the §6.3 escalation per-step wait; 0 falls through
	// to the executor default.
	AckTimeout time.Duration
	// DedupTiers / DedupWindows re-grade §11.2 frequency tiers and fold
	// windows per category inside this namespace.
	DedupTiers   map[string]dedup.Tier
	DedupWindows map[string]time.Duration
}

// UpdatePolicies applies one validated family replacement under the
// registry lock. The caller parses and validates BEFORE the closure —
// a refused write never leaves a half-applied table.
func (r *Registry) UpdatePolicies(app string, fn func(*AppPolicies)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.apps[app]
	if !ok {
		return ErrUnknownApp
	}
	fn(&a.policies)
	return nil
}

// Policies returns a deep copy of the namespace's override set so
// callers can read (and render) without racing later writes.
func (r *Registry) Policies(app string) (AppPolicies, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.apps[app]
	if !ok {
		return AppPolicies{}, false
	}
	return copyPolicies(a.policies), true
}

func copyPolicies(p AppPolicies) AppPolicies {
	return AppPolicies{
		ChannelIntensity: copyMap(p.ChannelIntensity),
		ModeByCategory:   copyMap(p.ModeByCategory),
		AckTimeout:       p.AckTimeout,
		DedupTiers:       copyMap(p.DedupTiers),
		DedupWindows:     copyMap(p.DedupWindows),
	}
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
