package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/rules"
	"github.com/cuihairu/herald/core/template"
)

// stubEvaluator returns a fixed decision (and optional error) for every
// evaluation, letting tests pin the rule engine's outcome precisely.
type stubEvaluator struct {
	decision *rules.Decision
	err      error
}

func (s *stubEvaluator) Evaluate(ctx context.Context, env rules.Env) (*rules.Decision, error) {
	return s.decision, s.err
}

// recordingObserver captures what the service reported to the observer.
type recordingObserver struct {
	shadows    []rules.ShadowHit
	evalErrors []string
}

func (r *recordingObserver) RecordShadow(ruleID string, channels []string, n *core.Notification) {
	r.shadows = append(r.shadows, rules.ShadowHit{RuleID: ruleID, Channels: channels})
}

func (r *recordingObserver) RecordEvalError(ruleID string, err error, n *core.Notification) {
	r.evalErrors = append(r.evalErrors, ruleID+": "+err.Error())
}

func newRuleTestService(t *testing.T) (*NotificationService, *mockQueue, *route.Router, *mockProviderRuntime) {
	t.Helper()
	templates := template.NewManager()
	router := route.NewRouter(&route.Config{})
	runtime := newMockProviderRuntime()
	queue := newMockQueue()

	for _, name := range []string{"static-provider", "rule-provider"} {
		runtime.RegisterProvider(name, &mockProvider{
			providerType: "test",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}, true)
	}
	router.SetRoute("alert", []string{"static-provider"})

	svc := NewNotificationService(templates, router, runtime, nil, queue)
	return svc, queue, router, runtime
}

func alertNotification() *core.Notification {
	return &core.Notification{
		Type:  "alert",
		Level: "error",
		Content: &core.DirectContent{
			Title: "Test Alert",
			Body:  "body",
		},
	}
}

func TestProcessWithRules(t *testing.T) {
	ctx := context.Background()

	t.Run("active hit routes to rule channels", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:   "r1",
			Mode:     rules.ModeActive,
			Channels: []string{"rule-provider"},
		}})

		res, err := svc.Process(ctx, alertNotification())
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.Accepted) != 1 || res.Accepted[0] != "rule-provider" {
			t.Fatalf("expected rule channel, got %v", res.Accepted)
		}
		if queue.tasks[0].Provider != "rule-provider" {
			t.Fatalf("expected task for rule-provider, got %s", queue.tasks[0].Provider)
		}
	})

	t.Run("explicit channels win over active hit", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:   "r1",
			Mode:     rules.ModeActive,
			Channels: []string{"rule-provider"},
		}})

		n := alertNotification()
		n.Channels = []string{"static-provider"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("explicit channels must win, got %s", queue.tasks[0].Provider)
		}
	})

	t.Run("shadow hit keeps static routing and is observed", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID: "r1",
			Mode:   rules.ModeShadow,
			Shadow: []rules.ShadowHit{{RuleID: "r1", Channels: []string{"rule-provider"}}},
		}})
		svc.SetRuleObserver(observer)

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("shadow must not change routing, got %s", queue.tasks[0].Provider)
		}
		if len(observer.shadows) != 1 || observer.shadows[0].RuleID != "r1" {
			t.Fatalf("expected shadow observation, got %+v", observer.shadows)
		}
	})

	t.Run("active hit without explicit channels falls back to static routing when steps missed", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID: "r1",
			Mode:   rules.ModeActive,
			// no channels: all route steps missed
		}})

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("expected static routing fallback, got %s", queue.tasks[0].Provider)
		}
	})

	t.Run("evaluation errors are observed without breaking delivery", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			EvalErrors: []rules.EvalError{{RuleID: "broken", Err: context.DeadlineExceeded}},
		}})
		svc.SetRuleObserver(observer)

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("expected static routing, got %s", queue.tasks[0].Provider)
		}
		if len(observer.evalErrors) != 1 || !strings.HasPrefix(observer.evalErrors[0], "broken: ") {
			t.Fatalf("expected eval error observation, got %v", observer.evalErrors)
		}
	})

	t.Run("total evaluation failure is observed", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleEngine(&stubEvaluator{err: context.DeadlineExceeded})
		svc.SetRuleObserver(observer)

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(observer.evalErrors) != 1 || !strings.HasPrefix(observer.evalErrors[0], ": ") {
			t.Fatalf("expected empty-rule-id eval error, got %v", observer.evalErrors)
		}
	})

	t.Run("nil decision and nil error changes nothing", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleEngine(&stubEvaluator{})
		svc.SetRuleObserver(observer)

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("expected static routing, got %s", queue.tasks[0].Provider)
		}
		if len(observer.shadows) != 0 || len(observer.evalErrors) != 0 {
			t.Fatalf("expected no observations, got %+v", observer)
		}
	})

	t.Run("observer alone without engine is inert", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleObserver(observer)

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(observer.shadows) != 0 || len(observer.evalErrors) != 0 {
			t.Fatalf("expected no observations without engine, got %+v", observer)
		}
		if queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("expected static routing, got %s", queue.tasks[0].Provider)
		}
	})
}

// TestProcessWithRealRuleEngine wires the actual rules.Engine into the
// service and verifies the end-to-end routing override.
func TestProcessWithRealRuleEngine(t *testing.T) {
	ctx := context.Background()
	svc, queue, _, _ := newRuleTestService(t)

	engine := rules.NewEngine(rules.NewMemoryStore())
	r := rules.Rule{
		ID:    "critical-to-oncall",
		Match: `level == "error" && params.env == "prod"`,
		Mode:  rules.ModeActive,
		Route: []rules.RouteStep{{Channels: []string{"rule-provider"}}},
	}
	if err := engine.Put(ctx, &r); err != nil {
		t.Fatalf("engine.Put: %v", err)
	}
	svc.SetRuleEngine(engine)

	n := alertNotification()
	n.Params = map[string]any{"env": "prod"}
	if _, err := svc.Process(ctx, n); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if queue.tasks[0].Provider != "rule-provider" {
		t.Fatalf("expected rule-engine routing, got %s", queue.tasks[0].Provider)
	}

	// env=staging misses the rule: static routing applies.
	n2 := alertNotification()
	n2.Params = map[string]any{"env": "staging"}
	if _, err := svc.Process(ctx, n2); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if queue.tasks[1].Provider != "static-provider" {
		t.Fatalf("expected static routing for non-matching notification, got %s", queue.tasks[1].Provider)
	}
}
