package rules

import (
	"fmt"
	"strings"
	"testing"
)

func validRule() Rule {
	return Rule{
		ID:    "high-fail-rate",
		Match: `params.fail_rate > 0.05`,
		Mode:  ModeActive,
		Route: []RouteStep{{Channels: []string{"oncall"}}},
	}
}

func TestRuleNormalize(t *testing.T) {
	r := Rule{ID: "  r1  ", Match: `level == "error"`}
	r.Normalize()
	if r.ID != "r1" {
		t.Errorf("expected trimmed id r1, got %q", r.ID)
	}
	if r.Mode != ModeShadow {
		t.Errorf("expected default mode shadow, got %q", r.Mode)
	}

	r2 := Rule{ID: "r2", Mode: ModeActive}
	r2.Normalize()
	if r2.Mode != ModeActive {
		t.Errorf("Normalize must not override explicit mode, got %q", r2.Mode)
	}
}

func TestRuleValidate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		r := validRule()
		if err := r.Validate(); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("bad ids", func(t *testing.T) {
		for _, id := range []string{"", " lead", "has space", "has/slash", strings.Repeat("x", 65)} {
			r := validRule()
			r.ID = id
			if err := r.Validate(); err == nil {
				t.Errorf("id %q: expected error, got nil", id)
			}
		}
		for _, id := range []string{"a", "A9._-", strings.Repeat("x", 64)} {
			r := validRule()
			r.ID = id
			if err := r.Validate(); err != nil {
				t.Errorf("id %q: expected no error, got %v", id, err)
			}
		}
	})

	t.Run("empty match", func(t *testing.T) {
		r := validRule()
		r.Match = "   "
		if err := r.Validate(); err == nil {
			t.Error("expected error for empty match")
		}
	})

	t.Run("unknown mode", func(t *testing.T) {
		r := validRule()
		r.Mode = Mode("wild")
		if err := r.Validate(); err == nil {
			t.Error("expected error for unknown mode")
		}
	})

	t.Run("route required", func(t *testing.T) {
		r := validRule()
		r.Route = nil
		if err := r.Validate(); err == nil {
			t.Error("expected error for empty route")
		}
	})

	t.Run("route step channels required", func(t *testing.T) {
		r := validRule()
		r.Route = []RouteStep{{Match: `level == "error"`}}
		if err := r.Validate(); err == nil {
			t.Error("expected error for step without channels")
		}

		r.Route = []RouteStep{{Channels: []string{" "}}}
		if err := r.Validate(); err == nil {
			t.Error("expected error for blank channel name")
		}
	})

	t.Run("for duration validated", func(t *testing.T) {
		// Valid durations are accepted since P2 enforces "for".
		for _, whole := range []string{"3m", "90s", "1h30m"} {
			r := validRule()
			r.For = &whole
			if err := r.Validate(); err != nil {
				t.Errorf("for %q: expected acceptance, got %v", whole, err)
			}
		}
		// Malformed, non-positive, and oversized values are rejected.
		for _, bad := range []string{"abc", "0s", "-5m", "25h", "9999h"} {
			r := validRule()
			r.For = &bad
			if err := r.Validate(); err == nil {
				t.Errorf("for %q: expected rejection", bad)
			}
		}
	})

	t.Run("group_by validated", func(t *testing.T) {
		// Valid group_by (with or without group_interval) is accepted since
		// P2 enforces group aggregation.
		r := validRule()
		r.GroupBy = []string{"env", "service"}
		if err := r.Validate(); err != nil {
			t.Errorf("group_by: expected acceptance, got %v", err)
		}
		interval := "10m"
		r.GroupInterval = &interval
		if err := r.Validate(); err != nil {
			t.Errorf("group_by + group_interval: expected acceptance, got %v", err)
		}

		// group_interval without group_by has nothing to aggregate over.
		r = validRule()
		r.GroupInterval = &interval
		if err := r.Validate(); err == nil {
			t.Error("group_interval without group_by: expected rejection")
		}

		// Malformed group_interval values are rejected.
		r = validRule()
		r.GroupBy = []string{"env"}
		for _, bad := range []string{"", "abc", "0s", "-5m", "25h"} {
			badInterval := bad
			r.GroupInterval = &badInterval
			if err := r.Validate(); err == nil {
				t.Errorf("group_interval %q: expected rejection", bad)
			}
		}

		// Field hygiene: bounded, non-empty, unique.
		r = validRule()
		r.GroupBy = []string{"env", "service", "env"}
		if err := r.Validate(); err == nil {
			t.Error("duplicate group_by field: expected rejection")
		}
		r.GroupBy = []string{"env", "  "}
		if err := r.Validate(); err == nil {
			t.Error("blank group_by field: expected rejection")
		}
		r.GroupBy = []string{"env", strings.Repeat("x", 65)}
		if err := r.Validate(); err == nil {
			t.Error("oversized group_by field: expected rejection")
		}
		r.GroupBy = make([]string, 9)
		for i := range r.GroupBy {
			r.GroupBy[i] = fmt.Sprintf("f%d", i)
		}
		if err := r.Validate(); err == nil {
			t.Error("more than 8 group_by fields: expected rejection")
		}
	})

	t.Run("inhibit validated", func(t *testing.T) {
		// A well-formed inhibit spec is accepted since P2 enforces
		// suppression; the source rule may be a forward reference.
		r := validRule()
		r.ID = "leaf-alert"
		ttl := "10m"
		r.Inhibit = &InhibitSpec{Source: "root-cause", Equal: []string{"env", "cluster"}, TTL: &ttl}
		if err := r.Validate(); err != nil {
			t.Errorf("inhibit: expected acceptance, got %v", err)
		}

		// Missing or malformed source id is rejected; self-reference too.
		r = validRule()
		r.Inhibit = &InhibitSpec{Equal: []string{"env"}}
		if err := r.Validate(); err == nil {
			t.Error("inhibit without source: expected rejection")
		}
		r = validRule()
		r.Inhibit = &InhibitSpec{Source: "bad id!", Equal: []string{"env"}}
		if err := r.Validate(); err == nil {
			t.Error("malformed inhibit source: expected rejection")
		}
		r = validRule()
		r.Inhibit = &InhibitSpec{Source: r.ID, Equal: []string{"env"}}
		if err := r.Validate(); err == nil {
			t.Error("self-inhibiting rule: expected rejection")
		}

		// equal field hygiene reuses the label-field rules.
		r = validRule()
		r.Inhibit = &InhibitSpec{Source: "root"}
		if err := r.Validate(); err == nil {
			t.Error("inhibit without equal fields: expected rejection")
		}
		r = validRule()
		r.Inhibit = &InhibitSpec{Source: "root", Equal: []string{"env", "env"}}
		if err := r.Validate(); err == nil {
			t.Error("duplicate equal fields: expected rejection")
		}

		// Malformed ttl values are rejected.
		r = validRule()
		for _, bad := range []string{"", "abc", "0s", "-5m", "25h"} {
			badTTL := bad
			r.Inhibit = &InhibitSpec{Source: "root", Equal: []string{"env"}, TTL: &badTTL}
			if err := r.Validate(); err == nil {
				t.Errorf("inhibit ttl %q: expected rejection", bad)
			}
		}
	})

	t.Run("not-yet-effective fields rejected", func(t *testing.T) {
		cases := map[string]func(*Rule){
			"escalation": func(r *Rule) { r.Escalation = &EscalationSpec{AckTimeout: "5m", To: []string{"boss"}} },
			"silence":    func(r *Rule) { r.Silence = &SilenceSpec{Start: "03:00", End: "07:00"} },
		}
		for name, mutate := range cases {
			r := validRule()
			mutate(&r)
			if err := r.Validate(); err == nil {
				t.Errorf("%s: expected rejection for P1-inert field", name)
			} else if !strings.Contains(err.Error(), "not effective") {
				t.Errorf("%s: error should explain the field is not effective, got %v", name, err)
			}
		}
	})
}
