package rules

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/expr-lang/expr/vm"
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

// TestRunProgramTimeout exercises the evaluation timeout guard by stalling
// evalProgram past evalTimeout. The safety guard keeps every real expression
// far below the timeout (and its memory budget rejects oversized ranges), so
// the guard can only be reached by stalling the runner itself.
func TestRunProgramTimeout(t *testing.T) {
	prog, err := compileExpr("true")
	if err != nil {
		t.Fatalf("compileExpr: %v", err)
	}
	orig := evalProgram
	stalled := make(chan struct{})
	evalProgram = func(*vm.Program, any) (any, error) {
		time.Sleep(2 * evalTimeout)
		// Signal after the goroutine has loaded evalProgram and entered
		// this stub: restoring the variable below only once this has
		// closed keeps the swap race-free under -race.
		close(stalled)
		return true, nil
	}
	defer func() { evalProgram = orig }()

	e := NewEngine(NewMemoryStore())
	_, err = e.runProgram(context.Background(), prog, Env{})
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("runProgram past the timeout must fail with the timeout error, got %v", err)
	}
	<-stalled
}

// TestRunProgramCanceledContext pins the deterministic cancel path: a dead
// parent context fails before evaluation starts, even though expr.Run would
// finish quickly enough to race the cancellation in the select below.
func TestRunProgramCanceledContext(t *testing.T) {
	prog, err := compileExpr("true")
	if err != nil {
		t.Fatalf("compileExpr: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	e := NewEngine(NewMemoryStore())
	_, err = e.runProgram(ctx, prog, Env{})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("runProgram with a canceled context must fail, got %v", err)
	}
}
