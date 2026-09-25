package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/incident"
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
	shadows     []rules.ShadowHit
	evalErrors  []string
	forPendings []string
	groupFolded []string
	inhibited   []string
	silenced    []string
	suppressed  []string
}

func (r *recordingObserver) RecordShadow(ruleID string, channels []string, n *core.Notification) {
	r.shadows = append(r.shadows, rules.ShadowHit{RuleID: ruleID, Channels: channels})
}

func (r *recordingObserver) RecordEvalError(ruleID string, err error, n *core.Notification) {
	r.evalErrors = append(r.evalErrors, ruleID+": "+err.Error())
}

func (r *recordingObserver) RecordForPending(ruleID string, n *core.Notification) {
	r.forPendings = append(r.forPendings, ruleID)
}

func (r *recordingObserver) RecordGroupFolded(ruleID string, n *core.Notification) {
	r.groupFolded = append(r.groupFolded, ruleID)
}

func (r *recordingObserver) RecordInhibited(ruleID string, n *core.Notification) {
	r.inhibited = append(r.inhibited, ruleID)
}

func (r *recordingObserver) RecordSilenced(ruleID string, n *core.Notification) {
	r.silenced = append(r.silenced, ruleID)
}

func (r *recordingObserver) RecordSuppressed(ruleID string, n *core.Notification) {
	r.suppressed = append(r.suppressed, ruleID)
}

// stubScheduler captures escalation scheduling without timers.
type stubScheduler struct {
	scheduled []escalation.Pending
	canceled  []string
}

func (s *stubScheduler) Schedule(_ context.Context, p escalation.Pending) error {
	s.scheduled = append(s.scheduled, p)
	return nil
}

func (s *stubScheduler) Cancel(_ context.Context, alertID string) error {
	s.canceled = append(s.canceled, alertID)
	return nil
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

	t.Run("for pending suppresses delivery and is observed", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:     "r-for",
			Mode:       rules.ModeActive,
			ForPending: true,
		}})
		svc.SetRuleObserver(observer)

		res, err := svc.Process(ctx, alertNotification())
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.TaskIDs) != 0 || len(res.Accepted) != 0 {
			t.Fatalf("pending window must suppress delivery, got %+v", res)
		}
		if len(queue.tasks) != 0 {
			t.Fatalf("expected no queued tasks, got %d", len(queue.tasks))
		}
		if len(observer.forPendings) != 1 || observer.forPendings[0] != "r-for" {
			t.Fatalf("expected for-pending observation, got %v", observer.forPendings)
		}
	})

	t.Run("for pending does not suppress explicit channels", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:     "r-for",
			Mode:       rules.ModeActive,
			ForPending: true,
		}})

		n := alertNotification()
		n.Channels = []string{"static-provider"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("explicit channels must go out despite pending, got %v", queue.tasks)
		}
	})

	t.Run("folded suppresses delivery and is observed", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID: "r-group",
			Mode:   rules.ModeActive,
			Folded: true,
		}})
		svc.SetRuleObserver(observer)

		res, err := svc.Process(ctx, alertNotification())
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.TaskIDs) != 0 || len(res.Accepted) != 0 {
			t.Fatalf("folded event must suppress delivery, got %+v", res)
		}
		if len(queue.tasks) != 0 {
			t.Fatalf("expected no queued tasks, got %d", len(queue.tasks))
		}
		if len(observer.groupFolded) != 1 || observer.groupFolded[0] != "r-group" {
			t.Fatalf("expected folded observation, got %v", observer.groupFolded)
		}
	})

	t.Run("folded does not suppress explicit channels", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID: "r-group",
			Mode:   rules.ModeActive,
			Folded: true,
		}})

		n := alertNotification()
		n.Channels = []string{"static-provider"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("explicit channels must go out despite folding, got %v", queue.tasks)
		}
	})

	t.Run("summary rides along with the opening event", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:   "r-group",
			Mode:     rules.ModeActive,
			Channels: []string{"rule-provider"},
			Summary: &rules.GroupSummary{
				RuleID:    "r-group",
				Group:     "env=prod",
				Count:     4,
				FirstSeen: time.Now().Add(-10 * time.Minute),
				LastSeen:  time.Now().Add(-6 * time.Minute),
			},
		}})

		res, err := svc.Process(ctx, alertNotification())
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		// The live event and the synthetic summary both reach rule-provider.
		if len(res.TaskIDs) != 2 || len(res.Accepted) != 2 {
			t.Fatalf("expected event + summary delivery, got %+v", res)
		}
		if len(queue.tasks) != 2 {
			t.Fatalf("expected 2 queued tasks, got %d", len(queue.tasks))
		}
		if queue.tasks[0].Payload.Content == nil || queue.tasks[0].Payload.Content.Title != "Test Alert" {
			t.Fatalf("first task must be the live event, got %+v", queue.tasks[0].Payload.Content)
		}
		summaryTask := queue.tasks[1]
		if summaryTask.Payload.Content == nil || !strings.Contains(summaryTask.Payload.Content.Title, "[Aggregation summary]") {
			t.Fatalf("second task must be the summary, got %+v", summaryTask.Payload.Content)
		}
		if !strings.Contains(summaryTask.Payload.Content.Body, "4") {
			t.Fatalf("summary body must carry the folded count, got %q", summaryTask.Payload.Content.Body)
		}
	})

	t.Run("summary without group label names the content grouping", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:   "r-hash",
			Mode:     rules.ModeActive,
			Channels: []string{"rule-provider"},
			Summary: &rules.GroupSummary{
				RuleID: "r-hash",
				Count:  2,
			},
		}})

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 2 {
			t.Fatalf("expected event + summary, got %d tasks", len(queue.tasks))
		}
		title := queue.tasks[1].Payload.Content.Title
		body := queue.tasks[1].Payload.Content.Body
		if !strings.Contains(title, "(content grouping)") {
			t.Fatalf("content-hashed summary must name the grouping, got %q", title)
		}
		if !strings.Contains(body, "r-hash") {
			t.Fatalf("summary body must name the rule, got %q", body)
		}
	})

	t.Run("inhibited suppresses delivery and is observed", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:    "leaf",
			Mode:      rules.ModeActive,
			Inhibited: true,
		}})
		svc.SetRuleObserver(observer)

		res, err := svc.Process(ctx, alertNotification())
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.TaskIDs) != 0 || len(res.Accepted) != 0 {
			t.Fatalf("inhibited event must suppress delivery, got %+v", res)
		}
		if len(queue.tasks) != 0 {
			t.Fatalf("expected no queued tasks, got %d", len(queue.tasks))
		}
		if len(observer.inhibited) != 1 || observer.inhibited[0] != "leaf" {
			t.Fatalf("expected inhibited observation, got %v", observer.inhibited)
		}
	})

	t.Run("inhibited does not suppress explicit channels", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:    "leaf",
			Mode:      rules.ModeActive,
			Inhibited: true,
		}})

		n := alertNotification()
		n.Channels = []string{"static-provider"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("explicit channels must go out despite inhibition, got %v", queue.tasks)
		}
	})

	t.Run("silenced suppresses delivery and is observed", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:   "r-quiet",
			Mode:     rules.ModeActive,
			Silenced: true,
		}})
		svc.SetRuleObserver(observer)

		res, err := svc.Process(ctx, alertNotification())
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.TaskIDs) != 0 || len(res.Accepted) != 0 {
			t.Fatalf("silenced event must suppress delivery, got %+v", res)
		}
		if len(queue.tasks) != 0 {
			t.Fatalf("expected no queued tasks, got %d", len(queue.tasks))
		}
		if len(observer.silenced) != 1 || observer.silenced[0] != "r-quiet" {
			t.Fatalf("expected silenced observation, got %v", observer.silenced)
		}
	})

	t.Run("silenced does not suppress explicit channels", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:   "r-quiet",
			Mode:     rules.ModeActive,
			Silenced: true,
		}})

		n := alertNotification()
		n.Channels = []string{"static-provider"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) != 1 || queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("explicit channels must go out despite the silence window, got %v", queue.tasks)
		}
	})

	t.Run("escalation arms after rule-routed delivery", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		sched := &stubScheduler{}
		svc.SetEscalationScheduler(sched)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:   "r-up",
			Mode:     rules.ModeActive,
			Channels: []string{"rule-provider"},
			Escalation: &rules.EscalationPlan{
				Timeout: 5 * time.Minute,
				To:      []string{"phone-bridge"},
			},
		}})

		n := alertNotification()
		n.Params = map[string]any{"alert_id": "incident-9"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(queue.tasks) == 0 {
			t.Fatalf("expected delivery before escalation arms")
		}
		if len(sched.scheduled) != 1 {
			t.Fatalf("expected one scheduled upgrade, got %d", len(sched.scheduled))
		}
		p := sched.scheduled[0]
		if p.RuleID != "r-up" || p.AlertID != "incident-9" || p.Timeout != 5*time.Minute {
			t.Fatalf("unexpected pending: %+v", p)
		}
		if len(p.To) != 1 || p.To[0] != "phone-bridge" || p.Title != "Test Alert" {
			t.Fatalf("pending must carry the plan and the alert context: %+v", p)
		}
	})

	t.Run("escalation falls back to the dedup key without alert_id", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		sched := &stubScheduler{}
		svc.SetEscalationScheduler(sched)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:     "r-up",
			Mode:       rules.ModeActive,
			Channels:   []string{"rule-provider"},
			Escalation: &rules.EscalationPlan{Timeout: time.Minute, To: []string{"phone"}},
		}})

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(sched.scheduled) != 1 || sched.scheduled[0].AlertID == "" {
			t.Fatalf("expected a non-empty fallback alert id, got %+v", sched.scheduled)
		}
	})

	t.Run("escalation tolerates notifications without inline content", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		sched := &stubScheduler{}
		svc.SetEscalationScheduler(sched)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:     "r-up",
			Mode:       rules.ModeActive,
			Channels:   []string{"rule-provider"},
			Escalation: &rules.EscalationPlan{Timeout: time.Minute, To: []string{"phone"}},
		}})

		// No Content: the identity falls back to the dedup key over the
		// remaining fields and the pending carries no title.
		n := alertNotification()
		n.Content = nil
		n.Level = "critical"
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(sched.scheduled) != 1 {
			t.Fatalf("expected one scheduled upgrade, got %+v", sched.scheduled)
		}
		if sched.scheduled[0].AlertID == "" || sched.scheduled[0].Title != "" {
			t.Fatalf("unexpected pending identity: %+v", sched.scheduled[0])
		}
	})

	t.Run("escalation does not arm for explicit channels", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		sched := &stubScheduler{}
		svc.SetEscalationScheduler(sched)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:     "r-up",
			Mode:       rules.ModeActive,
			Channels:   []string{"rule-provider"},
			Escalation: &rules.EscalationPlan{Timeout: time.Minute, To: []string{"phone"}},
		}})

		n := alertNotification()
		n.Channels = []string{"static-provider"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(sched.scheduled) != 0 {
			t.Fatalf("explicit-channel calls must not arm rule escalations, got %+v", sched.scheduled)
		}
	})

	t.Run("suppressed deliveries do not arm escalation", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		sched := &stubScheduler{}
		svc.SetEscalationScheduler(sched)
		// ForPending suppresses the delivery; no escalation may arm.
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:     "r-up",
			Mode:       rules.ModeActive,
			ForPending: true,
			Escalation: &rules.EscalationPlan{Timeout: time.Minute, To: []string{"phone"}},
		}})

		if _, err := svc.Process(ctx, alertNotification()); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(sched.scheduled) != 0 {
			t.Fatalf("suppressed event must not arm escalation, got %+v", sched.scheduled)
		}
	})

	t.Run("rule-routed delivery opens an incident", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		ledger := incident.New(0)
		svc.SetIncidentStore(ledger)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:     "r-up",
			Mode:       rules.ModeActive,
			Channels:   []string{"rule-provider"},
			GroupKey:   "group-1",
			Escalation: &rules.EscalationPlan{Timeout: time.Minute, To: []string{"phone"}},
		}})

		n := alertNotification()
		n.Params = map[string]any{"alert_id": "incident-9"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		inc := ledger.List(&incident.Filter{AlertID: "incident-9"})
		if len(inc) != 1 {
			t.Fatalf("expected one incident, got %+v", inc)
		}
		got := inc[0]
		if got.RuleID != "r-up" || got.GroupKey != "group-1" || got.Status() != incident.StatusOpen {
			t.Fatalf("unexpected incident identity: %+v", got)
		}
		if got.Title != "Test Alert" || got.Level != "error" {
			t.Fatalf("incident must carry the alert context: %+v", got)
		}
		if len(got.Channels) != 1 || got.Channels[0] != "rule-provider" {
			t.Fatalf("incident must record the delivery channels: %+v", got.Channels)
		}
	})

	t.Run("explicit-channel deliveries do not open incidents", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		ledger := incident.New(0)
		svc.SetIncidentStore(ledger)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:   "r-up",
			Mode:     rules.ModeActive,
			Channels: []string{"rule-provider"},
			GroupKey: "group-1",
		}})

		n := alertNotification()
		n.Channels = []string{"static-provider"}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if got := ledger.List(nil); len(got) != 0 {
			t.Fatalf("caller-intent deliveries are not rule incidents, got %+v", got)
		}
	})

	t.Run("HandleResolved closes the episode and delivers the summary", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		ledger := incident.New(0)
		svc.SetIncidentStore(ledger)
		ledger.Open(incident.Opening{
			RuleID:   "r-up",
			GroupKey: "group-1",
			AlertID:  "incident-9",
			Title:    "disk full",
			Level:    "critical",
			Channels: []string{"rule-provider"},
		})

		svc.HandleResolved(rules.ResolvedEvent{
			RuleID:   "r-up",
			GroupKey: "group-1",
			State:    &rules.RuleState{FirstSeen: time.Now().Add(-time.Minute), LastSeen: time.Now(), Count: 7, Fired: true},
		})

		inc := ledger.List(&incident.Filter{AlertID: "incident-9"})[0]
		if inc.Status() != incident.StatusResolved || inc.Events != 7 {
			t.Fatalf("expected resolved episode with 7 events, got %+v", inc)
		}
		if len(queue.tasks) != 1 {
			t.Fatalf("expected one recovery summary, got %d", len(queue.tasks))
		}
		task := queue.tasks[0]
		if task.Provider != "rule-provider" {
			t.Fatalf("summary must ride the episode's channels, got %s", task.Provider)
		}
		content := task.Payload.Content
		if content == nil || !strings.Contains(content.Title, "[Resolved]") || !strings.Contains(content.Title, "disk full") {
			t.Fatalf("unexpected summary title: %+v", content)
		}
		if !strings.Contains(content.Body, "incident-9") || !strings.Contains(content.Body, "7 events") {
			t.Fatalf("summary must carry the recovery facts, got %q", content.Body)
		}
	})

	t.Run("HandleResolved without a ledger or episode is a no-op", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		// No incident store attached at all.
		svc.HandleResolved(rules.ResolvedEvent{RuleID: "r", GroupKey: "g", State: &rules.RuleState{Count: 1}})
		// Ledger attached, but the episode is unknown.
		ledger := incident.New(0)
		svc.SetIncidentStore(ledger)
		svc.HandleResolved(rules.ResolvedEvent{RuleID: "r", GroupKey: "g", State: &rules.RuleState{Count: 1}})
		if len(queue.tasks) != 0 {
			t.Fatalf("no summary may go out, got %+v", queue.tasks)
		}
	})

	t.Run("a failing recovery summary lands on the timeline", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		ledger := incident.New(0)
		svc.SetIncidentStore(ledger)
		// The episode's channel has no provider: the summary cannot go out.
		ledger.Open(incident.Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "t", Channels: []string{"ghost-channel"}})

		svc.HandleResolved(rules.ResolvedEvent{
			RuleID:   "r-up",
			GroupKey: "g1",
			State:    &rules.RuleState{FirstSeen: time.Now(), LastSeen: time.Now(), Count: 3, Fired: true},
		})

		inc := ledger.List(&incident.Filter{AlertID: "a1"})[0]
		if inc.Status() != incident.StatusResolved {
			t.Fatalf("the episode still resolves, got %+v", inc)
		}
		if len(inc.Timeline) != 1 || inc.Timeline[0].Kind != "resolve_delivery_failed" {
			t.Fatalf("the failed summary must be visible on the timeline, got %+v", inc.Timeline)
		}
	})

	t.Run("a title-less episode falls back to the alert id in the summary", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		ledger := incident.New(0)
		svc.SetIncidentStore(ledger)
		ledger.Open(incident.Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "bare-9", Channels: []string{"rule-provider"}})

		svc.HandleResolved(rules.ResolvedEvent{
			RuleID:   "r-up",
			GroupKey: "g1",
			State:    &rules.RuleState{FirstSeen: time.Now(), LastSeen: time.Now(), Count: 2, Fired: true},
		})

		if len(queue.tasks) != 1 {
			t.Fatalf("expected one summary, got %+v", queue.tasks)
		}
		content := queue.tasks[0].Payload.Content
		if content == nil || content.Title != "[Resolved] bare-9" {
			t.Fatalf("summary must fall back to the alert id, got %+v", content)
		}
	})

	t.Run("Escalation attempts land on the incident timeline", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		ledger := incident.New(0)
		svc.SetIncidentStore(ledger)
		ledger.Open(incident.Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "a1", Title: "t"})

		// A failing escalation: fired, then failed — both on the timeline.
		svc.Escalate(escalation.Pending{
			RuleID: "r-up", AlertID: "a1", To: []string{"ghost-channel"},
			Timeout: time.Minute,
		})
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			inc := ledger.List(&incident.Filter{AlertID: "a1"})[0]
			if len(inc.Timeline) >= 2 {
				if inc.Timeline[0].Kind != "escalation_fired" || inc.Timeline[1].Kind != "escalation_failed" {
					t.Fatalf("unexpected timeline: %+v", inc.Timeline)
				}
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("escalation never reached the timeline: %+v", ledger.List(&incident.Filter{AlertID: "a1"})[0].Timeline)
	})

	t.Run("DeliverEscalation repeats the alert on the plan channels", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)

		err := svc.DeliverEscalation(ctx, escalation.Pending{
			RuleID:  "r-up",
			AlertID: "incident-9",
			To:      []string{"rule-provider"},
			Timeout: 5 * time.Minute,
			Title:   "disk full",
		})
		if err != nil {
			t.Fatalf("DeliverEscalation: %v", err)
		}
		if len(queue.tasks) != 1 {
			t.Fatalf("expected one queued escalation, got %d", len(queue.tasks))
		}
		content := queue.tasks[0].Payload.Content
		if content == nil || !strings.Contains(content.Title, "[Escalation]") || !strings.Contains(content.Title, "disk full") {
			t.Fatalf("unexpected escalation title: %+v", content)
		}
		if !strings.Contains(content.Body, "r-up") || !strings.Contains(content.Body, "incident-9") {
			t.Fatalf("escalation body must name the rule and alert, got %q", content.Body)
		}
	})

	t.Run("DeliverEscalation fails without channels", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		if err := svc.DeliverEscalation(ctx, escalation.Pending{AlertID: "a1"}); err == nil {
			t.Fatal("expected error for empty channel list")
		}
	})

	t.Run("escalation alert id falls back to scalar params", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		sched := &stubScheduler{}
		svc.SetEscalationScheduler(sched)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID:     "r-up",
			Mode:       rules.ModeActive,
			Channels:   []string{"rule-provider"},
			Escalation: &rules.EscalationPlan{Timeout: time.Minute, To: []string{"phone"}},
		}})

		n := alertNotification()
		n.Params = map[string]any{"alert_id": 42}
		if _, err := svc.Process(ctx, n); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(sched.scheduled) != 1 || sched.scheduled[0].AlertID != "42" {
			t.Fatalf("non-string alert_id must stringify, got %+v", sched.scheduled)
		}
	})

	t.Run("DeliverEscalation fails when every channel errors", func(t *testing.T) {
		svc, _, _, _ := newRuleTestService(t)
		err := svc.DeliverEscalation(ctx, escalation.Pending{
			AlertID: "a1",
			To:      []string{"ghost-channel"},
		})
		if err == nil {
			t.Fatal("expected error when no escalation channel accepts the delivery")
		}
		if !strings.Contains(err.Error(), "all channels failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("Escalate delivers in the background", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		// Background delivery: no error surfaces, the upgrade simply lands
		// on the queue shortly after Escalate returns.
		svc.Escalate(escalation.Pending{
			RuleID:  "r-up",
			AlertID: "a1",
			To:      []string{"rule-provider"},
			Timeout: 5 * time.Minute,
			Title:   "disk full",
		})
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if len(queue.tasks) == 1 {
				content := queue.tasks[0].Payload.Content
				if content == nil || !strings.Contains(content.Title, "[Escalation]") {
					t.Fatalf("unexpected escalation content: %+v", content)
				}
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatal("background escalation never reached the queue")
	})

	t.Run("escalation without a title names the alert id", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		if err := svc.DeliverEscalation(ctx, escalation.Pending{
			RuleID:  "r-up",
			AlertID: "incident-7",
			To:      []string{"rule-provider"},
			Timeout: time.Minute,
		}); err != nil {
			t.Fatalf("DeliverEscalation: %v", err)
		}
		content := queue.tasks[0].Payload.Content
		if content == nil || !strings.Contains(content.Title, "[Escalation]") || !strings.Contains(content.Title, "incident-7") {
			t.Fatalf("titleless escalation must fall back to the alert id, got %+v", content)
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

// TestProcessWithRealRuleEngineGroupBy wires the actual rules.Engine with a
// group_by rule and verifies folding through the full Process pipeline.
func TestProcessWithRealRuleEngineGroupBy(t *testing.T) {
	ctx := context.Background()
	svc, queue, _, _ := newRuleTestService(t)
	observer := &recordingObserver{}

	engine := rules.NewEngine(rules.NewMemoryStore())
	r := rules.Rule{
		ID:      "prod-alerts-grouped",
		Match:   `level == "error"`,
		Mode:    rules.ModeActive,
		Route:   []rules.RouteStep{{Channels: []string{"rule-provider"}}},
		GroupBy: []string{"env"},
	}
	if err := engine.Put(ctx, &r); err != nil {
		t.Fatalf("engine.Put: %v", err)
	}
	svc.SetRuleEngine(engine)
	svc.SetRuleObserver(observer)

	// First event of the env=prod group: delivered.
	n1 := alertNotification()
	n1.Params = map[string]any{"env": "prod"}
	res1, err := svc.Process(ctx, n1)
	if err != nil {
		t.Fatalf("Process first: %v", err)
	}
	if len(res1.TaskIDs) != 1 {
		t.Fatalf("first event must be delivered, got %+v", res1)
	}

	// Second event of the same group folds: not delivered, observed. The
	// body differs so plain content dedup would not catch it — the group
	// is what folds them.
	n2 := alertNotification()
	n2.Content.Body = "usage is now 95%"
	n2.Params = map[string]any{"env": "prod"}
	res2, err := svc.Process(ctx, n2)
	if err != nil {
		t.Fatalf("Process folded: %v", err)
	}
	if len(res2.TaskIDs) != 0 {
		t.Fatalf("folded event must not be delivered, got %+v", res2)
	}
	if len(observer.groupFolded) != 1 || observer.groupFolded[0] != "prod-alerts-grouped" {
		t.Fatalf("expected folded observation, got %v", observer.groupFolded)
	}
	if len(queue.tasks) != 1 {
		t.Fatalf("expected exactly one queued task, got %d", len(queue.tasks))
	}
}

func TestProcessActionDecisions(t *testing.T) {
	ctx := context.Background()

	t.Run("suppress action withholds the notification", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleObserver(observer)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID: "no-canary", Mode: rules.ModeActive, Action: rules.ActionSuppress,
		}})

		res, err := svc.Process(ctx, alertNotification())
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.TaskIDs) != 0 || len(res.Failed) != 0 {
			t.Fatalf("suppressed notification must enqueue nothing, got %+v", res)
		}
		if len(queue.tasks) != 0 {
			t.Fatalf("queue must stay empty, got %d tasks", len(queue.tasks))
		}
		if len(observer.suppressed) != 1 || observer.suppressed[0] != "no-canary" {
			t.Fatalf("expected suppressed observation, got %v", observer.suppressed)
		}
	})

	t.Run("allow action falls back to static routing", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			RuleID: "exempt", Mode: rules.ModeActive, Action: rules.ActionAllow,
		}})

		res, err := svc.Process(ctx, alertNotification()) // type "alert" → static-provider
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.Accepted) != 1 || res.Accepted[0] != "static-provider" {
			t.Fatalf("allow must deliver via static routing, got %v", res.Accepted)
		}
		if queue.tasks[0].Provider != "static-provider" {
			t.Fatalf("expected static route, got %s", queue.tasks[0].Provider)
		}
	})

	t.Run("default deny withholds unmatched traffic", func(t *testing.T) {
		svc, queue, _, _ := newRuleTestService(t)
		observer := &recordingObserver{}
		svc.SetRuleObserver(observer)
		svc.SetRuleEngine(&stubEvaluator{decision: &rules.Decision{
			Action: rules.ActionSuppress, Defaulted: true,
		}})

		res, err := svc.Process(ctx, alertNotification())
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if len(res.TaskIDs) != 0 {
			t.Fatalf("default deny must enqueue nothing, got %+v", res)
		}
		if len(queue.tasks) != 0 {
			t.Fatalf("queue must stay empty, got %d tasks", len(queue.tasks))
		}
		// No rule decided — the observation names the configuration, not a rule.
		if len(observer.suppressed) != 1 || observer.suppressed[0] != "" {
			t.Fatalf("expected empty-rule-id suppressed observation, got %v", observer.suppressed)
		}
	})

	t.Run("explicit channels bypass suppress and default deny", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			decision *rules.Decision
		}{
			{"suppress action", &rules.Decision{
				RuleID: "no-canary", Mode: rules.ModeActive, Action: rules.ActionSuppress}},
			{"default deny", &rules.Decision{
				Action: rules.ActionSuppress, Defaulted: true}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				svc, queue, _, _ := newRuleTestService(t)
				observer := &recordingObserver{}
				svc.SetRuleObserver(observer)
				svc.SetRuleEngine(&stubEvaluator{decision: tc.decision})

				n := alertNotification()
				n.Channels = []string{"static-provider"}
				res, err := svc.Process(ctx, n)
				if err != nil {
					t.Fatalf("Process: %v", err)
				}
				if len(res.Accepted) != 1 || res.Accepted[0] != "static-provider" {
					t.Fatalf("explicit channels are deliberate and go out, got %+v", res)
				}
				if len(queue.tasks) != 1 {
					t.Fatalf("expected the explicit delivery queued, got %d", len(queue.tasks))
				}
				if len(observer.suppressed) != 0 {
					t.Fatalf("no suppression observation for explicit traffic, got %v", observer.suppressed)
				}
			})
		}
	})
}
