package rules

import (
	"strings"
	"testing"
)

// TestCompileRuleRejectsInvalidFields drives compileRule directly with one
// malformed field at a time. Through Put these branches are unreachable —
// Validate compiles the same expressions first — but compileRule is a
// package-level function any caller may invoke, so each guard is exercised
// here instead of being annotated away.
func TestCompileRuleRejectsInvalidFields(t *testing.T) {
	badMatch := "&&&"
	cases := []struct {
		name  string
		tweak func(r *Rule)
	}{
		{"for", func(r *Rule) { r.For = strPtr("bogus") }},
		{"group_interval", func(r *Rule) {
			r.GroupBy = []string{"env"}
			r.GroupInterval = strPtr("bogus")
		}},
		{"inhibit_ttl", func(r *Rule) { r.Inhibit = &InhibitSpec{Source: "src", TTL: strPtr("bogus")} }},
		{"silence_window", func(r *Rule) { r.Silence = &SilenceSpec{Start: "25:99", End: "08:00"} }},
		{"silence_match", func(r *Rule) {
			r.Silence = &SilenceSpec{Start: "22:30", End: "06:00", Match: &badMatch}
		}},
		{"ack_timeout", func(r *Rule) { r.Escalation = &EscalationSpec{AckTimeout: "bogus"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validRule()
			tc.tweak(&r)
			compiled, err := compileRule(r)
			if err == nil {
				t.Fatalf("compileRule with invalid %s must fail, got %+v", tc.name, compiled)
			}
			if !strings.Contains(err.Error(), r.ID) {
				t.Fatalf("error must identify the rule, got %v", err)
			}
		})
	}
}
