package rules

import (
	"fmt"
	"regexp"
	"strings"
)

// Mode controls how a matched rule affects delivery.
type Mode string

const (
	// ModeShadow evaluates the rule on every notification but never
	// intercepts delivery; hits are recorded for observation (default).
	ModeShadow Mode = "shadow"
	// ModeActive overrides static routing when the rule matches.
	ModeActive Mode = "active"
	// ModeOff keeps the rule stored but skips it during evaluation.
	ModeOff Mode = "off"
)

// Action is what a matched rule does to the notification.
type Action string

const (
	// ActionRoute reroutes: the rule's Route steps resolve the channels
	// (the default for backward compatibility — every rule predating the
	// action field is a route rule).
	ActionRoute Action = "route"
	// ActionAllow passes the notification through unchanged: explicit
	// channels win, otherwise static routing applies — but evaluation
	// stops at this rule, so lower-priority rules cannot touch it. The
	// exemption primitive of the policy table.
	ActionAllow Action = "allow"
	// ActionSuppress withholds the notification outright. The unconditional
	// filter ("this traffic must never go out"), independent of the
	// schedule-driven silence and event-driven inhibit mechanisms.
	ActionSuppress Action = "suppress"
)

// RouteStep is one ordered routing candidate: the first step whose
// Match evaluates to true (empty Match always matches) wins.
type RouteStep struct {
	Match    string   `json:"match,omitempty" yaml:"match,omitempty"`
	Channels []string `json:"channels" yaml:"channels"`
}

// InhibitSpec suppresses lesser alerts while a root cause is active:
// while the source rule delivers, this rule's events whose equal-field
// values match a recorded presence entry are withheld (P2, event-driven
// TTL — refreshed on every source hit).
type InhibitSpec struct {
	// Source is the id of the root-cause rule whose deliveries mark
	// presence. It may be defined after this rule (forward reference).
	Source string `json:"source" yaml:"source"`
	// Equal lists the params fields whose values must match between the
	// source notification and this rule's notification for suppression to
	// apply (e.g. [env, cluster]).
	Equal []string `json:"equal" yaml:"equal"`
	// TTL is how long a presence entry suppresses after the last source
	// hit. Defaults to 30m; capped like other durations at 24h.
	TTL *string `json:"ttl,omitempty" yaml:"ttl,omitempty"`
}

// EscalationSpec re-notifies a wider channel when no ack arrives in time.
// The alert id used for acknowledgement is the notification's params
// "alert_id" (the caller's business identity); after ack_timeout without
// one, the delivery escalates to the "to" channels.
type EscalationSpec struct {
	AckTimeout string   `json:"ack_timeout,omitempty" yaml:"ack_timeout,omitempty"`
	To         []string `json:"to" yaml:"to"`
}

// SilenceSpec keeps a rule quiet during a daily window (HH:MM, process
// local time). Within the window the rule is frozen: its events are
// withheld and no for/group state advances. The optional match limits the
// silencing to matching events (e.g. `level != "critical"` silences
// everything but criticals during the window). Enforced in P2.
type SilenceSpec struct {
	Start string  `json:"start" yaml:"start"`
	End   string  `json:"end" yaml:"end"`
	Match *string `json:"match,omitempty" yaml:"match,omitempty"`
}

// Rule is the storage model of a notification rule. Every field is
// enforced: Match/Action/Mode/Route (the decision: what to match and
// whether to route, allow or suppress), Priority (evaluation order),
// For/GroupBy/GroupInterval (event-driven duration judgement and group
// aggregation), Inhibit and Silence (root-cause suppression and daily
// quiet windows), Escalation (ack-gated upgrade delivery).
type Rule struct {
	ID    string      `json:"id" yaml:"id"`
	Match string      `json:"match" yaml:"match"`
	Mode  Mode        `json:"mode,omitempty" yaml:"mode,omitempty"`
	Route []RouteStep `json:"route" yaml:"route"`

	// Action selects the decision on match: route (default), allow or
	// suppress. Route steps belong to action=route only; the stateful
	// fields below are route semantics too and rejected on allow/suppress.
	Action Action `json:"action,omitempty" yaml:"action,omitempty"`
	// Priority orders evaluation: higher first, ties keep insertion
	// order. The first matching active rule governs and stops evaluation.
	Priority int32 `json:"priority,omitempty" yaml:"priority,omitempty"`

	For     *string  `json:"for,omitempty" yaml:"for,omitempty"`
	GroupBy []string `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	// GroupInterval is how long a group must stay quiet before the next
	// event opens a new round; the folded events of the finished round are
	// delivered as a summary with that event. Defaults to 5m; requires
	// group_by.
	GroupInterval *string         `json:"group_interval,omitempty" yaml:"group_interval,omitempty"`
	Inhibit       *InhibitSpec    `json:"inhibit,omitempty" yaml:"inhibit,omitempty"`
	Escalation    *EscalationSpec `json:"escalation,omitempty" yaml:"escalation,omitempty"`
	Silence       *SilenceSpec    `json:"silence,omitempty" yaml:"silence,omitempty"`
}

var ruleIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// maxGroupByFields caps the group_by dimension count; more dimensions than
// that almost always means the rule wanted a different match expression.
const maxGroupByFields = 8

// validateLabelFields checks a list of params field names (group_by /
// inhibit.equal): non-empty, bounded and unique.
func validateLabelFields(r *Rule, fields []string, what string) error {
	if len(fields) == 0 {
		return fmt.Errorf("rules: rule %q: %s requires at least one field", r.ID, what)
	}
	if len(fields) > maxGroupByFields {
		return fmt.Errorf("rules: rule %q: %s has %d fields, max is %d", r.ID, what, len(fields), maxGroupByFields)
	}
	seen := make(map[string]bool, len(fields))
	for i, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" || len(f) > 64 {
			return fmt.Errorf("rules: rule %q: %s field %d must be 1-64 chars", r.ID, what, i)
		}
		if seen[f] {
			return fmt.Errorf("rules: rule %q: duplicate %s field %q", r.ID, what, f)
		}
		seen[f] = true
	}
	return nil
}

// validateGroupBy checks the group aggregation fields and the group
// interval; group_interval is only meaningful together with group_by.
func validateGroupBy(r *Rule) error {
	if len(r.GroupBy) == 0 {
		return fmt.Errorf("rules: rule %q: group_interval requires group_by", r.ID)
	}
	if err := validateLabelFields(r, r.GroupBy, "group_by"); err != nil {
		return err
	}
	if r.GroupInterval != nil {
		if _, err := ParseGroupInterval(*r.GroupInterval); err != nil {
			return err
		}
	}
	return nil
}

// validateInhibit checks the suppression spec: the source rule id must be
// well-formed (the rule itself may not exist yet), the equal fields must be
// hygienic, and the ttl, if set, must parse.
func validateInhibit(r *Rule) error {
	if !ruleIDPattern.MatchString(r.Inhibit.Source) {
		return fmt.Errorf("rules: rule %q: inhibit.source %q is not a valid rule id", r.ID, r.Inhibit.Source)
	}
	if r.Inhibit.Source == r.ID {
		return fmt.Errorf("rules: rule %q: a rule cannot inhibit itself", r.ID)
	}
	if err := validateLabelFields(r, r.Inhibit.Equal, "inhibit.equal"); err != nil {
		return err
	}
	if r.Inhibit.TTL != nil {
		if _, err := parseDurationField("inhibit ttl", *r.Inhibit.TTL); err != nil {
			return err
		}
	}
	return nil
}

// validateSilence checks the daily window: both bounds must parse as
// HH:MM and the window must be non-zero length.
func validateSilence(r *Rule) error {
	if _, err := ParseSilenceWindow(r.Silence.Start, r.Silence.End); err != nil {
		return fmt.Errorf("rules: rule %q: %w", r.ID, err)
	}
	return nil
}

// validateEscalation checks the upgrade spec: ack_timeout parses when set
// (default otherwise), and to names at least one non-blank channel.
func validateEscalation(r *Rule) error {
	if r.Escalation.AckTimeout != "" {
		if _, err := parseDurationField("ack_timeout", r.Escalation.AckTimeout); err != nil {
			return err
		}
	}
	if len(r.Escalation.To) == 0 {
		return fmt.Errorf("rules: rule %q: escalation.to must list at least one channel", r.ID)
	}
	for i, ch := range r.Escalation.To {
		if strings.TrimSpace(ch) == "" {
			return fmt.Errorf("rules: rule %q: escalation.to channel %d is empty", r.ID, i)
		}
	}
	return nil
}

// Normalize fills in defaults (empty mode becomes shadow, empty action
// becomes route) and trims the id.
func (r *Rule) Normalize() {
	r.ID = strings.TrimSpace(r.ID)
	if r.Mode == "" {
		r.Mode = ModeShadow
	}
	if r.Action == "" {
		r.Action = ActionRoute
	}
}

// Validate checks structural hygiene; expression compilation is a separate
// step (Engine.Validate) so this stays cheap for storage-layer reuse.
func (r Rule) Validate() error {
	if !ruleIDPattern.MatchString(r.ID) {
		return fmt.Errorf("rules: invalid rule id %q (want 1-64 chars of letters, digits, dot, dash, underscore)", r.ID)
	}
	if strings.TrimSpace(r.Match) == "" {
		return fmt.Errorf("rules: rule %q: match expression is empty", r.ID)
	}
	switch r.Mode {
	case ModeShadow, ModeActive, ModeOff:
	default:
		return fmt.Errorf("rules: rule %q: unknown mode %q", r.ID, r.Mode)
	}
	// Validate stays usable without a prior Normalize: an empty action
	// reads as the default route action (Normalize writes the same).
	action := r.Action
	if action == "" {
		action = ActionRoute
	}
	switch action {
	case ActionRoute, ActionAllow, ActionSuppress:
	default:
		return fmt.Errorf("rules: rule %q: unknown action %q", r.ID, r.Action)
	}
	if action == ActionRoute {
		if len(r.Route) == 0 {
			return fmt.Errorf("rules: rule %q: route must list at least one step", r.ID)
		}
		for i, step := range r.Route {
			if len(step.Channels) == 0 {
				return fmt.Errorf("rules: rule %q: route step %d has no channels", r.ID, i)
			}
			for _, ch := range step.Channels {
				if strings.TrimSpace(ch) == "" {
					return fmt.Errorf("rules: rule %q: route step %d has an empty channel name", r.ID, i)
				}
			}
		}
	} else if len(r.Route) > 0 {
		// A route table on an allow/suppress rule is a mixed intent — the
		// action already decided the outcome — and rejecting it at save
		// time beats silently ignoring it.
		return fmt.Errorf("rules: rule %q: route steps require action %q (rule has %q)", r.ID, ActionRoute, action)
	}
	if action != ActionRoute && (r.For != nil || len(r.GroupBy) > 0 || r.GroupInterval != nil ||
		r.Inhibit != nil || r.Escalation != nil || r.Silence != nil) {
		// for/group_by/inhibit/silence/escalation gate WHEN a routed
		// delivery happens; an allow/suppress rule has no delivery to gate.
		return fmt.Errorf("rules: rule %q: action %q cannot combine for/group_by/inhibit/silence/escalation", r.ID, action)
	}
	// All modeled fields are enforced; validation only checks their format
	// (compilation of match/silence.match happens in Engine.Validate).
	if r.For != nil {
		if _, err := ParseFor(*r.For); err != nil {
			return err
		}
	}
	if len(r.GroupBy) > 0 || r.GroupInterval != nil {
		if err := validateGroupBy(&r); err != nil {
			return err
		}
	}
	if r.Inhibit != nil {
		if err := validateInhibit(&r); err != nil {
			return err
		}
	}
	if r.Silence != nil {
		if err := validateSilence(&r); err != nil {
			return err
		}
	}
	if r.Escalation != nil {
		if err := validateEscalation(&r); err != nil {
			return err
		}
	}
	return nil
}
