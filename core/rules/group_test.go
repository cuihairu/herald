package rules

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseGroupInterval(t *testing.T) {
	if got, err := ParseGroupInterval("5m"); err != nil || got != 5*time.Minute {
		t.Errorf("ParseGroupInterval(\"5m\") = %v, %v; want 5m, nil", got, err)
	}
	// Unlike for, an empty string has no meaning here and is rejected.
	for _, in := range []string{"", "abc", "3", "0s", "-5m", "25h"} {
		if _, err := ParseGroupInterval(in); err == nil {
			t.Errorf("ParseGroupInterval(%q) = nil error, want rejection", in)
		}
	}
}

func TestRuleGroupKeyFromFields(t *testing.T) {
	base := NewEnv("alert", "error", "disk full", "on /dev/sda1", map[string]any{"env": "prod", "service": "api"})

	key, label := RuleGroupKey([]string{"env", "service"}, base)
	if key == "" {
		t.Fatal("expected non-empty group key")
	}
	if label != "env=prod,service=api" {
		t.Errorf("label = %q, want %q", label, "env=prod,service=api")
	}

	// Same field values → same key, even when free content differs: this is
	// the whole point of group_by (body variants of one logical alert fold
	// together).
	other := NewEnv("alert", "error", "disk full", "usage 91% on /dev/sda1", map[string]any{"service": "api", "env": "prod"})
	if k2, l2 := RuleGroupKey([]string{"env", "service"}, other); k2 != key || l2 != label {
		t.Errorf("expected same group despite body change, got %q/%q", k2, l2)
	}

	// A field value change → different group.
	changed := base
	changed.Params = map[string]any{"env": "staging", "service": "api"}
	if k2, _ := RuleGroupKey([]string{"env", "service"}, changed); k2 == key {
		t.Error("expected a different group for different field values")
	}

	// A missing field hashes like an empty one (still a well-defined group).
	missing := NewEnv("alert", "error", "disk full", "on /dev/sda1", map[string]any{"env": "prod"})
	k3, l3 := RuleGroupKey([]string{"env", "service"}, missing)
	if k3 == "" || l3 != "env=prod,service=" {
		t.Errorf("missing field: key=%q label=%q", k3, l3)
	}
}

func TestRuleGroupKeyFallbackWithoutGroupBy(t *testing.T) {
	env := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	key, label := RuleGroupKey(nil, env)
	if key != ForGroupKey(env) {
		t.Errorf("fallback key = %q, want the content hash %q", key, ForGroupKey(env))
	}
	if label != "" {
		t.Errorf("fallback label = %q, want empty", label)
	}
}

func TestStringifyParam(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"prod", "prod"},
		{42, "42"},
		{3.5, "3.5"},
		{true, "true"},
	}
	for _, c := range cases {
		if got := stringifyParam(c.in); got != c.want {
			t.Errorf("stringifyParam(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
	// Composite values go through their printed form — stable within a
	// process, which is all a group key needs.
	if got := stringifyParam(map[string]any{"a": 1}); got == "" {
		t.Error("composite value must not stringify to empty")
	}
	// json.Marshaler params (e.g. custom types unmarshaled from API JSON)
	// hash by their JSON form.
	mr := marshalerParam{v: "x"}
	if got := stringifyParam(mr); got != `{"v":"x"}` {
		t.Errorf("stringifyParam(marshaler) = %q, want JSON form", got)
	}
	if got := stringifyParam(marshalerParam{err: true}); got == "" {
		t.Error("marshal failure must fall back to a non-empty printed form")
	}
}

// marshalerParam exercises stringifyParam's json.Marshaler branch; with err
// set, Marshal fails so the printed-form fallback runs.
type marshalerParam struct {
	v   string
	err bool
}

func (m marshalerParam) MarshalJSON() ([]byte, error) {
	if m.err {
		return nil, errors.New("no")
	}
	return []byte(`{"v":"` + m.v + `"}`), nil
}

// fixedGroupClock returns a group tracker whose clock the test controls.
func fixedGroupClock(start time.Time) (*GroupTracker, *MemoryStateStore, *time.Time) {
	store := NewMemoryStateStore()
	tracker := NewGroupTracker(store)
	now := start
	tracker.now = func() time.Time { return now }
	return tracker, store, &now
}

func TestGroupTrackerFirstEventOpensRound(t *testing.T) {
	tracker, _, _ := fixedGroupClock(time.Unix(1000, 0))
	ctx := context.Background()

	folded, summary, err := tracker.Observe(ctx, "r1", "g1", "env=prod", 5*time.Minute)
	if err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	if folded || summary != nil {
		t.Errorf("first event: folded=%v summary=%+v, want false/nil", folded, summary)
	}
	state, _ := tracker.store.Get(ctx, stateGroupKey("r1", "g1"))
	if state == nil || state.Count != 1 {
		t.Errorf("expected round opened with count 1, got %+v", state)
	}
}

func TestGroupTrackerFoldsWithinInterval(t *testing.T) {
	tracker, _, now := fixedGroupClock(time.Unix(1000, 0))
	ctx := context.Background()

	if _, _, err := tracker.Observe(ctx, "r1", "g1", "env=prod", 5*time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	*now = (*now).Add(time.Minute)
	folded, summary, err := tracker.Observe(ctx, "r1", "g1", "env=prod", 5*time.Minute)
	if err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	if !folded || summary != nil {
		t.Errorf("event within interval: folded=%v summary=%+v, want true/nil", folded, summary)
	}
	state, _ := tracker.store.Get(ctx, stateGroupKey("r1", "g1"))
	if state == nil || state.Count != 2 {
		t.Errorf("expected folded count 2, got %+v", state)
	}
}

func TestGroupTrackerQuietClosesRoundWithSummary(t *testing.T) {
	tracker, _, now := fixedGroupClock(time.Unix(1000, 0))
	ctx := context.Background()

	// Round: one delivered event + two folded.
	first := time.Unix(1000, 0)
	if _, _, err := tracker.Observe(ctx, "r1", "g1", "env=prod", 5*time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	for i := 0; i < 2; i++ {
		*now = (*now).Add(time.Minute)
		if folded, _, err := tracker.Observe(ctx, "r1", "g1", "env=prod", 5*time.Minute); err != nil || !folded {
			t.Fatalf("folded event %d: folded=%v err=%v", i, folded, err)
		}
	}
	// Quiet past the interval; the next event opens a new round and carries
	// the finished round's summary (count includes the delivered first one).
	*now = (*now).Add(10 * time.Minute)
	folded, summary, err := tracker.Observe(ctx, "r1", "g1", "env=prod", 5*time.Minute)
	if err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	if folded {
		t.Error("event after quiet must open a new round, not fold")
	}
	if summary == nil {
		t.Fatal("expected a summary for the finished round")
	}
	if summary.Count != 3 || summary.Group != "env=prod" || summary.RuleID != "r1" {
		t.Errorf("unexpected summary: %+v", summary)
	}
	if !summary.FirstSeen.Equal(first) || !summary.LastSeen.Equal(first.Add(2*time.Minute)) {
		t.Errorf("summary window = %v..%v, want %v..%v", summary.FirstSeen, summary.LastSeen, first, first.Add(2*time.Minute))
	}
	// The new round starts at one event, not four.
	state, _ := tracker.store.Get(ctx, stateGroupKey("r1", "g1"))
	if state == nil || state.Count != 1 {
		t.Errorf("expected fresh round with count 1, got %+v", state)
	}
}

func TestGroupTrackerQuietWithoutFoldedEventsNoSummary(t *testing.T) {
	tracker, _, now := fixedGroupClock(time.Unix(1000, 0))
	ctx := context.Background()

	if _, _, err := tracker.Observe(ctx, "r1", "g1", "env=prod", 5*time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	*now = (*now).Add(10 * time.Minute)
	folded, summary, err := tracker.Observe(ctx, "r1", "g1", "env=prod", 5*time.Minute)
	if err != nil || folded {
		t.Fatalf("expected fresh round, got folded=%v err=%v", folded, err)
	}
	if summary != nil {
		t.Errorf("a round with no folded events must not produce a summary, got %+v", summary)
	}
}

func TestGroupTrackerGroupsAreIndependent(t *testing.T) {
	tracker, _, _ := fixedGroupClock(time.Unix(1000, 0))
	ctx := context.Background()

	if _, _, err := tracker.Observe(ctx, "r1", "gA", "env=prod", 5*time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	// Another group (or rule) opens its own round; gA must not leak in.
	folded, _, err := tracker.Observe(ctx, "r1", "gB", "env=staging", 5*time.Minute)
	if err != nil || folded {
		t.Errorf("group B first event: folded=%v err=%v, want fresh round", folded, err)
	}
	folded, _, err = tracker.Observe(ctx, "r2", "gA", "env=prod", 5*time.Minute)
	if err != nil || folded {
		t.Errorf("rule 2 first event: folded=%v err=%v, want fresh round", folded, err)
	}
}

func TestGroupTrackerResetRuleDropsAllRounds(t *testing.T) {
	tracker, store, _ := fixedGroupClock(time.Unix(1000, 0))
	ctx := context.Background()

	for _, g := range []string{"gA", "gB"} {
		if _, _, err := tracker.Observe(ctx, "r1", g, "l", 5*time.Minute); err != nil {
			t.Fatalf("Observe error = %v", err)
		}
	}
	// A for-window of the same rule shares the DeleteRule sweep.
	if err := store.Put(ctx, stateKey("r1", "gA"), &RuleState{Count: 1}, 0); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	if _, _, err := tracker.Observe(ctx, "r2", "gA", "l", 5*time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	if err := tracker.ResetRule(ctx, "r1"); err != nil {
		t.Fatalf("ResetRule error = %v", err)
	}
	for _, key := range []string{stateGroupKey("r1", "gA"), stateGroupKey("r1", "gB"), stateKey("r1", "gA")} {
		if state, _ := store.Get(ctx, key); state != nil {
			t.Errorf("key %s survived ResetRule", key)
		}
	}
	if state, _ := store.Get(ctx, stateGroupKey("r2", "gA")); state == nil {
		t.Error("ResetRule must not touch other rules")
	}
}
