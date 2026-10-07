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
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/audit"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/digest"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/feeds"
	"github.com/cuihairu/herald/core/groups"
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
// group-aggregation round, events withheld by root-cause inhibition,
// events withheld by a rule's daily silence window, and events withheld
// by an explicit suppress action or the default deny policy (empty rule
// id there — the configuration decided, not a rule).
// Implementations must be safe for concurrent use.
type RuleObserver interface {
	RecordShadow(ruleID string, channels []string, n *core.Notification)
	RecordEvalError(ruleID string, err error, n *core.Notification)
	RecordForPending(ruleID string, n *core.Notification)
	RecordGroupFolded(ruleID string, n *core.Notification)
	RecordInhibited(ruleID string, n *core.Notification)
	RecordSilenced(ruleID string, n *core.Notification)
	RecordSuppressed(ruleID string, n *core.Notification)
}

// EscalationScheduler arms and cancels ack-gated upgrade deliveries for
// routed rule events (nil by default: no escalation).
type EscalationScheduler interface {
	Schedule(ctx context.Context, p escalation.Pending) error
	Cancel(ctx context.Context, alertID string) error
}

// GroupResolver expands group references (the "group:" prefix on channel
// names) into their members. groups.Manager's Resolver implements it.
type GroupResolver interface {
	ExpandGroup(name string) ([]groups.Member, bool)
}

// UserResolver expands user references (the "user:" prefix on channel
// names) into their endpoints. audience.Manager implements it.
type UserResolver interface {
	ExpandUser(name string) ([]audience.Endpoint, bool)
}

// ChannelResolver expands plain channel names from the channels
// configuration block into their provider lists. route.Router implements
// it. nil (the default) keeps plain names literal.
type ChannelResolver interface {
	ExpandChannel(name string) ([]string, bool)
}

// NotificationService orchestrates the notification processing pipeline
type NotificationService struct {
	templates   *template.Manager
	router      *route.Router
	runtime     ProviderRuntime
	dedup       *dedup.Dedup
	foldAudit   audit.Recorder
	digestAgg   *digest.Aggregator
	digestPrefs *audience.PreferenceRegistry
	feeds       *feeds.Store
	queue       core.Queue
	planner     *DeliveryPlanner
	rules       RuleEvaluator
	observer    RuleObserver
	escalation  EscalationScheduler
	incidents   *incident.Store
	groups      GroupResolver
	users       UserResolver
	channels    ChannelResolver
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

// SetFoldAudit wires the audit trail receiving one row per dedup-suppressed
// notification — the「为什么这条没投」answer for folded duplicates. nil (the
// default) keeps processing unchanged.
func (s *NotificationService) SetFoldAudit(r audit.Recorder) {
	s.foldAudit = r
}

// SetDigest wires the §10 aggregator: an event whose audience preference
// folds (daily/weekly) is collected into its window instead of delivered,
// and each flipped window later emits one summary through this same
// pipeline (FlushDigest / digest.FlipLoop). A nil preference registry
// resolves through the category default table alone. nil aggregator (the
// default) keeps every event direct.
func (s *NotificationService) SetDigest(agg *digest.Aggregator, prefs *audience.PreferenceRegistry) {
	s.digestAgg = agg
	s.digestPrefs = prefs
}

// SetFeeds wires the §9 RSS pull-channel store (边界审计 §4: RSS 是拉式
// 渠道，不是 provider). Delivery targets whose channel classifies as
// RSS are projected into the store instead of planned as provider tasks;
// readers pull them on their own cadence. nil store (the default) keeps
// the rss channel name behaving as any unknown provider — a visible
// per-channel failure.
func (s *NotificationService) SetFeeds(store *feeds.Store) {
	s.feeds = store
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

// SetGroupResolver attaches the group reference expander. nil (the
// default) keeps channel references literal; a "group:" reference with no
// resolver fails that one channel at delivery time, not the whole
// notification.
func (s *NotificationService) SetGroupResolver(gr GroupResolver) {
	s.groups = gr
}

// SetUserResolver attaches the user reference expander. nil (the
// default) keeps channel references literal; a "user:" reference with no
// resolver fails that one channel at delivery time, not the whole
// notification.
func (s *NotificationService) SetUserResolver(ur UserResolver) {
	s.users = ur
}

// SetChannelResolver attaches the channels-block expander. nil (the
// default) keeps plain channel names literal — an unknown provider name
// fails at delivery time exactly as before.
func (s *NotificationService) SetChannelResolver(cr ChannelResolver) {
	s.channels = cr
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
		// intent. The outcomes that withhold delivery — an explicit
		// suppress action, the default deny policy, a silence window in
		// effect, a "for" window still running, a group round open for
		// folding, root-cause inhibition — only apply to the traffic the
		// rule table would route anyway; explicit-channel calls are
		// deliberate and go out regardless.
		//
		// The default policy suppresses whatever the table did not govern
		// (deny), shadow hits included — check it before the mode: a
		// defaulted decision may carry shadow observations with ModeShadow.
		if decision != nil && decision.Defaulted && len(n.Channels) == 0 {
			if s.observer != nil {
				s.observer.RecordSuppressed(decision.RuleID, n)
			}
			return &ProcessResult{NotificationID: n.ID}, nil
		}
		if decision != nil && decision.Mode == rules.ModeActive && len(n.Channels) == 0 {
			// An empty action reads as route — decisions predate the action
			// field (embedded stub evaluators still produce them), and a
			// decision carrying channels means routing.
			action := decision.Action
			if action == "" {
				action = rules.ActionRoute
			}
			if action == rules.ActionSuppress {
				if s.observer != nil {
					s.observer.RecordSuppressed(decision.RuleID, n)
				}
				return &ProcessResult{NotificationID: n.ID}, nil
			}
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
			if action == rules.ActionRoute {
				channels = decision.Channels
				summary = decision.Summary
				plan = decision.Escalation
				planRule = decision.RuleID
				planGroupKey = decision.GroupKey
			}
			// ActionAllow: the caller's course stands — evaluation stopped
			// at the allowing rule, and static routing below applies when
			// the caller named no channels.
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
		if s.foldAudit != nil {
			s.foldAudit.Record(audit.Event{
				Kind:         audit.DeliveryDeduped,
				AudienceID:   n.AudienceID,
				Category:     n.Type,
				RelationType: n.RelationType,
				Source:       n.Source,
				Detail:       dedupKey(n),
			})
		}
		return &ProcessResult{NotificationID: n.ID}, nil
	}

	// Digest aggregation (§10): an event whose preference folds is
	// collected into its audience×category window instead of delivered;
	// the flipped window later emits one summary through this same
	// pipeline. Realtime-preferring channels keep the event direct —
	// 直投 wins over folding. Dedup has already run, so a folded
	// duplicate never enters a window.
	if s.digestAgg != nil {
		if mode, ok := digest.Resolve(s.digestPrefs, n.AudienceID, n.Type, n.Channels); ok {
			s.digestAgg.Add(n, mode)
			return &ProcessResult{NotificationID: n.ID}, nil
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

// enqueue delivers one notification to the given channel references:
// expand each reference (plain channel or "group:" reference), resolve the
// provider, plan the task and push it to the queue, recording per-channel
// failures into result. Shared by every delivery path — live events, rule
// routes, static routes, group summaries, escalations, recoveries — so
// group expansion semantics exist exactly once.
// FlushDigest emits every window due right now as one summary per
// batch, returning how many summaries were enqueued (the first flush
// error, if any, rides alongside — the batches that succeeded keep
// their deliveries). digest.FlipLoop is the scheduled driver; this is
// the synchronous face for single-shot flushes and tests.
func (s *NotificationService) FlushDigest() (int, error) {
	if s.digestAgg == nil {
		return 0, nil
	}
	made := 0
	var firstErr error
	for _, b := range s.digestAgg.Due() {
		if err := s.FlushDigestBatch(b); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		made++
	}
	return made, firstErr
}

// FlushDigestBatch emits one flipped window as a summary notification
// through the normal pipeline — the summary enjoys the full retry and
// audit chain (关系详设 §10). Rendering reuses the template system: a
// registered "digest:<category>" template replaces the built-in
// 「今日 5 条告警」 shape with count/items/audience params. The summary
// keeps the window's category type, audience reference, relation
// snapshot and the union of the folded events' channels; folded events
// that named no channels fall back to static routing exactly as a
// direct notification would.
func (s *NotificationService) FlushDigestBatch(b digest.Batch) error {
	if len(b.Events) == 0 {
		return nil
	}
	latest := b.Events[len(b.Events)-1]
	unit := "今日"
	if b.Mode == digest.Weekly {
		unit = "本周"
	}
	title := fmt.Sprintf("%s %d 条%s", unit, len(b.Events), b.Key.Category)
	var body strings.Builder
	for _, ev := range b.Events {
		fmt.Fprintf(&body, "- %s %s\n", ev.CreatedAt.Format("01-02 15:04"), directTitle(ev))
	}
	sum := &core.Notification{
		ID:           uuid.New().String(),
		Type:         b.Key.Category,
		Level:        latest.Level,
		AudienceID:   b.Key.AudienceID,
		Channels:     unionChannels(b.Events),
		Content:      &core.DirectContent{Title: title, Body: strings.TrimRight(body.String(), "\n")},
		CreatedAt:    time.Now(),
		RelationType: latest.RelationType,
		Source:       latest.Source,
	}
	var rendered *template.RenderedData
	if s.templates != nil {
		tplID := "digest:" + b.Key.Category
		if _, err := s.templates.Get(tplID); err == nil {
			sum.TemplateRef = tplID
			sum.Params = map[string]any{
				"count":    len(b.Events),
				"items":    itemTitles(b.Events),
				"audience": b.Key.AudienceID,
				"mode":     string(b.Mode),
			}
			sum.Content = nil
			rendered, err = s.templates.Render(tplID, sum.Params)
			if err != nil {
				return fmt.Errorf("digest summary %s/%s render: %w", b.Key.AudienceID, b.Key.Category, err)
			}
			if sum.Level == "" && rendered.Level != "" {
				sum.Level = rendered.Level
			}
		}
	}
	if len(sum.Channels) == 0 {
		routed, err := s.router.Route(sum.Type, sum.Level)
		if err != nil {
			return fmt.Errorf("digest summary %s/%s: no route: %w", b.Key.AudienceID, b.Key.Category, err)
		}
		sum.Channels = routed
	}
	result := &ProcessResult{NotificationID: sum.ID}
	s.enqueue(context.Background(), sum, sum.Channels, rendered, result)
	return nil
}

// unionChannels collects every channel the folded events named, first
// occurrence order preserved, duplicates dropped.
func unionChannels(events []*core.Notification) []string {
	seen := make(map[string]bool)
	var out []string
	for _, ev := range events {
		for _, ch := range ev.Channels {
			if ch == "" || seen[ch] {
				continue
			}
			seen[ch] = true
			out = append(out, ch)
		}
	}
	return out
}

// itemTitles lists the folded events' titles for template rendering.
func itemTitles(events []*core.Notification) []string {
	titles := make([]string, 0, len(events))
	for _, ev := range events {
		titles = append(titles, directTitle(ev))
	}
	return titles
}

func (s *NotificationService) enqueue(ctx context.Context, n *core.Notification, channels []string, renderedData *template.RenderedData, result *ProcessResult) {
	// Routing decision first (pure), delivery execution after: the queue
	// is only touched by the execution half.
	targets, failed := s.expandRefs(channels)
	result.Failed = append(result.Failed, failed...)
	projected := false
	for _, dt := range targets {
		if s.feeds != nil && audience.ClassifyChannel(dt.channel) == audience.ChannelRSS {
			// §9 pull channel: project once per notification — a mixed
			// rss+email fan-out both files the item and delivers the push
			// half; the pull half never becomes a provider task.
			if !projected {
				s.projectFeed(n, renderedData, result)
				projected = true
			}
			continue
		}
		s.enqueueOne(ctx, n, dt, renderedData, result)
	}
}

// projectFeed files one notification into the feed store as its pull
// projection (§9): the rendered (or direct) title, the direct body, the
// audience reference (empty marks 公开内容) and the notification id as
// the feed guid. A store that refuses the item lands in result.Failed —
// reported like a failed provider, not swallowed.
func (s *NotificationService) projectFeed(n *core.Notification, renderedData *template.RenderedData, result *ProcessResult) {
	title := directTitle(n)
	if renderedData != nil && renderedData.Title != "" {
		title = renderedData.Title
	}
	body := directBody(n)
	if err := s.feeds.Add(feeds.Item{
		ID:          n.ID,
		Category:    n.Type,
		Title:       title,
		Body:        body,
		AudienceID:  n.AudienceID,
		PublishedAt: n.CreatedAt,
	}); err != nil {
		result.Failed = append(result.Failed, ChannelError{Channel: "rss", Error: err.Error()})
	}
}

// deliveryTarget is one concrete delivery after reference expansion: a
// provider instance and optionally pinned recipients (group members may
// pin them; nil falls back to the notification's own recipient list).
type deliveryTarget struct {
	channel string
	targets []string
}

// expandRefs resolves a batch of channel references into concrete
// delivery targets. It is the pure decision step of routing: it never
// touches the queue, so Routing (decide who gets what) stays separable
// from Delivery (Plan, Push, retry). A reference that fails to resolve
// is recorded as a per-channel failure and the rest still expand — no
// single bad reference takes the notification down.
func (s *NotificationService) expandRefs(refs []string) ([]deliveryTarget, []ChannelError) {
	targets := make([]deliveryTarget, 0, len(refs))
	var failed []ChannelError
	for _, ref := range refs {
		expanded, err := s.expandRef(ref)
		if err != nil {
			// An unknown group (or a group reference with no resolver) is
			// configuration drift: that reference fails, visibly, and the
			// remaining channels still go out.
			failed = append(failed, ChannelError{Channel: ref, Error: err.Error()})
			continue
		}
		targets = append(targets, expanded...)
	}
	return targets, failed
}

// expandRef resolves one channel reference. Plain names resolve by
// priority: an explicit provider instance wins over the channels block
// (channel 显式 > channels 块); a name in neither stays a literal
// provider target whose miss fails that channel at delivery time, as
// before. The routes table sits below both — it only routes
// notifications that named no channels at all. "group:" references
// expand to the group's members (each carrying its optional recipient
// pins); "user:" references expand to their endpoints, merged per
// provider instance.
func (s *NotificationService) expandRef(ref string) ([]deliveryTarget, error) {
	if groups.IsRef(ref) {
		if s.groups == nil {
			return nil, fmt.Errorf("group reference %q but no group resolver is configured", ref)
		}
		members, ok := s.groups.ExpandGroup(strings.TrimPrefix(ref, groups.RefPrefix))
		if !ok {
			return nil, fmt.Errorf("unknown group %q", strings.TrimPrefix(ref, groups.RefPrefix))
		}
		out := make([]deliveryTarget, 0, len(members))
		for _, m := range members {
			out = append(out, deliveryTarget{channel: m.Channel, targets: m.Recipients})
		}
		return out, nil
	}
	if audience.IsUserRef(ref) {
		if s.users == nil {
			return nil, fmt.Errorf("user reference %q but no user resolver is configured", ref)
		}
		name := strings.TrimPrefix(ref, audience.UserPrefix)
		eps, ok := s.users.ExpandUser(name)
		if !ok {
			return nil, fmt.Errorf("unknown user %q", name)
		}
		return mergeUserEndpoints(eps), nil
	}
	if _, err := s.runtime.GetProvider(ref); err != nil && s.channels != nil {
		if providers, ok := s.channels.ExpandChannel(ref); ok {
			out := make([]deliveryTarget, 0, len(providers))
			for _, p := range providers {
				out = append(out, deliveryTarget{channel: p})
			}
			return out, nil
		}
	}
	return []deliveryTarget{{channel: ref}}, nil
}

// mergeUserEndpoints folds endpoints onto their provider instance so a
// user fan-out is one task per provider (all targets bundled in it),
// never one task per endpoint — several recipients sharing an instance
// still produce a single delivery. Deterministic order keeps tasks and
// logs stable across runs.
func mergeUserEndpoints(eps []audience.Endpoint) []deliveryTarget {
	byType := make(map[string][]string)
	for _, ep := range eps {
		byType[ep.Type] = append(byType[ep.Type], ep.Target)
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	out := make([]deliveryTarget, 0, len(types))
	for _, t := range types {
		out = append(out, deliveryTarget{channel: t, targets: byType[t]})
	}
	return out
}

// enqueueOne plans and pushes the delivery to one concrete channel.
func (s *NotificationService) enqueueOne(ctx context.Context, n *core.Notification, dt deliveryTarget, renderedData *template.RenderedData, result *ProcessResult) {
	provider, err := s.runtime.GetProvider(dt.channel)
	if err != nil {
		result.Failed = append(result.Failed, ChannelError{Channel: dt.channel, Error: err.Error()})
		return
	}

	if !s.runtime.IsEnabled(dt.channel) {
		result.Failed = append(result.Failed, ChannelError{Channel: dt.channel, Error: "provider is disabled"})
		return
	}

	// A pinned target list (group member recipients) overrides the
	// notification's own; without one the notification decides.
	targets := dt.targets
	if len(targets) == 0 {
		targets = resolveTargets(n, dt.channel)
	}

	task, err := s.planner.Plan(ctx, provider, n, renderedData, targets, dt.channel)
	if err != nil {
		result.Failed = append(result.Failed, ChannelError{Channel: dt.channel, Error: err.Error()})
		return
	}

	if err := s.queue.Push(ctx, task); err != nil {
		result.Failed = append(result.Failed, ChannelError{Channel: dt.channel, Error: fmt.Sprintf("queue: %v", err)})
		return
	}

	result.TaskIDs = append(result.TaskIDs, task.ID)
	result.Accepted = append(result.Accepted, dt.channel)
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
