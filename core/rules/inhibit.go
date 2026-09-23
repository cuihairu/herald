package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// DefaultInhibitTTL is how long a root-cause presence entry keeps
// suppressing after the last hit of the source rule, when the rule sets
// inhibit without an explicit ttl.
const DefaultInhibitTTL = 30 * time.Minute

// inhibitKey builds the durable presence key for one target rule and one
// equal-field value combination: rule:{rule_id}:inhibit:{value_hash}. The
// DeleteRule sweep ("rule:{id}:") covers it, so replacing or deleting
// either rule drops the entries.
func inhibitKey(ruleID, valueHash string) string {
	return "rule:" + ruleID + ":inhibit:" + valueHash
}

// EqualFieldsHash hashes the values of a rule's label fields (equal /
// group_by) for one notification. Field order comes from the rule, values
// from the environment; a missing field hashes like an empty one. The same
// (fields, notification) pair always yields the same hash — that is what
// makes presence lookup an O(1) exact-match instead of a scan.
func EqualFieldsHash(fields []string, env Env) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, stringifyParam(env.Params[f]))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:8])
}

// InhibitTracker keeps the "root cause is present" set in the StateStore.
// Entries are written (and refreshed) whenever the source rule actually
// delivers, and consulted before a target rule delivers. There is no
// background timer: an entry expires after its TTL, refreshed on every
// source hit, so the suppression naturally lifts when the root cause stops
// firing (recovery summaries are a P3 concern).
//
// The tracker reuses RuleState as its storage shape (Count/FirstSeen
// informational; Fired unused).
type InhibitTracker struct {
	store StateStore
	now   func() time.Time
}

// NewInhibitTracker builds a tracker over store.
func NewInhibitTracker(store StateStore) *InhibitTracker {
	return &InhibitTracker{store: store, now: time.Now}
}

// Record marks the equal-field combination of one target rule as
// suppressed, refreshing the TTL. The entry expires exactly after ttl
// (no buffer — unlike for-windows, presence has no state worth keeping
// past its lifetime). Store failures are returned verbatim so the caller
// can decide how to degrade.
func (t *InhibitTracker) Record(ctx context.Context, targetRuleID, valueHash string, ttl time.Duration) error {
	now := t.now()
	return t.store.Put(ctx, inhibitKey(targetRuleID, valueHash), &RuleState{
		FirstSeen: now,
		LastSeen:  now,
		Count:     1,
	}, ttl)
}

// Present reports whether the equal-field combination of one target rule is
// currently suppressed.
func (t *InhibitTracker) Present(ctx context.Context, targetRuleID, valueHash string) (bool, error) {
	state, err := t.store.Get(ctx, inhibitKey(targetRuleID, valueHash))
	if err != nil {
		return false, err
	}
	return state != nil, nil
}
