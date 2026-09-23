package rules

import (
	"context"
	"testing"
	"time"
)

func TestEqualFieldsHash(t *testing.T) {
	env := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod", "cluster": "c1", "service": "api"})

	fields := []string{"env", "cluster"}
	hash := EqualFieldsHash(fields, env)
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// Same field values → same hash (field order comes from the rule, so
	// [env,cluster] and [cluster,env] hash the same values differently —
	// that is fine as long as both sides use the same rule's field list).
	if again := EqualFieldsHash([]string{"env", "cluster"}, env); again != hash {
		t.Error("expected stable hash for the same fields and values")
	}

	// A value change must change the hash.
	other := env
	other.Params = map[string]any{"env": "prod", "cluster": "c2", "service": "api"}
	if EqualFieldsHash(fields, other) == hash {
		t.Error("expected a different hash for a different cluster value")
	}

	// Free content must not affect the hash: only the equal fields matter.
	retitle := env
	retitle.Title = "different alert entirely"
	if EqualFieldsHash(fields, retitle) != hash {
		t.Error("title must not affect the equal-fields hash")
	}

	// A missing field hashes like an empty one (a well-defined group).
	missing := env
	missing.Params = map[string]any{"env": "prod"}
	if EqualFieldsHash(fields, missing) == "" {
		t.Error("missing field must still produce a hash")
	}
}

func TestInhibitTrackerRecordAndPresent(t *testing.T) {
	store := NewMemoryStateStore()
	tracker := NewInhibitTracker(store)
	ctx := context.Background()

	if present, err := tracker.Present(ctx, "leaf", "h1"); err != nil || present {
		t.Fatalf("empty presence: present=%v err=%v, want false nil", present, err)
	}

	if err := tracker.Record(ctx, "leaf", "h1", time.Minute); err != nil {
		t.Fatalf("Record error = %v", err)
	}
	if present, err := tracker.Present(ctx, "leaf", "h1"); err != nil || !present {
		t.Fatalf("after Record: present=%v err=%v, want true nil", present, err)
	}
	// A different value combination for the same target is not suppressed.
	if present, _ := tracker.Present(ctx, "leaf", "h2"); present {
		t.Error("h2 must not be suppressed by h1's presence entry")
	}
	// Other targets are independent.
	if present, _ := tracker.Present(ctx, "leaf2", "h1"); present {
		t.Error("leaf2 must not be suppressed by leaf's presence entry")
	}

	// Presence expires: a short TTL lifts the suppression.
	if err := tracker.Record(ctx, "leaf", "h-exp", 5*time.Millisecond); err != nil {
		t.Fatalf("Record error = %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if present, err := tracker.Present(ctx, "leaf", "h-exp"); err != nil || present {
		t.Errorf("expired presence: present=%v err=%v, want false nil", present, err)
	}
}
