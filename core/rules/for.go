package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const (
	// MaxForDuration caps the "for" window accepted at save time; a typo
	// like 9999h must be rejected at configuration time, not discovered in
	// an alert that never fires.
	MaxForDuration = 24 * time.Hour
	// stateTTLBuffer extends every state entry past the "for" window so a
	// fired group's marker survives until the match stops holding; the
	// explicit reset paths (match miss, rule change) remain the primary
	// cleanup and the TTL is the backstop.
	stateTTLBuffer = time.Hour
)

// ParseFor parses the "for" field: an empty string means no duration
// (forDur 0); otherwise a Go duration (e.g. 3m, 90s, 1h30m) within
// MaxForDuration and strictly positive.
func ParseFor(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return parseDurationField("for", s)
}

// parseDurationField parses a rule duration field (for/group_interval) with
// the shared positivity and range constraints.
func parseDurationField(name, s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("rules: invalid %s duration %q: %w", name, s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("rules: %s duration %q must be positive", name, s)
	}
	if d > MaxForDuration {
		return 0, fmt.Errorf("rules: %s duration %q exceeds max %v", name, s, MaxForDuration)
	}
	return d, nil
}

// ForGroupKey derives the alert-group identity of a notification: the
// canonical JSON of the evaluation environment, hashed. Two notifications
// share a group when their type, level, title, body and params all match,
// so "same logical alert" is stable across events regardless of map
// iteration order (encoding/json sorts map keys).
func ForGroupKey(env Env) string {
	data, err := json.Marshal(env)
	if err != nil {
		// Defensive: params arrive from decoded JSON, so they are always
		// marshalable in practice; a hand-built Env carrying an arbitrary
		// Go value still gets a stable group key instead of a panic.
		return "unmarshalable"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// ForTracker makes the event-driven "for" judgement on top of a StateStore:
// every hit updates (or creates) the group's window and reports whether the
// condition has now held long enough. It is safe for concurrent use through
// the underlying store.
type ForTracker struct {
	store StateStore
	now   func() time.Time
}

// NewForTracker builds a tracker over store.
func NewForTracker(store StateStore) *ForTracker {
	return &ForTracker{store: store, now: time.Now}
}

// ForOutcome is the event-driven "for" judgement for one hit.
type ForOutcome int

const (
	// ForPending: the window is still running — the event must not route.
	ForPending ForOutcome = iota
	// ForFire: the window just elapsed — the alert fires now.
	ForFire
	// ForSilent: the alert already fired for this group — further hits
	// stay silent (and, for rules with group_by, fall through to group
	// aggregation so folded events are still counted).
	ForSilent
)

// Observe records one hit of ruleID for the given group and reports the
// window outcome. stateErr is returned verbatim so the caller can treat
// store failures as rule evaluation errors (fail-open: the rule is skipped,
// routing survives).
func (t *ForTracker) Observe(ctx context.Context, ruleID, groupKey string, forDur time.Duration) (ForOutcome, error) {
	key := stateKey(ruleID, groupKey)
	now := t.now()
	state, err := t.store.Get(ctx, key)
	if err != nil {
		return ForPending, err
	}
	if state == nil {
		state = &RuleState{FirstSeen: now, LastSeen: now, Count: 1}
		return ForPending, t.store.Put(ctx, key, state, forDur+stateTTLBuffer)
	}
	state.LastSeen = now
	state.Count++
	if state.Fired {
		// Alert already went out for this group; stay silent until reset.
		return ForSilent, t.store.Put(ctx, key, state, forDur+stateTTLBuffer)
	}
	if now.Sub(state.FirstSeen) >= forDur {
		state.Fired = true
		return ForFire, t.store.Put(ctx, key, state, forDur+stateTTLBuffer)
	}
	return ForPending, t.store.Put(ctx, key, state, forDur+stateTTLBuffer)
}

// Reset drops the in-progress window for one rule group. It is called when
// the rule's match stops holding: "for" means CONTINUOUS holding, so a gap
// in events must not accumulate into a false duration.
func (t *ForTracker) Reset(ctx context.Context, ruleID, groupKey string) error {
	return t.store.Delete(ctx, stateKey(ruleID, groupKey))
}

// Take drops the window for one rule group like Reset, but returns the
// state it removed: the caller can tell a "window in progress" reset from
// a RESOLVED alert (a fired group whose match stopped holding — the alert
// the rule delivered has recovered).
func (t *ForTracker) Take(ctx context.Context, ruleID, groupKey string) (*RuleState, error) {
	key := stateKey(ruleID, groupKey)
	state, err := t.store.Get(ctx, key)
	if err != nil || state == nil {
		return nil, err
	}
	return state, t.store.Delete(ctx, key)
}

// ResetRule drops every window of a rule; used when the rule is replaced or
// deleted, so stale windows never survive a rule change.
func (t *ForTracker) ResetRule(ctx context.Context, ruleID string) error {
	return t.store.DeleteRule(ctx, ruleID)
}
