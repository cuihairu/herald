package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/audit"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/digest"
	"github.com/cuihairu/herald/core/template"
	"github.com/google/uuid"
)

// The §13.3 触发面: the app dispatch composition. The api layer resolves
// the namespace half (category registration, urgency default, relation
// vocabulary, token scopes) and hands over the composed context here;
// the service owns the delivery half — 三方交集匹配, dedup, digest,
// enqueue — riding the same pipeline as /notify below the gate.

// DispatchSpec is the taxonomy half of one dispatch: everything the
// namespace already resolved. Urgency arrives parsed; audiences are the
// refs to deliver to ("user:", "group:", or plain channel names).
type DispatchSpec struct {
	App          string
	Category     string
	Urgency      audience.Urgency
	RelationType audience.RelationType
	Audiences    []string
	DedupKey     string
	EventID      string
	State        string
	Template     string
	Params       map[string]any
	Title, Body  string
}

// DispatchRequest carries the composed per-call context: the effective
// policy (namespace overrides over the operator's global tables), the
// 关系×联系面 filter, and the namespace's own template manager.
type DispatchRequest struct {
	Spec      DispatchSpec
	Policy    *audience.DeliveryPolicy
	Filter    *audience.Filter
	Templates *template.Manager
}

// DispatchOutcome is the §13.3 受理结果与投递计划摘要: who matched, who
// was refused and why, what the plan looks like. A dispatch where every
// channel was refused is still a successful acceptance — the refusals
// ARE the answer, not an error.
type DispatchOutcome struct {
	NotificationID string   `json:"notification_id"`
	Category       string   `json:"category"`
	Urgency        string   `json:"urgency"`
	Mode           string   `json:"mode,omitempty"`
	Suppressed     bool     `json:"suppressed,omitempty"`
	Plan           []PlanSt `json:"plan,omitempty"`
	Dispatched     []Aud    `json:"dispatched"`
	Refused        []Ref    `json:"refused,omitempty"`
	Folded         []string `json:"folded,omitempty"`
	// Delivery bookkeeping, same vocabulary as /notify.
	TaskIDs  []string       `json:"task_ids,omitempty"`
	Accepted []string       `json:"accepted,omitempty"`
	Failed   []ChannelError `json:"failed,omitempty"`
}

// PlanSt is one stage of the §6.3 delivery plan: the channels that go
// together and how long the escalation waits for an ack before the next
// stage. Only the first stage is enqueued by this face — later stages
// advance with the ack sources that arrive with the callback face.
type PlanSt struct {
	Channels   []string `json:"channels"`
	AckTimeout string   `json:"ack_timeout"`
}

// Aud is one audience's kept channel set.
type Aud struct {
	Audience string   `json:"audience"`
	Channels []string `json:"channels"`
}

// Ref is one refused audience×channel with the §6 reason
// (filtered / phone_disabled / intensity_exceeded).
type Ref struct {
	Audience string `json:"audience"`
	Channel  string `json:"channel"`
	Reason   string `json:"reason"`
}

// Dispatch accepts one namespace event and starts its delivery plan.
// The gates run in §14 order — dedup decides 要不要发 before matching
// decides 发给谁 — and the per-audience half reuses the enqueue
// pipeline (rss projection, planning, queue) unchanged.
func (s *NotificationService) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchOutcome, error) {
	if s.queue == nil {
		return nil, fmt.Errorf("queue is not configured")
	}
	spec := req.Spec
	out := &DispatchOutcome{
		Category:   spec.Category,
		Urgency:    spec.Urgency.String(),
		Dispatched: []Aud{},
	}

	// Template render happens once, through the namespace's own manager:
	// a dispatch never sees the operator's global template table.
	var renderedData *template.RenderedData
	if spec.Template != "" {
		if req.Templates == nil {
			return nil, fmt.Errorf("template %q named but the namespace has no template manager", spec.Template)
		}
		var err error
		renderedData, err = req.Templates.Render(spec.Template, spec.Params)
		if err != nil {
			return nil, fmt.Errorf("template render: %w", err)
		}
	}

	// One notification identity for the whole dispatch: every audience
	// copy carries it, so delivery logs and the fold ledger trace back
	// to one acceptance.
	nID := uuid.New().String()
	out.NotificationID = nID
	base := &core.Notification{
		ID:          nID,
		Type:        spec.Category,
		TemplateRef: spec.Template,
		Params:      spec.Params,
		DedupKey:    spec.DedupKey,
		EventID:     spec.EventID,
		State:       spec.State,
	}
	if spec.Title != "" || spec.Body != "" {
		base.Content = &core.DirectContent{Title: spec.Title, Body: spec.Body}
	}

	// §11 gate first (要不要发 before 发给谁): the key derives from the
	// explicit dedup_key or the spec content, so a repeat dispatch folds
	// regardless of which audiences it names.
	if s.gate != nil {
		gOut := s.gate.Decide(dedup.Event{
			EventID:     spec.EventID,
			Key:         gateKey(base),
			State:       spec.State,
			Category:    spec.Category,
			Fingerprint: nID,
		})
		if gOut.Decision == dedup.Suppress {
			if s.foldAudit != nil {
				s.foldAudit.Record(audit.Event{
					Kind:         audit.DeliveryDeduped,
					Category:     spec.Category,
					RelationType: string(spec.RelationType),
					Source:       "app:" + spec.App,
					Detail:       fmt.Sprintf("%s: %s (×%d)", gOut.Reason, gateKey(base), gOut.Count),
				})
			}
			out.Suppressed = true
			return out, nil
		}
	}

	// The §6.3 plan shape is category-driven, so it is one plan for the
	// whole dispatch. The mode's must-deliver input keys on the declared
	// relation type — 指派型 (enrollment) runs parallel by default —
	// until per-relation policy bits join the read path.
	mode := req.Policy.ModeFor(spec.Category, spec.RelationType == audience.RelationEnrollment)

	// Deterministic audience order keeps responses and delivery logs
	// stable across repeats.
	audiences := make([]string, len(spec.Audiences))
	copy(audiences, spec.Audiences)
	sort.Strings(audiences)

	res := &ProcessResult{NotificationID: nID}

	// Pass one — the 三方交集 match is per audience, the plan is per
	// dispatch: collect every audience's kept targets, refusals and
	// digest verdicts, then build one plan over the union of kept
	// channels.
	type audiencePlan struct {
		aud    string
		kept   []deliveryTarget
		folded bool
	}
	plans := make([]audiencePlan, 0, len(audiences))
	union := make([]string, 0, len(audiences))
	seenChannel := make(map[string]bool)
	for _, aud := range audiences {
		targets := s.dispatchCandidates(aud)
		if len(targets) == 0 {
			out.Refused = append(out.Refused, Ref{Audience: aud, Channel: "*", Reason: "no_channels"})
			continue
		}
		channels := make([]string, len(targets))
		for i, dt := range targets {
			channels[i] = dt.channel
		}
		matches := req.Policy.MatchChannels(req.Filter, aud, spec.Category, channels)

		kept := make([]deliveryTarget, 0, len(matches))
		keptNames := make([]string, 0, len(matches))
		for i, m := range matches {
			if !m.Kept {
				out.Refused = append(out.Refused, Ref{Audience: aud, Channel: m.Channel, Reason: m.Reason})
				continue
			}
			kept = append(kept, targets[i])
			keptNames = append(keptNames, m.Channel)
			if !seenChannel[m.Channel] {
				seenChannel[m.Channel] = true
				union = append(union, m.Channel)
			}
		}
		if len(kept) == 0 {
			continue
		}

		ap := audiencePlan{aud: aud, kept: kept}
		// §10 digest: an audience whose preference folds collects into
		// its window instead of delivering — the fold is per audience,
		// the rest of the dispatch is untouched.
		if s.digestAgg != nil {
			if digestMode, ok := digest.Resolve(s.digestPrefs, aud, spec.Category, keptNames); ok {
				nAud := *base
				nAud.AudienceID = aud
				nAud.RelationType = string(spec.RelationType)
				nAud.Source = "app:" + spec.App
				nAud.Level = levelFor(renderedData, spec.Urgency)
				s.digestAgg.Add(&nAud, digestMode)
				out.Folded = append(out.Folded, aud)
				ap.folded = true
			}
		}
		plans = append(plans, ap)
	}

	plan := req.Policy.BuildPlan(mode, union, 0)
	out.Plan = planSummary(plan)
	out.Mode = mode.String()

	// Pass two — deliver. Escalation enqueues its first stage now; the
	// remaining stages ride the plan summary and advance with the ack
	// sources that arrive with the callback face.
	stage := plan.Stages[0].Channels
	for _, ap := range plans {
		if ap.folded {
			continue
		}
		stageTargets := make([]deliveryTarget, 0, len(stage))
		for _, dt := range ap.kept {
			for _, ch := range stage {
				if dt.channel == ch {
					stageTargets = append(stageTargets, dt)
				}
			}
		}
		nAud := *base
		nAud.AudienceID = ap.aud
		nAud.RelationType = string(spec.RelationType)
		nAud.Source = "app:" + spec.App
		nAud.Level = levelFor(renderedData, spec.Urgency)
		s.deliverTargets(ctx, &nAud, stageTargets, renderedData, res)
		out.Dispatched = append(out.Dispatched, Aud{Audience: ap.aud, Channels: stage})
	}

	out.TaskIDs = res.TaskIDs
	out.Accepted = res.Accepted
	out.Failed = res.Failed
	if len(res.TaskIDs) == 0 && len(res.Failed) > 0 {
		return out, fmt.Errorf("all channels failed: %s", formatChannelErrors(res.Failed))
	}
	return out, nil
}

// dispatchCandidates resolves one audience's candidate channels: the
// audience's active contact surfaces when it holds any (the binding is
// the resolved delivery), else the reference expansion the /notify path
// uses — a zero-surface audience (config-seeded recipients, groups)
// keeps its pre-model behavior (§14 兼容).
func (s *NotificationService) dispatchCandidates(aud string) []deliveryTarget {
	if s.surfaces != nil {
		if surfaces := s.surfaces.Surfaces(aud); len(surfaces) > 0 {
			out := make([]deliveryTarget, 0, len(surfaces))
			for _, surface := range surfaces {
				if surface.Status != audience.SurfaceActive {
					continue
				}
				out = append(out, deliveryTarget{channel: surface.Channel, targets: []string{surface.Target}})
			}
			return out
		}
	}
	targets, _ := s.expandRefs([]string{aud})
	return targets
}

// planSummary renders the plan stages in wire vocabulary.
func planSummary(plan audience.Plan) []PlanSt {
	out := make([]PlanSt, 0, len(plan.Stages))
	for _, stage := range plan.Stages {
		out = append(out, PlanSt{
			Channels:   stage.Channels,
			AckTimeout: stage.AckTimeout.String(),
		})
	}
	return out
}

// levelFor derives the notification level from the rendered template,
// falling back to the urgency's vocabulary so downstream level-based
// styling has a stable value.
func levelFor(renderedData *template.RenderedData, urgency audience.Urgency) string {
	if renderedData != nil && renderedData.Level != "" {
		return renderedData.Level
	}
	switch urgency {
	case audience.UrgencyCritical:
		return "error"
	case audience.UrgencyUrgent:
		return "warning"
	default:
		return "info"
	}
}
