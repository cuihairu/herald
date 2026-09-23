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
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("rules: invalid for duration %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("rules: for duration %q must be positive", s)
	}
	if d > MaxForDuration {
		return 0, fmt.Errorf("rules: for duration %q exceeds max %v", s, MaxForDuration)
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
		// Defensive: Env holds only JSON-marshalable fields.
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

// Observe records one hit of ruleID for the given group and reports whether
// the alert should fire now:
//
//   - first sighting: the window starts, not fired;
//   - window elapsed: fires once, marks the state Fired;
//   - already fired:  further hits stay silent until a Reset;
//   - window running: not fired.
//
// stateErr is returned verbatim so the caller can treat store failures as
// rule evaluation errors (fail-open: the rule is skipped, routing survives).
func (t *ForTracker) Observe(ctx context.Context, ruleID, groupKey string, forDur time.Duration) (fired bool, err error) {
	key := stateKey(ruleID, groupKey)
	now := t.now()
	state, err := t.store.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if state == nil {
		state = &RuleState{FirstSeen: now, LastSeen: now, Count: 1}
	} else {
		state.LastSeen = now
		state.Count++
	}
	if state.Fired {
		// Alert already went out for this group; stay silent until reset.
		return false, t.store.Put(ctx, key, state, forDur+stateTTLBuffer)
	}
	if now.Sub(state.FirstSeen) >= forDur {
		state.Fired = true
		return true, t.store.Put(ctx, key, state, forDur+stateTTLBuffer)
	}
	return false, t.store.Put(ctx, key, state, forDur+stateTTLBuffer)
}

// Reset drops the in-progress window for one rule group. It is called when
// the rule's match stops holding: "for" means CONTINUOUS holding, so a gap
// in events must not accumulate into a false duration.
func (t *ForTracker) Reset(ctx context.Context, ruleID, groupKey string) error {
	return t.store.Delete(ctx, stateKey(ruleID, groupKey))
}

// ResetRule drops every window of a rule; used when the rule is replaced or
// deleted, so stale windows never survive a rule change.
func (t *ForTracker) ResetRule(ctx context.Context, ruleID string) error {
	return t.store.DeleteRule(ctx, ruleID)
}
