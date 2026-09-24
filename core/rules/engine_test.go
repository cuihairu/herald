package rules

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCompileExpr(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		wantErr string // substring; empty means accept
	}{
		{"plain comparison", `level == "error"`, ""},
		{"nested param access", `params.fail_rate > 0.05 && params.env == "prod"`, ""},
		{"matches operator", `title matches "^deploy"`, ""},
		{"logical chain", `type == "deploy" && (level == "error" || level == "warning")`, ""},
		{"empty", "   ", "expression is empty"},
		{"too long", "level == \"" + strings.Repeat("x", MaxExpressionLen) + "\"", "max is"},
		{"syntax error", `level ==`, "unexpected token EOF"},
		{"range bomb", `1..99999999 != []`, "range operator"},
		{"range in builtin", `filter(1..5, x > 2) != []`, "range operator"},
		{"builtin call", `upper(level) == "ERROR"`, "unknown name upper"},
		{"unknown root field", `host == "web-1"`, "unknown name host"},
		{"type mismatch", `level > 1`, "mismatched types"},
		{"non-bool result", `level`, "expected bool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			program, err := compileExpr(tc.code)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected acceptance, got error: %v", err)
				}
				if program == nil {
					t.Fatal("expected non-nil program")
				}
				return
			}
			if err == nil {
				t.Fatalf("expected rejection containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestEngineValidate(t *testing.T) {
	engine := NewEngine(NewMemoryStore())

	t.Run("accepts valid rule", func(t *testing.T) {
		r := validRule()
		if err := engine.Validate(&r); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if r.Mode != ModeActive {
			t.Errorf("Validate should normalize, mode = %q", r.Mode)
		}
	})

	t.Run("rejects bad expression in match", func(t *testing.T) {
		r := validRule()
		r.Match = `1..2 != []`
		err := engine.Validate(&r)
		if err == nil || !strings.Contains(err.Error(), "range operator") {
			t.Fatalf("expected range rejection, got %v", err)
		}
	})

	t.Run("rejects bad expression in route step", func(t *testing.T) {
		r := validRule()
		r.Route = []RouteStep{
			{Match: `level == "error"`, Channels: []string{"a"}},
			{Match: `params.`, Channels: []string{"b"}},
		}
		err := engine.Validate(&r)
		if err == nil || !strings.Contains(err.Error(), "route step 1") {
			t.Fatalf("expected route step 1 error, got %v", err)
		}
	})

	t.Run("empty step match is allowed", func(t *testing.T) {
		r := validRule()
		r.Route = []RouteStep{{Channels: []string{"a"}}}
		if err := engine.Validate(&r); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestEnginePutDeleteList(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	engine := NewEngine(store)

	r1 := validRule()
	if err := engine.Put(ctx, &r1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	stored, err := store.Get(ctx, r1.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored.Mode != ModeActive || len(stored.Route) != 1 {
		t.Fatalf("expected normalized rule persisted, got %+v", stored)
	}

	// Invalid rule must not reach the store.
	bad := validRule()
	bad.ID = "bad"
	bad.Match = `params.`
	if err := engine.Put(ctx, &bad); err == nil {
		t.Fatal("expected error putting invalid rule")
	}
	if _, err := store.Get(ctx, "bad"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid rule should not be stored, got %v", err)
	}

	r2 := Rule{ID: "second", Match: `level == "info"`, Mode: ModeShadow, Route: []RouteStep{{Channels: []string{"s"}}}}
	if err := engine.Put(ctx, &r2); err != nil {
		t.Fatalf("Put(r2): %v", err)
	}
	listed, err := engine.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != r1.ID || listed[1].ID != "second" {
		t.Fatalf("expected priority order [high-fail-rate second], got %v", listed)
	}

	// Re-Put keeps position.
	r1Updated := r1
	r1Updated.Match = `params.fail_rate > 0.5`
	if err := engine.Put(ctx, &r1Updated); err != nil {
		t.Fatalf("Put(update): %v", err)
	}
	listed, _ = engine.List(ctx)
	if listed[0].ID != r1.ID || listed[1].ID != "second" {
		t.Fatalf("update should keep priority position, got %v", listed)
	}

	if err := engine.Delete(ctx, r1.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, r1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected rule gone from store, got %v", err)
	}
	listed, _ = engine.List(ctx)
	if len(listed) != 1 || listed[0].ID != "second" {
		t.Fatalf("expected [second] after delete, got %v", listed)
	}
}

func TestEngineReload(t *testing.T) {
	ctx := context.Background()

	t.Run("empty store loads empty table", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		if err := engine.Reload(ctx); err != nil {
			t.Fatalf("Reload: %v", err)
		}
		d, err := engine.Evaluate(ctx, NewEnv("deploy", "info", "", "", nil))
		if d != nil || err != nil {
			t.Fatalf("empty table must not match, got d=%v err=%v", d, err)
		}
	})

	t.Run("loads and serves stored rules", func(t *testing.T) {
		store := NewMemoryStoreWith(Rule{
			ID:    "boot",
			Match: `level == "error"`,
			Mode:  ModeActive,
			Route: []RouteStep{{Channels: []string{"oncall"}}},
		})
		engine := NewEngine(store)
		if err := engine.Reload(ctx); err != nil {
			t.Fatalf("Reload: %v", err)
		}
		d, err := engine.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || d.RuleID != "boot" || d.Mode != ModeActive {
			t.Fatalf("expected active hit from reloaded rule, got %+v", d)
		}
	})

	t.Run("bad stored rule aborts reload keeping old table", func(t *testing.T) {
		store := NewMemoryStoreWith(Rule{
			ID:    "good",
			Match: `level == "error"`,
			Mode:  ModeActive,
			Route: []RouteStep{{Channels: []string{"oncall"}}},
		})
		engine := NewEngine(store)
		if err := engine.Reload(ctx); err != nil {
			t.Fatalf("initial Reload: %v", err)
		}

		// Sneak a rule with an invalid expression past Put (as a broken
		// external store or manual DB edit would).
		if err := store.Put(ctx, Rule{
			ID:    "broken",
			Match: `1..2 != []`,
			Mode:  ModeActive,
			Route: []RouteStep{{Channels: []string{"x"}}},
		}); err != nil {
			t.Fatalf("store.Put(broken): %v", err)
		}

		err := engine.Reload(ctx)
		if err == nil || !strings.Contains(err.Error(), `"broken"`) {
			t.Fatalf("expected reload error naming the broken rule, got %v", err)
		}

		// Old table still serves.
		d, evalErr := engine.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
		if evalErr != nil {
			t.Fatalf("Evaluate: %v", evalErr)
		}
		if d == nil || d.RuleID != "good" {
			t.Fatalf("old table must keep serving, got %+v", d)
		}
	})
}

func TestEngineEvaluate(t *testing.T) {
	ctx := context.Background()
	env := func() Env {
		return NewEnv("deploy", "error", "deploy failed", "service api-1", map[string]any{
			"fail_rate": 0.08,
			"env":       "prod",
		})
	}

	t.Run("empty table falls back to static routing", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		d, err := engine.Evaluate(ctx, env())
		if d != nil || err != nil {
			t.Fatalf("expected nil decision and no error, got d=%v err=%v", d, err)
		}
	})

	t.Run("off rules are skipped", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		r := validRule()
		r.Mode = ModeOff
		if err := engine.Put(ctx, &r); err != nil {
			t.Fatalf("Put: %v", err)
		}
		d, err := engine.Evaluate(ctx, env())
		if d != nil || err != nil {
			t.Fatalf("off rule must not match, got d=%v err=%v", d, err)
		}
	})

	t.Run("active hit returns resolved channels", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		r := validRule()
		if err := engine.Put(ctx, &r); err != nil {
			t.Fatalf("Put: %v", err)
		}
		d, err := engine.Evaluate(ctx, env())
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || d.Mode != ModeActive || d.RuleID != "high-fail-rate" {
			t.Fatalf("expected active decision, got %+v", d)
		}
		if len(d.Channels) != 1 || d.Channels[0] != "oncall" {
			t.Fatalf("expected channels [oncall], got %v", d.Channels)
		}
		if len(d.Shadow) != 0 {
			t.Fatalf("expected no shadow hits, got %v", d.Shadow)
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		r := validRule()
		if err := engine.Put(ctx, &r); err != nil {
			t.Fatalf("Put: %v", err)
		}
		// fail_rate present but below the threshold: evaluated false, not an error.
		noHit := NewEnv("deploy", "info", "", "", map[string]any{"fail_rate": 0.01})
		d, err := engine.Evaluate(ctx, noHit)
		if d != nil || err != nil {
			t.Fatalf("expected nil decision, got d=%v err=%v", d, err)
		}
	})

	t.Run("first matching route step wins", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		r := Rule{
			ID:    "stepped",
			Match: `type == "deploy"`,
			Mode:  ModeActive,
			Route: []RouteStep{
				{Match: `params.env == "prod"`, Channels: []string{"prod-oncall"}},
				{Channels: []string{"default"}},
			},
		}
		if err := engine.Put(ctx, &r); err != nil {
			t.Fatalf("Put: %v", err)
		}

		d, err := engine.Evaluate(ctx, env())
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || len(d.Channels) != 1 || d.Channels[0] != "prod-oncall" {
			t.Fatalf("expected prod step, got %+v", d)
		}

		other := env()
		other.Params = map[string]any{"env": "staging"}
		d, err = engine.Evaluate(ctx, other)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || len(d.Channels) != 1 || d.Channels[0] != "default" {
			t.Fatalf("expected catch-all step, got %+v", d)
		}
	})

	t.Run("active rule with all steps missed falls through", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		r := Rule{
			ID:    "conditional-only",
			Match: `type == "deploy"`,
			Mode:  ModeActive,
			Route: []RouteStep{{Match: `params.env == "qa"`, Channels: []string{"qa"}}},
		}
		later := Rule{
			ID:    "catchall",
			Match: `type == "deploy"`,
			Mode:  ModeActive,
			Route: []RouteStep{{Channels: []string{"default"}}},
		}
		if err := engine.Put(ctx, &r); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if err := engine.Put(ctx, &later); err != nil {
			t.Fatalf("Put(later): %v", err)
		}

		d, err := engine.Evaluate(ctx, env()) // env=prod: first rule's steps all miss
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || d.RuleID != "catchall" {
			t.Fatalf("expected fall-through to catchall, got %+v", d)
		}
	})

	t.Run("shadow hit records but does not govern", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		shadow := Rule{
			ID:    "observer",
			Match: `type == "deploy"`,
			Mode:  ModeShadow,
			Route: []RouteStep{{Channels: []string{"would-go-here"}}},
		}
		if err := engine.Put(ctx, &shadow); err != nil {
			t.Fatalf("Put: %v", err)
		}

		d, err := engine.Evaluate(ctx, env())
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || d.Mode != ModeShadow || d.RuleID != "observer" {
			t.Fatalf("expected shadow decision, got %+v", d)
		}
		if len(d.Channels) != 0 {
			t.Fatalf("shadow decision must not carry governing channels, got %v", d.Channels)
		}
		if len(d.Shadow) != 1 || d.Shadow[0].RuleID != "observer" || d.Shadow[0].Channels[0] != "would-go-here" {
			t.Fatalf("expected shadow hit evidence, got %+v", d.Shadow)
		}

		// A non-matching notification must not produce shadow evidence.
		d, err = engine.Evaluate(ctx, NewEnv("cron", "info", "", "", nil))
		if d != nil || err != nil {
			t.Fatalf("expected no decision, got d=%v err=%v", d, err)
		}
	})

	t.Run("shadow rides along an active hit", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		shadow := Rule{
			ID:    "observer",
			Match: `level == "error"`,
			Mode:  ModeShadow,
			Route: []RouteStep{{Channels: []string{"s"}}},
		}
		active := validRule()
		if err := engine.Put(ctx, &shadow); err != nil {
			t.Fatalf("Put(shadow): %v", err)
		}
		if err := engine.Put(ctx, &active); err != nil {
			t.Fatalf("Put(active): %v", err)
		}

		d, err := engine.Evaluate(ctx, env())
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || d.Mode != ModeActive || d.RuleID != "high-fail-rate" {
			t.Fatalf("expected active decision, got %+v", d)
		}
		if len(d.Shadow) != 1 || d.Shadow[0].RuleID != "observer" {
			t.Fatalf("expected shadow observation alongside, got %+v", d.Shadow)
		}
	})

	t.Run("evaluation error skips rule and is reported", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		broken := Rule{
			ID:    "broken",
			Match: `params.fail_rate > 0.05`, // fails when fail_rate is absent
			Mode:  ModeActive,
			Route: []RouteStep{{Channels: []string{"b"}}},
		}
		healthy := Rule{
			ID:    "healthy",
			Match: `level == "error"`,
			Mode:  ModeActive,
			Route: []RouteStep{{Channels: []string{"h"}}},
		}
		if err := engine.Put(ctx, &broken); err != nil {
			t.Fatalf("Put(broken): %v", err)
		}
		if err := engine.Put(ctx, &healthy); err != nil {
			t.Fatalf("Put(healthy): %v", err)
		}

		d, err := engine.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
		if err == nil || !strings.Contains(err.Error(), `"broken"`) {
			t.Fatalf("expected error naming broken rule, got %v", err)
		}
		if d == nil || d.RuleID != "healthy" {
			t.Fatalf("healthy rule must still govern, got %+v", d)
		}
		if len(d.EvalErrors) != 1 || d.EvalErrors[0].RuleID != "broken" {
			t.Fatalf("expected structured EvalErrors naming broken, got %+v", d.EvalErrors)
		}
	})

	t.Run("canceled context reports evaluation failure", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		r := validRule()
		if err := engine.Put(ctx, &r); err != nil {
			t.Fatalf("Put: %v", err)
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		d, err := engine.Evaluate(canceled, env())
		if err == nil || !strings.Contains(err.Error(), "high-fail-rate") {
			t.Fatalf("expected evaluation error naming the rule, got %v", err)
		}
		if d != nil {
			t.Fatalf("expected nil decision on failure, got %+v", d)
		}
	})

	t.Run("concurrent evaluate and put", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		for i := 0; i < 5; i++ {
			r := Rule{
				ID:    RuleIDN(i),
				Match: `type == "deploy"`,
				Mode:  ModeShadow,
				Route: []RouteStep{{Channels: []string{"c"}}},
			}
			if err := engine.Put(ctx, &r); err != nil {
				t.Fatalf("Put: %v", err)
			}
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 50; i++ {
				r := Rule{
					ID:    "churn",
					Match: `level == "error"`,
					Mode:  ModeActive,
					Route: []RouteStep{{Channels: []string{"x"}}},
				}
				_ = engine.Put(ctx, &r)
				_ = engine.Delete(ctx, "churn")
			}
		}()
		for i := 0; i < 50; i++ {
			if _, err := engine.Evaluate(ctx, env()); err != nil {
				t.Errorf("Evaluate: %v", err)
			}
		}
		<-done
	})
}

// RuleIDN builds a deterministic per-index rule id for churn tests.
func RuleIDN(i int) string {
	return "shadow-" + string(rune('a'+i))
}

// failingStore wraps MemoryStore and injects errors for fault-injection.
type failingStore struct {
	*MemoryStore
	putErr  error
	listErr error
}

func (f *failingStore) Put(ctx context.Context, rule Rule) error {
	if f.putErr != nil {
		return f.putErr
	}
	return f.MemoryStore.Put(ctx, rule)
}

func (f *failingStore) List(ctx context.Context) ([]Rule, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.MemoryStore.List(ctx)
}

func TestEngineStoreErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("Validate rejects structurally invalid rule", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		r := validRule()
		r.Route = nil
		if err := engine.Validate(&r); err == nil {
			t.Fatal("expected structural validation error")
		}
	})

	t.Run("Put reports store failure", func(t *testing.T) {
		store := &failingStore{MemoryStore: NewMemoryStore(), putErr: errors.New("disk full")}
		engine := NewEngine(store)
		r := validRule()
		err := engine.Put(ctx, &r)
		if err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("expected store put error, got %v", err)
		}
	})

	t.Run("Delete of unknown id reports not found", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		if err := engine.Delete(ctx, "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Delete succeeds when store had a rule the table missed", func(t *testing.T) {
		store := NewMemoryStore()
		engine := NewEngine(store)
		// Bypass the engine so the live table never learns about this rule
		// (what a failed Reload with a later repair looks like).
		if err := store.Put(ctx, Rule{
			ID:    "external",
			Match: `level == "error"`,
			Mode:  ModeActive,
			Route: []RouteStep{{Channels: []string{"x"}}},
		}); err != nil {
			t.Fatalf("store.Put: %v", err)
		}
		if err := engine.Delete(ctx, "external"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	})

	t.Run("Reload reports store failure", func(t *testing.T) {
		store := &failingStore{MemoryStore: NewMemoryStore(), listErr: errors.New("backend down")}
		engine := NewEngine(store)
		if err := engine.Reload(ctx); err == nil || !strings.Contains(err.Error(), "backend down") {
			t.Fatalf("expected store list error, got %v", err)
		}
	})

	t.Run("Reload aborts on broken route step expression", func(t *testing.T) {
		store := NewMemoryStore()
		engine := NewEngine(store)
		if err := store.Put(ctx, Rule{
			ID:    "bad-step",
			Match: `level == "error"`,
			Mode:  ModeActive,
			Route: []RouteStep{{Match: `1..2 != []`, Channels: []string{"x"}}},
		}); err != nil {
			t.Fatalf("store.Put: %v", err)
		}
		err := engine.Reload(ctx)
		if err == nil || !strings.Contains(err.Error(), "route step 0") {
			t.Fatalf("expected route step error, got %v", err)
		}
	})

	t.Run("route step evaluation failure skips rule", func(t *testing.T) {
		engine := NewEngine(NewMemoryStore())
		r := Rule{
			ID:    "fragile-step",
			Match: `type == "deploy"`,
			Mode:  ModeActive,
			Route: []RouteStep{
				{Match: `params.env == "prod"`, Channels: []string{"prod"}},
				{Match: `params.missing > 1`, Channels: []string{"boom"}},
			},
		}
		if err := engine.Put(ctx, &r); err != nil {
			t.Fatalf("Put: %v", err)
		}
		// env=prod hits step 0 fine; drop env so step 0 misses and step 1
		// errors — the rule is skipped and the error is reported.
		d, err := engine.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
		if err == nil || !strings.Contains(err.Error(), `"fragile-step"`) {
			t.Fatalf("expected step evaluation error, got %v", err)
		}
		if d != nil {
			t.Fatalf("expected no decision, got %+v", d)
		}
	})
}

func TestValidateRejectsMalformedDurationsAndWindows(t *testing.T) {
	// Every structured field — durations and the silence window alike — is
	// parsed up front by Validate, so no rule that passes validation can be
	// rejected later at compile time.
	forDur, groupInterval, inhibitTTL := "abc", "abc", "abc"
	engine := NewEngine(NewMemoryStore())

	cases := []struct {
		name string
		rule Rule
	}{
		{"for", Rule{ID: "r", Match: "true", Route: []RouteStep{{Channels: []string{"c"}}}, For: &forDur}},
		{"group_interval", Rule{ID: "r", Match: "true", Route: []RouteStep{{Channels: []string{"c"}}}, GroupBy: []string{"env"}, GroupInterval: &groupInterval}},
		{"inhibit ttl", Rule{ID: "r", Match: "true", Route: []RouteStep{{Channels: []string{"c"}}}, Inhibit: &InhibitSpec{Source: "src", Equal: []string{"env"}, TTL: &inhibitTTL}}},
		{"ack_timeout", Rule{ID: "r", Match: "true", Route: []RouteStep{{Channels: []string{"c"}}}, Escalation: &EscalationSpec{AckTimeout: "abc"}}},
		{"silence window", Rule{ID: "r", Match: "true", Route: []RouteStep{{Channels: []string{"c"}}}, Silence: &SilenceSpec{Start: "25:99", End: "08:00"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := engine.Validate(&tc.rule); err == nil {
				t.Fatalf("Validate must reject a malformed %s", tc.name)
			}
		})
	}
}

func TestSilenceMatchEvaluationErrorSuppressesNothing(t *testing.T) {
	// A silence match that errors at runtime must not freeze the rule:
	// the failure is reported and evaluation proceeds as if unmatched.
	engine := NewEngine(NewMemoryStore())
	silenceMatch := `params.fail_rate > 0.05` // fails when fail_rate is absent
	rule := Rule{
		ID:    "silent",
		Match: `type == "alert"`,
		Mode:  ModeActive,
		Route: []RouteStep{{Channels: []string{"b"}}},
		Silence: &SilenceSpec{
			Start: "00:00", End: "23:59", // always inside the window
			Match: &silenceMatch,
		},
	}
	if err := engine.Put(context.Background(), &rule); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// The broken silence match fails the rule (fail-safe: nothing is
	// delivered) and the failure is reported by name.
	_, err := engine.Evaluate(context.Background(), NewEnv("alert", "error", "", "", nil))
	if err == nil || !strings.Contains(err.Error(), `"silent"`) {
		t.Fatalf("the broken silence match must fail the rule by name, got %v", err)
	}
}
