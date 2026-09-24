package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/incident"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/rules"
	"github.com/cuihairu/herald/core/template"
	"github.com/google/uuid"
)

// ProcessResult holds the outcome of notification processing
type ProcessResult struct {
	NotificationID string
	TaskIDs        []string
	Accepted       []string // channels that succeeded
	Failed         []ChannelError
}

// ChannelError records a per-channel failure
type ChannelError struct {
	Channel string
	Error   string
}

// ProviderRuntime defines the runtime capabilities needed by NotificationService
type ProviderRuntime interface {
	GetProvider(name string) (core.Provider, error)
	IsEnabled(name string) bool
}

// RuleEvaluator is the subset of the rules engine the service needs.
type RuleEvaluator interface {
	Evaluate(ctx context.Context, env rules.Env) (*rules.Decision, error)
}

// RuleObserver receives rule evaluation observations: shadow-mode hits
// (dry-run evidence), per-rule evaluation failures, active-rule events
// suppressed by a still-running "for" window, events folded into an open
// group-aggregation round, events withheld by root-cause inhibition, and
// events withheld by a rule's daily silence window.
// Implementations must be safe for concurrent use.
type RuleObserver interface {
	RecordShadow(ruleID string, channels []string, n *core.Notification)
	RecordEvalError(ruleID string, err error, n *core.Notification)
	RecordForPending(ruleID string, n *core.Notification)
	RecordGroupFolded(ruleID string, n *core.Notification)
	RecordInhibited(ruleID string, n *core.Notification)
	RecordSilenced(ruleID string, n *core.Notification)
}

// EscalationScheduler arms and cancels ack-gated upgrade deliveries for
// routed rule events (nil by default: no escalation).
type EscalationScheduler interface {
	Schedule(ctx context.Context, p escalation.Pending) error
	Cancel(ctx context.Context, alertID string) error
}

// NotificationService orchestrates the notification processing pipeline
type NotificationService struct {
	templates  *template.Manager
	router     *route.Router
	runtime    ProviderRuntime
	dedup      *dedup.Dedup
	queue      core.Queue
	planner    *DeliveryPlanner
	rules      RuleEvaluator
	observer   RuleObserver
	escalation EscalationScheduler
	incidents  *incident.Store
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(
	templates *template.Manager,
	router *route.Router,
	runtime ProviderRuntime,
	dedup *dedup.Dedup,
	queue core.Queue,
) *NotificationService {
	return &NotificationService{
		templates: templates,
		router:    router,
		runtime:   runtime,
		dedup:     dedup,
		queue:     queue,
		planner:   NewDeliveryPlanner(templates),
	}
}

// SetRuleEngine attaches the rule engine evaluated after dedup and before
// routing. nil (the default) keeps processing unchanged.
func (s *NotificationService) SetRuleEngine(re RuleEvaluator) {
	s.rules = re
}

// SetRuleObserver attaches the observer receiving shadow hits and rule
// evaluation failures. nil (the default) discards observations.
func (s *NotificationService) SetRuleObserver(ro RuleObserver) {
	s.observer = ro
}

// SetEscalationScheduler attaches the ack-gated upgrade scheduler. nil
// (the default) keeps escalation declarations inert.
func (s *NotificationService) SetEscalationScheduler(es EscalationScheduler) {
	s.escalation = es
}

// SetIncidentStore attaches the incident ledger. nil (the default) keeps
// processing unchanged; with a store, rule-routed deliveries open
// incidents and acks, escalations and recoveries mark them.
func (s *NotificationService) SetIncidentStore(store *incident.Store) {
	s.incidents = store
}

// Process processes a Notification, generates DeliveryTasks, and enqueues them.
func (s *NotificationService) Process(ctx context.Context, n *core.Notification) (*ProcessResult, error) {
	if s.queue == nil {
		return nil, fmt.Errorf("queue is not configured")
	}
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	n.CreatedAt = time.Now()

	// Rule evaluation — before routing and before dedup: stateful rule
	// semantics (for windows, group rounds, escalation re-arm, recovery)
	// describe the alert's whole story and need EVERY event, while dedup
	// decides whether a DELIVERY repeats. Evaluation runs for every
	// notification so shadow observation covers explicit-channel traffic
	// too; only routing is conditional on it.
	channels := n.Channels
	var summary *rules.GroupSummary
	var plan *rules.EscalationPlan
	var planRule string
	var planGroupKey string
	if s.rules != nil {
		decision, evalErr := s.rules.Evaluate(ctx, rules.NewEnv(
			n.Type, n.Level, directTitle(n), directBody(n), n.Params,
		))
		if s.observer != nil {
			if decision != nil {
				for _, hit := range decision.Shadow {
					s.observer.RecordShadow(hit.RuleID, hit.Channels, n)
				}
				for _, ee := range decision.EvalErrors {
					s.observer.RecordEvalError(ee.RuleID, ee.Err, n)
				}
			} else if evalErr != nil {
				// Every rule failed before producing any observation.
				s.observer.RecordEvalError("", evalErr, n)
			}
		}
		// Explicit channels win for ROUTING: rules are an incremental
		// capability over static routing, never an override of caller
		// intent. The active-rule outcomes that suppress delivery — a
		// silence window in effect, a "for" window still running, a group
		// round open for folding, root-cause inhibition — only apply to the
		// traffic the rule would route anyway; explicit-channel calls are
		// deliberate and go out regardless.
		if decision != nil && decision.Mode == rules.ModeActive && len(n.Channels) == 0 {
			if decision.Silenced {
				if s.observer != nil {
					s.observer.RecordSilenced(decision.RuleID, n)
				}
				return &ProcessResult{NotificationID: n.ID}, nil
			}
			if decision.ForPending {
				if s.observer != nil {
					s.observer.RecordForPending(decision.RuleID, n)
				}
				return &ProcessResult{NotificationID: n.ID}, nil
			}
			if decision.Folded {
				if s.observer != nil {
					s.observer.RecordGroupFolded(decision.RuleID, n)
				}
				return &ProcessResult{NotificationID: n.ID}, nil
			}
			if decision.Inhibited {
				if s.observer != nil {
					s.observer.RecordInhibited(decision.RuleID, n)
				}
				return &ProcessResult{NotificationID: n.ID}, nil
			}
			channels = decision.Channels
			summary = decision.Summary
			plan = decision.Escalation
			planRule = decision.RuleID
			planGroupKey = decision.GroupKey
		}
	}
	if len(channels) == 0 {
		routed, err := s.router.Route(n.Type, n.Level)
		if err != nil {
			return nil, fmt.Errorf("no route: %w", err)
		}
		channels = routed
	}

	// Render template if specified
	var renderedData *template.RenderedData
	if n.TemplateRef != "" {
		var err error
		renderedData, err = s.templates.Render(n.TemplateRef, n.Params)
		if err != nil {
			return nil, fmt.Errorf("template render: %w", err)
		}
		if n.Level == "" && renderedData.Level != "" {
			n.Level = renderedData.Level
		}
	}

	// Dedup — the last gate before delivery (key derived from notification
	// content, not auto-generated ID). Rule state has already advanced for
	// this event: a repeat delivery is suppressed, but the alert's story
	// (for/group/recovery) is not interrupted by it.
	if s.dedup != nil && s.dedup.Check(dedupKey(n)) {
		return &ProcessResult{NotificationID: n.ID}, nil
	}

	// Generate DeliveryTasks for each channel, collecting errors
	result := &ProcessResult{NotificationID: n.ID}
	s.enqueue(ctx, n, channels, renderedData, result)

	// The summary of a finished group round rides along with the event
	// that opened the new round. It is a synthetic notification produced
	// by the rule engine itself, so it bypasses rule evaluation (a
	// broadly-matching rule must not fold its own summaries back into the
	// group) and dedup (each round's summary differs in content, and a
	// dedup hit here would silently lose folded events).
	if summary != nil {
		s.enqueue(ctx, summaryNotification(n, summary), channels, nil, result)
	}

	// The delivery went out on the rule's routing: arm (or re-arm) the
	// ack-gated upgrade. A persistence failure of the scheduler is not a
	// delivery failure — the in-memory timer still fires, only a restart
	// would lose the pending upgrade — so it does not fail the Process.
	if plan != nil && s.escalation != nil {
		_ = s.escalation.Schedule(ctx, escalation.Pending{
			RuleID:    planRule,
			AlertID:   alertIDOf(n),
			To:        plan.To,
			Timeout:   plan.Timeout,
			CreatedAt: time.Now(),
			Type:      n.Type,
			Level:     n.Level,
			Title:     directTitle(n),
		})
	}

	// The delivery also opens (or continues) the alert's incident in the
	// ledger: the recovery side needs the episode's channels to deliver
	// the summary, the ack side needs the alert id.
	if s.incidents != nil {
		s.incidents.Open(incident.Opening{
			RuleID:   planRule,
			GroupKey: planGroupKey,
			AlertID:  alertIDOf(n),
			Title:    directTitle(n),
			Level:    n.Level,
			Channels: channels,
		})
	}

	// If zero tasks created, return error
	if len(result.TaskIDs) == 0 && len(result.Failed) > 0 {
		return result, fmt.Errorf("all channels failed: %s", formatChannelErrors(result.Failed))
	}

	return result, nil
}

// enqueue delivers one notification to the given channels: resolve the
// provider, plan the task and push it to the queue, recording per-channel
// failures into result. Shared by the live-event path and the synthetic
// group-summary path.
func (s *NotificationService) enqueue(ctx context.Context, n *core.Notification, channels []string, renderedData *template.RenderedData, result *ProcessResult) {
	for _, channel := range channels {
		provider, err := s.runtime.GetProvider(channel)
		if err != nil {
			result.Failed = append(result.Failed, ChannelError{Channel: channel, Error: err.Error()})
			continue
		}

		if !s.runtime.IsEnabled(channel) {
			result.Failed = append(result.Failed, ChannelError{Channel: channel, Error: "provider is disabled"})
			continue
		}

		targets := resolveTargets(n, channel)

		task, err := s.planner.Plan(ctx, provider, n, renderedData, targets, channel)
		if err != nil {
			result.Failed = append(result.Failed, ChannelError{Channel: channel, Error: err.Error()})
			continue
		}

		if err := s.queue.Push(ctx, task); err != nil {
			result.Failed = append(result.Failed, ChannelError{Channel: channel, Error: fmt.Sprintf("queue: %v", err)})
			continue
		}

		result.TaskIDs = append(result.TaskIDs, task.ID)
		result.Accepted = append(result.Accepted, channel)
	}
}

// alertIDOf derives the acknowledgement identity of a notification: the
// caller's business alert id from params when present, otherwise the
// content dedup key — a stable identity for the same alert content (the
// caller can then ack that key, though supplying an alert_id is the
// intended usage).
func alertIDOf(n *core.Notification) string {
	if v, ok := n.Params["alert_id"]; ok && v != nil {
		switch v := v.(type) {
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		default:
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return dedupKey(n)
}

// DeliverEscalation delivers the synthetic upgrade notification for a
// fired escalation: it repeats the alert on the plan's "to" channels so
// the wider audience sees what timed out without an ack. Like group
// summaries it bypasses rule evaluation and dedup — escalation exists
// exactly to repeat an alert that already went out.
func (s *NotificationService) DeliverEscalation(ctx context.Context, p escalation.Pending) error {
	if len(p.To) == 0 {
		return fmt.Errorf("escalation: no channels to escalate to")
	}
	n := escalationNotification(p)
	result := &ProcessResult{NotificationID: n.ID}
	s.enqueue(ctx, n, p.To, nil, result)
	if len(result.TaskIDs) == 0 && len(result.Failed) > 0 {
		return fmt.Errorf("escalation: all channels failed: %s", formatChannelErrors(result.Failed))
	}
	return nil
}

// Escalate implements escalation.Notifier: it delivers the upgrade in the
// background context of a timer callback. The attempt and its outcome land
// on the incident's timeline — an escalation nobody can see fired is an
// escalation that did not happen.
func (s *NotificationService) Escalate(p escalation.Pending) {
	if s.incidents != nil {
		s.incidents.Append(p.RuleID, p.AlertID, "escalation_fired", strings.Join(p.To, ", "))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.DeliverEscalation(ctx, p); err != nil && s.incidents != nil {
		s.incidents.Append(p.RuleID, p.AlertID, "escalation_failed", err.Error())
	}
}

// HandleResolved closes the episode the rule engine reports recovered
// (a fired group whose match stopped holding) and delivers the recovery
// summary on the episode's original channels. Delivery failures of the
// summary are recorded on the timeline but do not surface: the alert
// itself was already delivered, and resolution is a downgrade.
func (s *NotificationService) HandleResolved(ev rules.ResolvedEvent) {
	if s.incidents == nil {
		return
	}
	inc := s.incidents.Resolve(ev.RuleID, ev.GroupKey, ev.State.Count)
	if inc == nil {
		return
	}
	n := resolvedNotification(inc, ev)
	result := &ProcessResult{NotificationID: n.ID}
	s.enqueue(context.Background(), n, inc.Channels, nil, result)
	if len(result.TaskIDs) == 0 && len(result.Failed) > 0 {
		s.incidents.AppendTo(inc.ID, "resolve_delivery_failed",
			formatChannelErrors(result.Failed))
	}
}

// resolvedNotification builds the recovery summary: title, original alert
// identity and the recovery facts (events while open, episode length).
func resolvedNotification(inc *incident.Incident, ev rules.ResolvedEvent) *core.Notification {
	title := inc.Title
	if title == "" {
		title = inc.AlertID
	}
	events := ev.State.Count
	span := ev.State.LastSeen.Sub(ev.State.FirstSeen).Truncate(time.Second)
	return &core.Notification{
		ID:    uuid.New().String(),
		Type:  "resolve",
		Level: inc.Level,
		Content: &core.DirectContent{
			Title: "[Resolved] " + title,
			Body: fmt.Sprintf(
				"Alert %q recovered: %d events over %s while open (episode opened %s ago).",
				inc.AlertID, events, span, time.Since(inc.OpenedAt).Truncate(time.Second),
			),
		},
		Params: map[string]any{
			"alert_id":      inc.AlertID,
			"rule_id":       inc.RuleID,
			"incident_id":   inc.ID,
			"resolved":      true,
			"resolved_at":   time.Now().Format(time.RFC3339),
			"episode_event": events,
		},
	}
}

// escalationNotification builds the synthetic upgrade notification for a
// fired escalation, carrying the original alert's context and the
// escalation facts in both the body and params.
func escalationNotification(p escalation.Pending) *core.Notification {
	title := p.Title
	if title == "" {
		title = p.AlertID
	}
	return &core.Notification{
		ID:    uuid.New().String(),
		Type:  p.Type,
		Level: p.Level,
		Params: map[string]any{
			"escalation_rule": p.RuleID,
			"alert_id":        p.AlertID,
		},
		Content: &core.DirectContent{
			Title: fmt.Sprintf("[Escalation] %s", title),
			Body: fmt.Sprintf("Rule %s re-delivers alert %q: no acknowledgement arrived within %s.",
				p.RuleID, p.AlertID, p.Timeout),
		},
		CreatedAt: time.Now(),
	}
}

// summaryNotification builds the synthetic notification reporting a finished
// group round. It inherits the triggering event's type, level and recipients
// so it reaches the same audience through the same channel semantics, and
// carries the round's counts in both the body and params.
func summaryNotification(trigger *core.Notification, s *rules.GroupSummary) *core.Notification {
	group := s.Group
	if group == "" {
		group = "(content grouping)"
	}
	title := fmt.Sprintf("[Aggregation summary] %s: %d events", group, s.Count)
	body := fmt.Sprintf("Rule %s folded %d similar notifications between %s and %s; this summary is delivered together with the event that opened the new round.",
		s.RuleID, s.Count, s.FirstSeen.Format(time.RFC3339), s.LastSeen.Format(time.RFC3339))
	return &core.Notification{
		ID:    uuid.New().String(),
		Type:  trigger.Type,
		Level: trigger.Level,
		Params: map[string]any{
			"group_rule":       s.RuleID,
			"group":            group,
			"group_count":      s.Count,
			"group_first_seen": s.FirstSeen.Format(time.RFC3339),
			"group_last_seen":  s.LastSeen.Format(time.RFC3339),
		},
		Recipients: trigger.Recipients,
		Content:    &core.DirectContent{Title: title, Body: body},
		CreatedAt:  time.Now(),
	}
}

// resolveTargets returns the target list for a given channel
func resolveTargets(n *core.Notification, channel string) []string {
	if n.Recipients != nil {
		if targets, ok := n.Recipients[channel]; ok && len(targets) > 0 {
			return targets
		}
	}
	return nil
}

// directTitle returns the inline title available before template rendering;
// template-rendered text deliberately does not feed rule matching.
func directTitle(n *core.Notification) string {
	if n.Content != nil {
		return n.Content.Title
	}
	return ""
}

// directBody returns the inline body available before template rendering.
func directBody(n *core.Notification) string {
	if n.Content != nil {
		return n.Content.Body
	}
	return ""
}

// formatChannelErrors formats channel errors into a single string
func formatChannelErrors(errs []ChannelError) string {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, fmt.Sprintf("%s: %s", e.Channel, e.Error))
	}
	return fmt.Sprintf("%v", msgs)
}

// dedupKey generates a stable, canonical key from notification content
func dedupKey(n *core.Notification) string {
	// Build a canonical structure with sorted keys
	channels := make([]string, len(n.Channels))
	copy(channels, n.Channels)
	sort.Strings(channels)

	key := struct {
		Type       string              `json:"type"`
		Level      string              `json:"level"`
		Template   string              `json:"template"`
		Channels   []string            `json:"channels"`
		Recipients map[string][]string `json:"recipients"`
		Params     map[string]any      `json:"params"`
		Title      string              `json:"title,omitempty"`
		Body       string              `json:"body,omitempty"`
	}{
		Type:       n.Type,
		Level:      n.Level,
		Template:   n.TemplateRef,
		Channels:   channels,
		Recipients: n.Recipients,
		Params:     n.Params,
	}
	if n.Content != nil {
		key.Title = n.Content.Title
		key.Body = n.Content.Body
	}

	// json.Marshal on struct fields is deterministic (field order follows struct definition)
	// map entries are NOT guaranteed sorted, so we rely on the struct field order
	data, _ := json.Marshal(key)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
