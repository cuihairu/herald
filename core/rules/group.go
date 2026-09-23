package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DefaultGroupInterval is the quiet period a group must reach before its
// next event opens a new round, when the rule sets group_by but no
// explicit group_interval.
const DefaultGroupInterval = 5 * time.Minute

// ParseGroupInterval parses the "group_interval" field. Unlike for, an
// empty value has no meaning here (the caller applies the default), so an
// empty string is rejected.
func ParseGroupInterval(s string) (time.Duration, error) {
	return parseDurationField("group_interval", s)
}

// stateGroupKey builds the durable key for one rule's group window:
// rule:{rule_id}:group:{group_hash}.
func stateGroupKey(ruleID, groupKey string) string {
	return "rule:" + ruleID + ":group:" + groupKey
}

// stringifyParam renders one group_by field value for hashing: strings
// verbatim, scalars via their printed form, anything else as JSON. A nil
// (missing) value hashes like an empty one.
func stringifyParam(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Marshaler:
		data, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(data)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// RuleGroupKey derives the group key and a human-readable label for a
// notification under the given group_by fields. Without group_by, the key
// falls back to the full-content hash (ForGroupKey) and the label is empty.
func RuleGroupKey(groupBy []string, env Env) (key, label string) {
	if len(groupBy) == 0 {
		return ForGroupKey(env), ""
	}
	parts := make([]string, 0, len(groupBy))
	labelParts := make([]string, 0, len(groupBy))
	for _, f := range groupBy {
		v := stringifyParam(env.Params[f])
		parts = append(parts, v)
		labelParts = append(labelParts, f+"="+v)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:8]), strings.Join(labelParts, ",")
}

// GroupSummary carries the folded events of a finished aggregation round;
// the service delivers it as a synthetic summary notification together
// with the event that opened the new round.
type GroupSummary struct {
	RuleID string
	// Group is the human-readable group label (e.g. env=prod,service=api);
	// empty for content-hashed groups.
	Group string
	// Count is the total number of events in the finished round, including
	// the first one that was delivered individually.
	Count     uint64
	FirstSeen time.Time
	LastSeen  time.Time
}

// GroupTracker runs the event-driven aggregation state machine on top of a
// StateStore. There is no background timer: the round closes when the next
// event arrives after groupInterval of quiet, at which point the folded
// events are reported as a summary alongside that event.
//
// Group rounds reuse RuleState as their storage shape: Count/FirstSeen/
// LastSeen mean the same thing as in for-windows and Fired stays false.
type GroupTracker struct {
	store StateStore
	now   func() time.Time
}

// NewGroupTracker builds a tracker over store.
func NewGroupTracker(store StateStore) *GroupTracker {
	return &GroupTracker{store: store, now: time.Now}
}

// Observe records one hit of ruleID for the given group and reports the
// aggregation outcome:
//
//   - fresh round (first event, or the group was quiet for interval):
//     folded=false; summary is non-nil when the finished round had folded
//     events to report;
//   - round running: folded=true — the event has been counted and must not
//     be delivered.
//
// Store failures are returned verbatim so the caller can treat them as
// rule evaluation errors (fail-open).
func (t *GroupTracker) Observe(ctx context.Context, ruleID, groupKey, groupLabel string, interval time.Duration) (folded bool, summary *GroupSummary, err error) {
	key := stateGroupKey(ruleID, groupKey)
	now := t.now()
	state, err := t.store.Get(ctx, key)
	if err != nil {
		return false, nil, err
	}
	if state == nil || now.Sub(state.LastSeen) >= interval {
		var s *GroupSummary
		if state != nil && state.Count > 1 {
			s = &GroupSummary{
				RuleID:    ruleID,
				Group:     groupLabel,
				Count:     state.Count,
				FirstSeen: state.FirstSeen,
				LastSeen:  state.LastSeen,
			}
		}
		fresh := &RuleState{FirstSeen: now, LastSeen: now, Count: 1}
		return false, s, t.store.Put(ctx, key, fresh, interval+stateTTLBuffer)
	}
	state.Count++
	state.LastSeen = now
	return true, nil, t.store.Put(ctx, key, state, interval+stateTTLBuffer)
}

// ResetRule drops every group round of a rule; used when the rule is
// replaced or deleted, so stale rounds never survive a rule change.
func (t *GroupTracker) ResetRule(ctx context.Context, ruleID string) error {
	return t.store.DeleteRule(ctx, ruleID)
}
