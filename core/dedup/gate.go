package dedup

import (
	"sync"
	"time"
)

// Decision is the gate's verdict for one event (§11 四道闸: 幂等 →
// 折叠/状态 → 频控 → 投递).
type Decision int

const (
	// Pass sends the event into the delivery pipeline.
	Pass Decision = iota
	// Suppress keeps the event out of the pipeline. The fold ledger
	// retains its fingerprint under the key either way — 审计可展开
	// 「这条 ×N 里面是哪 N 条」.
	Suppress
)

// Reasons name why an event was suppressed, for the audit row's Detail.
const (
	// ReasonIdempotent: this event_id already delivered (§11.1 事件幂等).
	ReasonIdempotent = "idempotent"
	// ReasonStateRepeat: a state-type alert re-reported the same state
	// without flipping (§11.1 状态机去重).
	ReasonStateRepeat = "state-repeat"
	// ReasonOnce: a once-tier category saw its key for the second time.
	ReasonOnce = "once"
	// ReasonThrottled: the key delivered within the fold window.
	ReasonThrottled = "throttled"
)

// Event is one candidate at the gate. All identity fields are optional:
// an empty EventID skips the idempotency layer, an empty State skips the
// state machine, and the tier defaults from the category.
type Event struct {
	// EventID is the §11.1 event identity — a repeat delivery of the
	// same id is dropped even if the content drifted.
	EventID string
	// Key is the dedup key (explicit dedup_key or content-derived).
	Key string
	// State is the state-machine dimension (e.g. "down"/"ok"). The gate
	// fires on the first sighting and on every flip.
	State string
	// Category selects the §11.2 default tier.
	Category string
	// Fingerprint identifies the original event inside the fold ledger
	// (typically the notification ID).
	Fingerprint string
}

// Outcome reports the gate verdict and, for suppressions, the fold
// ledger state the audit row should carry.
type Outcome struct {
	Decision Decision
	// Reason names the suppression layer (one of the Reason* constants),
	// empty on Pass.
	Reason string
	// Count is the number of suppressed duplicates folded under this key
	// after this event — the ×N in the audit row. Zero on Pass.
	Count int
}

// Fold is one key's suppression ledger: how many copies were folded and
// which originals they were, oldest first.
type Fold struct {
	Count  int
	Events []string
}

const (
	// gateCap bounds each in-memory table FIFO-style, mirroring the
	// notify idempotency table and the incident ledger: beyond the cap
	// the oldest entries lose their dedup guarantee, and a restart
	// clears every table — "deliver again", never "silently drop".
	gateCap = 4096
	// foldEventCap bounds the retained original list per key. The count
	// keeps accruing past it — the ×N stays truthful, only the expansion
	// truncates.
	foldEventCap = 100
)

// Gate is the §11 dedup layer: event idempotency, the state machine and
// the tiered content stage behind one Decide call. In-memory like the
// rest of the MVP tables.
type Gate struct {
	mu     sync.Mutex
	window time.Duration
	tier   func(category string) Tier

	events  map[string]struct{}  // event_id → seen (幂等)
	eventsQ []string             // FIFO eviction order
	folds   map[string]*Fold     // key → suppression ledger
	foldQ   []string             // FIFO eviction order
	states  map[string]string    // key → last state (状态机)
	stateQ  []string             // FIFO eviction order
	once    map[string]struct{}  // once-tier keys already delivered
	onceQ   []string             // FIFO eviction order
	stamps  map[string]time.Time // throttle-tier last delivery
	stampQ  []string             // FIFO eviction order
}

// NewGate builds a gate with the given fold window and tier resolver.
// A nil resolver falls back to the §11.2 category default table.
func NewGate(window time.Duration, tier func(category string) Tier) *Gate {
	if window <= 0 {
		window = 5 * time.Minute
	}
	if tier == nil {
		tier = DefaultTier
	}
	return &Gate{
		window: window,
		tier:   tier,
		events: make(map[string]struct{}),
		folds:  make(map[string]*Fold),
		states: make(map[string]string),
		once:   make(map[string]struct{}),
		stamps: make(map[string]time.Time),
	}
}

// Window exposes the fold window (the throttle tier redelivers a key
// only after it expires).
func (g *Gate) Window() time.Duration {
	return g.window
}

// Fold returns a copy of the key's suppression ledger, for audit
// expansion and tests.
func (g *Gate) Fold(key string) (Fold, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.folds[key]
	if !ok {
		return Fold{}, false
	}
	return Fold{Count: f.Count, Events: append([]string(nil), f.Events...)}, true
}

// Decide runs one event through the §11 ladder under one lock — the
// gate sits on the delivery path's critical section and the tables are
// small maps.
func (g *Gate) Decide(ev Event) Outcome {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 1. 事件幂等: a repeated event_id is dropped regardless of content
	// drift, tier, or state.
	if ev.EventID != "" {
		if _, seen := g.events[ev.EventID]; seen {
			return Outcome{Decision: Suppress, Reason: ReasonIdempotent,
				Count: g.fold("event:"+ev.EventID, ev.Fingerprint)}
		}
		g.rememberEvent(ev.EventID)
	}

	// 2. 状态机: first sighting and every flip deliver; a repeat of the
	// current state folds into the key's ledger.
	if ev.State != "" {
		if last, ok := g.states[ev.Key]; ok {
			if last == ev.State {
				return Outcome{Decision: Suppress, Reason: ReasonStateRepeat,
					Count: g.fold(ev.Key, ev.Fingerprint)}
			}
			// A flip starts a fresh episode: the new state delivers and
			// the old episode's fold record closes out.
			g.rememberState(ev.Key, ev.State)
			g.unfold(ev.Key)
			return Outcome{Decision: Pass}
		}
		g.rememberState(ev.Key, ev.State)
		return Outcome{Decision: Pass}
	}

	// 3. 频控三档 on the content stage.
	switch g.tier(ev.Category) {
	case TierAlways:
		return Outcome{Decision: Pass}
	case TierOnce:
		if _, seen := g.once[ev.Key]; seen {
			return Outcome{Decision: Suppress, Reason: ReasonOnce,
				Count: g.fold(ev.Key, ev.Fingerprint)}
		}
		g.rememberOnce(ev.Key)
		return Outcome{Decision: Pass}
	default: // TierThrottle
		if t, ok := g.stamps[ev.Key]; ok && time.Since(t) < g.window {
			return Outcome{Decision: Suppress, Reason: ReasonThrottled,
				Count: g.fold(ev.Key, ev.Fingerprint)}
		}
		// Window expired (or first sighting): deliver, re-stamp, and
		// open a fresh fold episode for the key.
		g.stamp(ev.Key)
		g.unfold(ev.Key)
		return Outcome{Decision: Pass}
	}
}

// fold records one suppressed copy under the key and returns the new
// duplicate count.
func (g *Gate) fold(key, fingerprint string) int {
	f := g.folds[key]
	if f == nil {
		f = &Fold{}
		g.folds[key] = f
		g.foldQ = append(g.foldQ, key)
		g.evictLocked()
	}
	f.Count++
	if len(f.Events) < foldEventCap {
		f.Events = append(f.Events, fingerprint)
	}
	return f.Count
}

// unfold closes a key's fold episode — a fresh delivery or state flip
// restarts the ×N count.
func (g *Gate) unfold(key string) {
	if _, ok := g.folds[key]; !ok {
		return
	}
	delete(g.folds, key)
	for i, k := range g.foldQ {
		if k == key {
			g.foldQ = append(g.foldQ[:i], g.foldQ[i+1:]...)
			break
		}
	}
}

// stamp marks the key as just delivered for the throttle tier.
func (g *Gate) stamp(key string) {
	if _, ok := g.stamps[key]; !ok {
		g.stampQ = append(g.stampQ, key)
		g.evictLocked()
	}
	g.stamps[key] = time.Now()
}

// rememberEvent/rememberState/rememberOnce insert into their table with
// FIFO eviction past the cap.
func (g *Gate) rememberEvent(id string) {
	// The only caller (Decide's idempotency stage) has just established
	// the id is fresh — insert without re-checking.
	g.events[id] = struct{}{}
	g.eventsQ = append(g.eventsQ, id)
	g.evictLocked()
}

func (g *Gate) rememberState(key, state string) {
	if _, ok := g.states[key]; !ok {
		g.stateQ = append(g.stateQ, key)
		g.evictLocked()
	}
	g.states[key] = state
}

func (g *Gate) rememberOnce(key string) {
	// Same contract as rememberEvent: Decide only calls here for a key
	// it just established is fresh.
	g.once[key] = struct{}{}
	g.onceQ = append(g.onceQ, key)
	g.evictLocked()
}

// evictLocked drops each table's oldest entries while it outgrows the cap.
func (g *Gate) evictLocked() {
	for len(g.eventsQ) > gateCap {
		oldest := g.eventsQ[0]
		g.eventsQ = g.eventsQ[1:]
		delete(g.events, oldest)
	}
	for len(g.foldQ) > gateCap {
		oldest := g.foldQ[0]
		g.foldQ = g.foldQ[1:]
		delete(g.folds, oldest)
	}
	for len(g.stateQ) > gateCap {
		oldest := g.stateQ[0]
		g.stateQ = g.stateQ[1:]
		delete(g.states, oldest)
	}
	for len(g.onceQ) > gateCap {
		oldest := g.onceQ[0]
		g.onceQ = g.onceQ[1:]
		delete(g.once, oldest)
	}
	for len(g.stampQ) > gateCap {
		oldest := g.stampQ[0]
		g.stampQ = g.stampQ[1:]
		delete(g.stamps, oldest)
	}
}
