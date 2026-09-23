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

// RouteStep is one ordered routing candidate: the first step whose
// Match evaluates to true (empty Match always matches) wins.
type RouteStep struct {
	Match    string   `json:"match,omitempty" yaml:"match,omitempty"`
	Channels []string `json:"channels" yaml:"channels"`
}

// InhibitSpec suppresses lesser alerts while a root cause is active.
// Modeled in P1; enforcement lands in P2.
type InhibitSpec struct {
	Equal []string `json:"equal,omitempty" yaml:"equal,omitempty"`
}

// EscalationSpec re-notifies a wider channel when no ack arrives in time.
// Modeled in P1; enforcement lands in P3 (needs ACK tracking).
type EscalationSpec struct {
	AckTimeout string   `json:"ack_timeout,omitempty" yaml:"ack_timeout,omitempty"`
	To         []string `json:"to,omitempty" yaml:"to,omitempty"`
}

// SilenceSpec keeps a rule quiet during a daily window (HH:MM local time).
// Modeled in P1; enforcement lands in P2.
type SilenceSpec struct {
	Start string `json:"start,omitempty" yaml:"start,omitempty"`
	End   string `json:"end,omitempty" yaml:"end,omitempty"`
}

// Rule is the storage model of a notification rule. Enforced semantics:
// Match/Mode/Route in P1; For in P2 (event-driven duration judgement).
// GroupBy/Inhibit/Escalation/Silence are modeled but still rejected by
// Validate until implemented — accepting them silently would promise
// behavior that never happens.
type Rule struct {
	ID    string      `json:"id" yaml:"id"`
	Match string      `json:"match" yaml:"match"`
	Mode  Mode        `json:"mode,omitempty" yaml:"mode,omitempty"`
	Route []RouteStep `json:"route" yaml:"route"`

	For        *string         `json:"for,omitempty" yaml:"for,omitempty"`
	GroupBy    []string        `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	Inhibit    *InhibitSpec    `json:"inhibit,omitempty" yaml:"inhibit,omitempty"`
	Escalation *EscalationSpec `json:"escalation,omitempty" yaml:"escalation,omitempty"`
	Silence    *SilenceSpec    `json:"silence,omitempty" yaml:"silence,omitempty"`
}

var ruleIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// Normalize fills in defaults (empty mode becomes shadow) and trims the id.
func (r *Rule) Normalize() {
	r.ID = strings.TrimSpace(r.ID)
	if r.Mode == "" {
		r.Mode = ModeShadow
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
	// Fields below are modeled for forward compatibility but not enforced
	// yet; reject them so users never rely on behavior that does not exist.
	// For is enforced (P2): only its format is validated here.
	if r.For != nil {
		if _, err := ParseFor(*r.For); err != nil {
			return err
		}
	}
	if len(r.GroupBy) > 0 {
		return fmt.Errorf("rules: rule %q: group_by is not effective until P2 (drop it or wait)", r.ID)
	}
	if r.Inhibit != nil {
		return fmt.Errorf("rules: rule %q: inhibit is not effective until P2 (drop it or wait)", r.ID)
	}
	if r.Escalation != nil {
		return fmt.Errorf("rules: rule %q: escalation is not effective until P3 (drop it or wait)", r.ID)
	}
	if r.Silence != nil {
		return fmt.Errorf("rules: rule %q: silence is not effective until P2 (drop it or wait)", r.ID)
	}
	return nil
}
