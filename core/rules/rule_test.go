package rules

import (
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

	t.Run("not-yet-effective fields rejected", func(t *testing.T) {
		cases := map[string]func(*Rule){
			"group_by":   func(r *Rule) { r.GroupBy = []string{"env"} },
			"inhibit":    func(r *Rule) { r.Inhibit = &InhibitSpec{Equal: []string{"env"}} },
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
