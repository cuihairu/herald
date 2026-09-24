package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/dedup"
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

// NotificationService orchestrates the notification processing pipeline
type NotificationService struct {
	templates *template.Manager
	router    *route.Router
	runtime   ProviderRuntime
	dedup     *dedup.Dedup
	queue     core.Queue
	planner   *DeliveryPlanner
	rules     RuleEvaluator
	observer  RuleObserver
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

// Process processes a Notification, generates DeliveryTasks, and enqueues them.
func (s *NotificationService) Process(ctx context.Context, n *core.Notification) (*ProcessResult, error) {
	if s.queue == nil {
		return nil, fmt.Errorf("queue is not configured")
	}
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	n.CreatedAt = time.Now()

	// Dedup check — key derived from notification content, not auto-generated ID
	if s.dedup != nil && s.dedup.Check(dedupKey(n)) {
		return &ProcessResult{NotificationID: n.ID}, nil
	}

	// Rule evaluation — after dedup, before routing: intercepted events
	// must not consume queue capacity. Evaluation runs for every
	// notification so shadow observation covers explicit-channel traffic
	// too; only routing is conditional on it.
	channels := n.Channels
	var summary *rules.GroupSummary
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
