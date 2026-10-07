package dedup

import "fmt"

// Tier is the §11.2 频控三档: how freely the same dedup key may redeliver.
// The gate order (幂等 → 折叠/状态 → 频控) puts the tier to work on the
// content stage only — event-idempotency and the state machine run
// regardless of tier.
type Tier string

const (
	// TierOnce: the same key delivers once per process lifetime (§11.2
	// 仅一次 — one-shot events).
	TierOnce Tier = "once"
	// TierThrottle: the same key redelivers only after the fold window
	// expires (§11.2 窗口节流 — "节点告警 30 分钟一条").
	TierThrottle Tier = "throttle"
	// TierAlways: every copy delivers (§11.2 允许重复 — 关键审计类).
	TierAlways Tier = "always"
)

// ParseTier parses a configured tier value. Unknown values are refused,
// never clamped — a mis-graded category must fail loudly at startup, not
// silently deliver differently than documented.
func ParseTier(s string) (Tier, error) {
	switch Tier(s) {
	case TierOnce, TierThrottle, TierAlways:
		return Tier(s), nil
	default:
		return "", fmt.Errorf("dedup: unknown frequency tier %q (want once|throttle|always)", s)
	}
}

// defaultCategoryTiers is the §11.2 品类默认档 table (品类默认建议
// column): system events are one-shot, alerts throttle to one per window,
// and unlisted categories keep the historical behavior — window throttle.
var defaultCategoryTiers = map[string]Tier{
	"system": TierOnce,
	"alerts": TierThrottle,
}

// DefaultTier answers the §11.2 category default for a category.
func DefaultTier(category string) Tier {
	if t, ok := defaultCategoryTiers[category]; ok {
		return t
	}
	return TierThrottle
}

// Narrow applies the §11.2 只许收窄不许放宽 rule: a user-side tier may
// make delivery strictly less frequent than the category default, never
// more. The lattice is once ⊂ throttle ⊂ always (once narrowest); a
// request wider than the base keeps the base. An empty request means the
// user expressed no preference — the base stands.
func Narrow(base, request Tier) Tier {
	if request == "" {
		return base
	}
	if rank(request) < rank(base) {
		return request
	}
	return base
}

// rank orders the lattice from narrowest (0) to widest (2). Unknown
// values rank widest so Narrow ignores them rather than crashing on
// hand-edited preferences.
func rank(t Tier) int {
	switch t {
	case TierOnce:
		return 0
	case TierThrottle:
		return 1
	default:
		return 2
	}
}
