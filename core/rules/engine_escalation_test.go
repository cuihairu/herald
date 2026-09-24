package rules

import (
	"context"
	"testing"
	"time"
)

func escalationEngine(t *testing.T, ackTimeout string) *Engine {
	t.Helper()
	engine := NewEngine(NewMemoryStore())
	r := validRule()
	r.ID = "r-up"
	r.Match = `level != ""`
	r.Escalation = &EscalationSpec{AckTimeout: ackTimeout, To: []string{"phone-bridge"}}
	if err := engine.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return engine
}

func TestEngineEscalationRidesTheDecision(t *testing.T) {
	engine := escalationEngine(t, "3m")
	d, err := engine.Evaluate(context.Background(), NewEnv("alert", "error", "t", "b", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || len(d.Channels) != 1 {
		t.Fatalf("expected a routed decision, got %+v", d)
	}
	if d.Escalation == nil {
		t.Fatalf("routed decision must carry the escalation plan")
	}
	if d.Escalation.Timeout != 3*time.Minute {
		t.Errorf("escalation timeout = %v, want 3m", d.Escalation.Timeout)
	}
	if len(d.Escalation.To) != 1 || d.Escalation.To[0] != "phone-bridge" {
		t.Errorf("escalation to = %v, want [phone-bridge]", d.Escalation.To)
	}
}

func TestEngineEscalationDefaultTimeout(t *testing.T) {
	engine := escalationEngine(t, "")
	d, err := engine.Evaluate(context.Background(), NewEnv("alert", "error", "t", "b", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Escalation == nil {
		t.Fatalf("expected a decision with an escalation plan, got %+v", d)
	}
	if d.Escalation.Timeout != DefaultAckTimeout {
		t.Errorf("default ack_timeout = %v, want %v", d.Escalation.Timeout, DefaultAckTimeout)
	}
}

func TestEngineRejectsBadAckTimeout(t *testing.T) {
	engine := NewEngine(NewMemoryStore())
	for name, spec := range map[string]*EscalationSpec{
		"unparsable": {AckTimeout: "soon", To: []string{"phone"}},
		"zero":       {AckTimeout: "0", To: []string{"phone"}},
		"negative":   {AckTimeout: "-5m", To: []string{"phone"}},
		"over cap":   {AckTimeout: "25h", To: []string{"phone"}},
	} {
		r := validRule()
		r.ID = "r-up"
		r.Escalation = spec
		if err := engine.Put(context.Background(), &r); err == nil {
			t.Errorf("%s: expected Put to reject the ack_timeout", name)
		}
	}
}
