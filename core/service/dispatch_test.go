package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/audit"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/digest"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/template"
)

// dispatchService builds the §13.3 face's full context: one email
// provider, the relation×surface pair, and a policy whose taxonomy makes
// "alerts" urgent. Audiences resolve from their contact surfaces, so no
// user resolver is needed.
func dispatchService(t *testing.T, window time.Duration) (*NotificationService, *audience.Registry, *audience.SurfaceRegistry, *mockQueue, *mockProviderRuntime) {
	t.Helper()
	templates := template.NewManager()
	router := route.NewRouter(&route.Config{})
	runtime := newMockProviderRuntime()
	runtime.RegisterProvider("email", &mockProvider{
		providerType: "email",
		capability: core.ProviderCapability{
			PayloadKinds:   []core.PayloadKind{core.PayloadContent},
			ContentFormats: []string{"plain"},
		},
	}, true)
	dedupMgr := dedup.NewDedup(&dedup.Config{Window: window})
	q := newMockQueue()

	svc := NewNotificationService(templates, router, runtime, dedupMgr, q)
	relations := audience.NewRegistry()
	surfaces := audience.NewSurfaceRegistry()
	svc.SetSurfaces(surfaces)
	return svc, relations, surfaces, q, runtime
}

func dispatchPolicy(t *testing.T) *audience.DeliveryPolicy {
	t.Helper()
	p, err := audience.NewDeliveryPolicy(map[string]string{"alerts": "urgent"}, nil, nil, false)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	return p
}

// activate gives one audience an active email surface plus its
// subscription relation, the minimum standing arrangement for a
// dispatch to reach it.
func activate(t *testing.T, surfaces *audience.SurfaceRegistry, relations *audience.Registry, aud, category string) {
	t.Helper()
	if _, err := surfaces.Activate(aud, "email", aud+"@example.com", "test"); err != nil {
		t.Fatalf("activate %s: %v", aud, err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: aud, Category: category, Channel: "email", Type: audience.RelationSubscription, Source: "test",
	}); err != nil {
		t.Fatalf("subscribe %s: %v", aud, err)
	}
}

// TestDispatchMatchPlanAccept is the §13.3 happy path: the three-way
// intersection keeps both audiences' email, the urgent category builds
// an escalation plan (email L1 first stage), and only the first stage
// enqueues.
func TestDispatchMatchPlanAccept(t *testing.T) {
	svc, relations, surfaces, q, _ := dispatchService(t, time.Minute)
	activate(t, surfaces, relations, "alice", "alerts")
	activate(t, surfaces, relations, "bob", "alerts")
	// alice also holds an L4 sms surface: her kept set spans two
	// intensity bands, which is what makes the escalation chain.
	if _, err := surfaces.Activate("alice", "sms", "+8610000000000", "test"); err != nil {
		t.Fatalf("activate alice sms: %v", err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "sms", Type: audience.RelationSubscription, Source: "test",
	}); err != nil {
		t.Fatalf("subscribe alice sms: %v", err)
	}

	out, err := svc.Dispatch(context.Background(), DispatchRequest{
		Spec: DispatchSpec{
			App:       "ferry",
			Category:  "alerts",
			Urgency:   audience.UrgencyUrgent,
			Audiences: []string{"bob", "alice"},
		},
		Policy: dispatchPolicy(t),
		Filter: audience.NewFilter(relations, surfaces),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if out.Suppressed {
		t.Fatalf("dispatch suppressed, want accepted")
	}
	if out.Mode != "escalation" || out.Urgency != "urgent" {
		t.Fatalf("mode/urgency: want escalation/urgent, got %s/%s", out.Mode, out.Urgency)
	}
	if len(out.Dispatched) != 2 {
		t.Fatalf("dispatched: want 2 audiences, got %v", out.Dispatched)
	}
	if len(out.Refused) != 0 {
		t.Fatalf("refused: want none, got %v", out.Refused)
	}
	// Stage one is the lowest-intensity kept channel — email for both.
	for _, d := range out.Dispatched {
		if len(d.Channels) != 1 || d.Channels[0] != "email" {
			t.Fatalf("stage one for %s: want [email], got %v", d.Audience, d.Channels)
		}
	}
	// The plan summary carries the whole escalation chain.
	if len(out.Plan) != 2 || out.Plan[0].Channels[0] != "email" || out.Plan[1].Channels[0] != "sms" {
		t.Fatalf("plan: want [[email] [sms]], got %v", out.Plan)
	}
	if len(q.tasks) != 2 {
		t.Fatalf("tasks: want one per audience stage one, got %d", len(q.tasks))
	}
	if len(out.TaskIDs) != 2 || len(out.Accepted) != 2 {
		t.Fatalf("bookkeeping: want 2/2, got %v / %v", out.TaskIDs, out.Accepted)
	}
	// Surface binding pins the delivery target: the task goes to the
	// active surface's target, not a config-side recipient list.
	for _, task := range q.tasks {
		if len(task.Targets) != 1 || !strings.HasSuffix(task.Targets[0], "@example.com") {
			t.Fatalf("targets: want the surface target, got %v", task.Targets)
		}
	}
}

// TestDispatchRefusalReasons: a surface-holder without a relation is
// fail-closed refused (filtered), an L5 channel under the phone gate is
// phone_disabled, an out-of-window channel is intensity_exceeded, and a
// reference that resolves to nothing answers no_channels.
func TestDispatchRefusalReasons(t *testing.T) {
	svc, relations, surfaces, q, _ := dispatchService(t, time.Minute)

	// dave holds a surface but no standing relation — fail-closed.
	if _, err := surfaces.Activate("dave", "email", "dave@example.com", "test"); err != nil {
		t.Fatalf("activate dave: %v", err)
	}
	// carol subscribes email and holds an L5 phone surface; the normal
	// urgency window refuses the phone, and the phone gate refuses it
	// regardless (allow_phone is false here).
	activate(t, surfaces, relations, "carol", "alerts")
	if _, err := surfaces.Activate("carol", "phone", "+8610000000000", "test"); err != nil {
		t.Fatalf("activate carol phone: %v", err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: "carol", Category: "alerts", Channel: "phone", Type: audience.RelationSubscription, Source: "test",
	}); err != nil {
		t.Fatalf("subscribe carol phone: %v", err)
	}

	policy, err := audience.NewDeliveryPolicy(map[string]string{"alerts": "normal"}, nil, nil, false)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}

	out, err := svc.Dispatch(context.Background(), DispatchRequest{
		Spec: DispatchSpec{
			App:       "ferry",
			Category:  "alerts",
			Audiences: []string{"dave", "carol", "group:ghosts"},
		},
		Policy: policy,
		Filter: audience.NewFilter(relations, surfaces),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	reasons := map[string]map[string]string{}
	for _, ref := range out.Refused {
		if reasons[ref.Audience] == nil {
			reasons[ref.Audience] = map[string]string{}
		}
		reasons[ref.Audience][ref.Channel] = ref.Reason
	}
	if got := reasons["dave"]["email"]; got != "filtered" {
		t.Fatalf("dave: want filtered, got %q", got)
	}
	if got := reasons["carol"]["phone"]; got != "phone_disabled" {
		t.Fatalf("carol phone: want phone_disabled, got %q", got)
	}
	if got := reasons["group:ghosts"]["*"]; got != "no_channels" {
		t.Fatalf("ghosts: want no_channels, got %q", got)
	}
	// carol's email still went out — refusals are per channel, never a
	// whole-audience veto.
	if len(q.tasks) != 1 {
		t.Fatalf("tasks: want carol email only, got %d", len(q.tasks))
	}
	if len(out.Dispatched) != 1 || out.Dispatched[0].Audience != "carol" {
		t.Fatalf("dispatched: want carol only, got %v", out.Dispatched)
	}
}

// TestDispatchDedupSuppression: a repeat dispatch of the same dedup_key
// folds — no tasks, the outcome says suppressed, and the audit trail
// names the fold.
func TestDispatchDedupSuppression(t *testing.T) {
	svc, relations, surfaces, q, _ := dispatchService(t, time.Minute)
	activate(t, surfaces, relations, "alice", "alerts")
	trail := audit.New(0)
	svc.SetFoldAudit(trail)

	req := DispatchRequest{
		Spec: DispatchSpec{
			App:       "ferry",
			Category:  "alerts",
			DedupKey:  "node-17-down",
			State:     "down",
			Audiences: []string{"alice"},
		},
		Policy: dispatchPolicy(t),
		Filter: audience.NewFilter(relations, surfaces),
	}
	first, err := svc.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if first.Suppressed || len(q.tasks) != 1 {
		t.Fatalf("first dispatch: want delivered, got suppressed=%v tasks=%d", first.Suppressed, len(q.tasks))
	}
	second, err := svc.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	if !second.Suppressed || len(q.tasks) != 1 {
		t.Fatalf("second dispatch: want suppressed with no new tasks, got suppressed=%v tasks=%d", second.Suppressed, len(q.tasks))
	}
	// A state flip delivers again — the §11 state machine fires on flips.
	req.Spec.State = "ok"
	third, err := svc.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("flip dispatch: %v", err)
	}
	if third.Suppressed {
		t.Fatalf("flip dispatch: want delivered, got suppressed")
	}
	if len(q.tasks) != 2 {
		t.Fatalf("tasks after flip: want 2, got %d", len(q.tasks))
	}
}

// TestDispatchTemplateNamespace: rendering goes through the namespace's
// own manager; an unknown template there is a refusal even if the global
// table holds a template of the same name.
func TestDispatchTemplateNamespace(t *testing.T) {
	svc, relations, surfaces, q, _ := dispatchService(t, time.Minute)
	activate(t, surfaces, relations, "alice", "alerts")

	// The global table knows "greet" — the namespace must not see it.
	if err := svc.templates.Register(&template.Template{
		ID: "greet", Name: "greet", Title: "GLOBAL {{.x}}",
		Fields: []template.Field{{Label: "x", Value: "{{.x}}"}},
	}); err != nil {
		t.Fatalf("global template: %v", err)
	}
	nsTemplates := template.NewManager()
	if err := nsTemplates.Register(&template.Template{
		ID: "node_down", Name: "节点下线", Title: "节点 {{.node}} 下线", Level: "error",
		Fields: []template.Field{{Label: "node", Value: "{{.node}}"}},
	}); err != nil {
		t.Fatalf("ns template: %v", err)
	}

	req := DispatchRequest{
		Spec: DispatchSpec{
			App:       "ferry",
			Category:  "alerts",
			Template:  "node_down",
			Params:    map[string]any{"node": "node-17"},
			Audiences: []string{"alice"},
		},
		Policy:    dispatchPolicy(t),
		Filter:    audience.NewFilter(relations, surfaces),
		Templates: nsTemplates,
	}
	if _, err := svc.Dispatch(context.Background(), req); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("tasks: want 1, got %d", len(q.tasks))
	}
	if q.tasks[0].Level != "error" {
		t.Fatalf("level: want template's error, got %q", q.tasks[0].Level)
	}

	req.Spec.Template = "greet"
	if _, derr := svc.Dispatch(context.Background(), req); derr == nil {
		t.Fatalf("global template leak: want refusal, got delivery")
	}
	req.Spec.Template = "node_down"
	req.Templates = nil
	if _, err := svc.Dispatch(context.Background(), req); err == nil || !strings.Contains(err.Error(), "no template manager") {
		t.Fatalf("nil manager: want refusal naming the manager, got %v", err)
	}
}

// TestDispatchDigestFold: an audience whose preference folds collects
// into the digest window instead of delivering — the marketing default
// table folds without any explicit preference row.
func TestDispatchDigestFold(t *testing.T) {
	svc, relations, surfaces, q, _ := dispatchService(t, time.Minute)
	activate(t, surfaces, relations, "alice", "marketing")

	daily, _ := digest.ParseDaily("09:00", time.UTC)
	weekly, _ := digest.ParseWeekly("Mon 09:00", time.UTC)
	svc.SetDigest(digest.NewAggregator(daily, weekly), audience.NewPreferenceRegistry())

	out, err := svc.Dispatch(context.Background(), DispatchRequest{
		Spec: DispatchSpec{
			App:       "ferry",
			Category:  "marketing",
			Audiences: []string{"alice"},
		},
		Policy: dispatchPolicy(t),
		Filter: audience.NewFilter(relations, surfaces),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(out.Folded) != 1 || out.Folded[0] != "alice" {
		t.Fatalf("folded: want [alice], got %v", out.Folded)
	}
	if len(q.tasks) != 0 || len(out.Dispatched) != 0 {
		t.Fatalf("folded audience must not deliver: tasks=%d dispatched=%v", len(q.tasks), out.Dispatched)
	}
}

// TestDispatchDirectContentAndInactiveSurfaces: title/body ride
// without a template, and an inactive surface drops out of the
// candidate set instead of being matched and refused.
func TestDispatchDirectContentAndInactiveSurfaces(t *testing.T) {
	svc, relations, surfaces, q, _ := dispatchService(t, time.Minute)
	activate(t, surfaces, relations, "alice", "alerts")
	// A second surface the audience used to hold, now dead on the
	// reconciler's word: it must not reach matching at all.
	if _, err := surfaces.Activate("alice", "sms", "+8610000000000", "test"); err != nil {
		t.Fatalf("activate sms: %v", err)
	}
	if err := relations.Subscribe(audience.Relation{
		AudienceID: "alice", Category: "alerts", Channel: "sms", Type: audience.RelationSubscription, Source: "test",
	}); err != nil {
		t.Fatalf("subscribe sms: %v", err)
	}
	if err := surfaces.Invalidate("alice", "sms"); err != nil {
		t.Fatalf("invalidate sms: %v", err)
	}

	out, err := svc.Dispatch(context.Background(), DispatchRequest{
		Spec: DispatchSpec{
			App:       "ferry",
			Category:  "alerts",
			Title:     "node down",
			Body:      "node-17 stopped reporting",
			State:     "down",
			Audiences: []string{"alice"},
		},
		Policy: dispatchPolicy(t),
		Filter: audience.NewFilter(relations, surfaces),
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(q.tasks) != 1 {
		t.Fatalf("tasks: want email only (sms inactive), got %d", len(q.tasks))
	}
	if len(out.Refused) != 0 {
		t.Fatalf("refused: the inactive surface never reaches matching, got %v", out.Refused)
	}
}

// TestDispatchQueueUnconfigured: the face is the pipeline's front door;
// without a queue it refuses.
func TestDispatchQueueUnconfigured(t *testing.T) {
	templates := template.NewManager()
	router := route.NewRouter(&route.Config{})
	svc := NewNotificationService(templates, router, newMockProviderRuntime(), nil, nil)
	_, err := svc.Dispatch(context.Background(), DispatchRequest{
		Spec:   DispatchSpec{Category: "alerts", Audiences: []string{"alice"}},
		Policy: dispatchPolicy(t),
	})
	if err == nil || !strings.Contains(err.Error(), "queue is not configured") {
		t.Fatalf("want queue refusal, got %v", err)
	}
}

// TestDispatchDeliveryFailure: a disabled provider lands its channel in
// failed, and an all-channels-failed dispatch answers the error with the
// bookkeeping attached.
func TestDispatchDeliveryFailure(t *testing.T) {
	svc, relations, surfaces, _, rt := dispatchService(t, time.Minute)
	activate(t, surfaces, relations, "alice", "alerts")
	rt.RegisterProvider("email", &mockProvider{providerType: "email"}, false)

	out, err := svc.Dispatch(context.Background(), DispatchRequest{
		Spec: DispatchSpec{
			App:       "ferry",
			Category:  "alerts",
			Audiences: []string{"alice"},
		},
		Policy: dispatchPolicy(t),
		Filter: audience.NewFilter(relations, surfaces),
	})
	if err == nil || !strings.Contains(err.Error(), "all channels failed") {
		t.Fatalf("want all-channels-failed, got %v", err)
	}
	if len(out.Failed) != 1 || !strings.Contains(out.Failed[0].Error, "disabled") {
		t.Fatalf("failed bookkeeping: want disabled provider, got %v", out.Failed)
	}
}
