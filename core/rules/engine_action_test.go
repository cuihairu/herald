package rules

import (
	"context"
	"testing"
)

// putRule is the test shorthand: validate-and-put through the engine, the
// same path the API and config seeding use.
func putRule(t *testing.T, e *Engine, r Rule) {
	t.Helper()
	if err := e.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put(%s): %v", r.ID, err)
	}
}

func TestEngineSuppressAction(t *testing.T) {
	ctx := context.Background()
	e := NewEngine(NewMemoryStore())
	putRule(t, e, Rule{
		ID: "no-canary-noise", Match: `params.env == "canary"`,
		Mode: ModeActive, Action: ActionSuppress,
	})

	d, err := e.Evaluate(ctx, NewEnv("deploy", "error", "", "", map[string]any{"env": "canary"}))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Action != ActionSuppress || d.RuleID != "no-canary-noise" || d.Mode != ModeActive {
		t.Fatalf("expected active suppress decision, got %+v", d)
	}
	if d.Channels != nil {
		t.Errorf("suppress decision must carry no channels, got %v", d.Channels)
	}
	if d.Defaulted {
		t.Error("a rule-governed suppression is not the default policy")
	}

	// Non-matching traffic is untouched (allow default policy → nil).
	d, err = e.Evaluate(ctx, NewEnv("deploy", "error", "", "", map[string]any{"env": "prod"}))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d != nil {
		t.Fatalf("non-matching env must fall through, got %+v", d)
	}
}

func TestEngineAllowAction(t *testing.T) {
	ctx := context.Background()
	e := NewEngine(NewMemoryStore())
	// The exemption sits at a higher priority than the reroute below it;
	// evaluation stops at the allow, so the reroute never applies.
	putRule(t, e, Rule{
		ID: "canary-exempt", Match: `params.env == "canary"`,
		Mode: ModeActive, Action: ActionAllow, Priority: 100,
	})
	putRule(t, e, Rule{
		ID: "everything-else", Match: `level == "error"`,
		Mode: ModeActive, Action: ActionRoute,
		Route: []RouteStep{{Channels: []string{"oncall"}}},
	})

	d, err := e.Evaluate(ctx, NewEnv("deploy", "error", "", "", map[string]any{"env": "canary"}))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Action != ActionAllow || d.RuleID != "canary-exempt" {
		t.Fatalf("expected allow decision from the exempting rule, got %+v", d)
	}
	if d.Channels != nil {
		t.Errorf("allow decision carries no channels, got %v", d.Channels)
	}

	// Without the exemption the reroute governs.
	d, err = e.Evaluate(ctx, NewEnv("deploy", "error", "", "", map[string]any{"env": "prod"}))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Action != ActionRoute || d.RuleID != "everything-else" {
		t.Fatalf("expected route decision below the exemption, got %+v", d)
	}
	if len(d.Channels) != 1 || d.Channels[0] != "oncall" {
		t.Fatalf("expected oncall channels, got %v", d.Channels)
	}
}

func TestEnginePriorityOrder(t *testing.T) {
	ctx := context.Background()
	e := NewEngine(NewMemoryStore())
	// Inserted low-priority first: insertion order alone would let
	// broad-net win; the explicit priority must promote the specific rule.
	putRule(t, e, Rule{
		ID: "broad-net", Match: `level == "error"`,
		Mode: ModeActive, Action: ActionRoute,
		Route: []RouteStep{{Channels: []string{"broad"}}},
	})
	putRule(t, e, Rule{
		ID: "critical-first", Match: `level == "error"`,
		Mode: ModeActive, Action: ActionRoute, Priority: 10,
		Route: []RouteStep{{Channels: []string{"critical"}}},
	})

	d, err := e.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.RuleID != "critical-first" {
		t.Fatalf("higher priority must govern, got %+v", d)
	}

	// List reports evaluation order, not insertion order.
	listed, err := e.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "critical-first" || listed[1].ID != "broad-net" {
		t.Fatalf("List must be evaluation order, got %v", listed)
	}

	// Equal priorities keep insertion order (stable): the appended rule
	// stays below the incumbent even after the table re-sorts.
	putRule(t, e, Rule{
		ID: "tie-append", Match: `level == "error"`,
		Mode: ModeActive, Action: ActionRoute,
		Route: []RouteStep{{Channels: []string{"tie"}}},
	})
	d, err = e.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.RuleID != "critical-first" {
		t.Fatalf("tie must keep the incumbent governing, got %+v", d)
	}

	// Raising a rule's priority via Put moves it in the live table.
	tie := Rule{
		ID: "tie-append", Match: `level == "error"`,
		Mode: ModeActive, Action: ActionRoute, Priority: 20,
		Route: []RouteStep{{Channels: []string{"tie"}}},
	}
	putRule(t, e, tie)
	d, err = e.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.RuleID != "tie-append" {
		t.Fatalf("updated priority must take effect immediately, got %+v", d)
	}
}

func TestEngineDefaultPolicy(t *testing.T) {
	ctx := context.Background()

	t.Run("allow default keeps nil decision", func(t *testing.T) {
		e := NewEngine(NewMemoryStore())
		e.SetDefaultPolicy(PolicyAllow)
		d, err := e.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d != nil {
			t.Fatalf("allow default must keep the nil decision, got %+v", d)
		}
	})

	t.Run("deny withholds unmatched traffic", func(t *testing.T) {
		e := NewEngine(NewMemoryStore())
		e.SetDefaultPolicy(PolicyDeny)
		d, err := e.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || d.Action != ActionSuppress || !d.Defaulted {
			t.Fatalf("expected defaulted suppression, got %+v", d)
		}
		if d.RuleID != "" || d.Mode != "" {
			t.Fatalf("no rule governed — id and mode must be empty, got %q/%q", d.RuleID, d.Mode)
		}
	})

	t.Run("deny keeps shadow observations", func(t *testing.T) {
		e := NewEngine(NewMemoryStore())
		putRule(t, e, Rule{
			ID: "watcher", Match: `level == "error"`, Mode: ModeShadow,
			Route: []RouteStep{{Channels: []string{"oncall"}}},
		})
		e.SetDefaultPolicy(PolicyDeny)
		d, err := e.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || !d.Defaulted || d.Action != ActionSuppress {
			t.Fatalf("expected defaulted suppression, got %+v", d)
		}
		if len(d.Shadow) != 1 || d.Shadow[0].RuleID != "watcher" {
			t.Fatalf("shadow observation must survive the default policy, got %+v", d.Shadow)
		}
	})

	t.Run("deny does not touch governed traffic", func(t *testing.T) {
		e := NewEngine(NewMemoryStore())
		putRule(t, e, Rule{
			ID: "router", Match: `level == "error"`, Mode: ModeActive,
			Route: []RouteStep{{Channels: []string{"oncall"}}},
		})
		e.SetDefaultPolicy(PolicyDeny)
		d, err := e.Evaluate(ctx, NewEnv("deploy", "error", "", "", nil))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || d.RuleID != "router" || d.Action != ActionRoute || d.Defaulted {
			t.Fatalf("an active hit must govern regardless of the default, got %+v", d)
		}
	})
}

func TestEngineShadowRecordsAction(t *testing.T) {
	ctx := context.Background()
	e := NewEngine(NewMemoryStore())
	putRule(t, e, Rule{
		ID: "would-suppress", Match: `level == "info"`, Mode: ModeShadow,
		Action: ActionSuppress,
	})
	putRule(t, e, Rule{
		ID: "would-route", Match: `level == "info"`, Mode: ModeShadow,
		Route: []RouteStep{{Channels: []string{"oncall"}}},
	})

	d, err := e.Evaluate(ctx, NewEnv("deploy", "info", "", "", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || len(d.Shadow) != 2 {
		t.Fatalf("expected two shadow hits, got %+v", d)
	}
	var sawSuppress, sawRoute bool
	for _, hit := range d.Shadow {
		switch hit.RuleID {
		case "would-suppress":
			sawSuppress = hit.Action == ActionSuppress
		case "would-route":
			sawRoute = hit.Action == ActionRoute
		}
	}
	if !sawSuppress || !sawRoute {
		t.Fatalf("shadow hits must carry their would-be action, got %+v", d.Shadow)
	}
}

func TestEnginePutValidateFailure(t *testing.T) {
	// The match compiles but the spec is structurally invalid (escalation
	// without channels): Put must reject through Validate, after the
	// compile step, and the rule must not reach the store.
	e := NewEngine(NewMemoryStore())
	r := Rule{
		ID: "compiles-but-invalid", Match: `level == "error"`, Mode: ModeActive,
		Route:     []RouteStep{{Channels: []string{"oncall"}}},
		Escalation: &EscalationSpec{},
	}
	if err := e.Put(context.Background(), &r); err == nil {
		t.Fatal("expected Validate rejection from Put")
	}
	if _, err := e.Get(context.Background(), r.ID); err == nil {
		t.Fatal("rejected rule must not be stored")
	}
}

func TestEngineNonRouteActionCarriesShadow(t *testing.T) {
	ctx := context.Background()
	e := NewEngine(NewMemoryStore())
	// The shadow watcher sits above the suppressor: its observation must
	// ride along with the suppression decision.
	putRule(t, e, Rule{
		ID: "watcher", Match: `level == "info"`, Mode: ModeShadow,
		Route: []RouteStep{{Channels: []string{"oncall"}}}, Priority: 10,
	})
	putRule(t, e, Rule{
		ID: "blocker", Match: `level == "info"`, Mode: ModeActive,
		Action: ActionSuppress,
	})

	d, err := e.Evaluate(ctx, NewEnv("deploy", "info", "", "", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Action != ActionSuppress || d.RuleID != "blocker" {
		t.Fatalf("expected suppress decision, got %+v", d)
	}
	if len(d.Shadow) != 1 || d.Shadow[0].RuleID != "watcher" {
		t.Fatalf("shadow observation must ride along, got %+v", d.Shadow)
	}
}
