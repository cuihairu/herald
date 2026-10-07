package audience

import (
	"testing"
	"time"
)

func TestOverlay(t *testing.T) {
	base, err := NewDeliveryPolicy(
		map[string]string{"alerts": "urgent", "marketing": "routine"},
		map[string]string{"sms": "L4"},
		map[string]string{"marketing": "fixed"},
		true,
	)
	if err != nil {
		t.Fatalf("base policy: %v", err)
	}

	ov := base.Overlay(Overlay{
		UrgencyByCategory: map[string]Urgency{"alerts": UrgencyCritical},
		ChannelIntensity:  map[string]Intensity{"email": IntensitySMS},
		ModeByCategory:    map[string]Mode{"alerts": ModeParallel},
	})

	// Overrides win per key.
	if got := ov.UrgencyFor("alerts"); got != UrgencyCritical {
		t.Fatalf("alerts urgency: want critical, got %v", got)
	}
	if got := ov.IntensityOf("email"); got != IntensitySMS {
		t.Fatalf("email intensity: want L4, got %v", got)
	}
	if got := ov.ModeFor("alerts", false); got != ModeParallel {
		t.Fatalf("alerts mode: want parallel, got %v", got)
	}

	// Untouched keys keep the base answers, taxonomy defaults included.
	if got := ov.UrgencyFor("marketing"); got != UrgencyRoutine {
		t.Fatalf("marketing urgency: want base routine, got %v", got)
	}
	if got := ov.UrgencyFor("unknown-cat"); got != UrgencyNormal {
		t.Fatalf("unknown category: want taxonomy normal, got %v", got)
	}
	if got := ov.IntensityOf("sms"); got != IntensitySMS {
		t.Fatalf("sms intensity: want base L4, got %v", got)
	}

	// The phone gate is not overridable: the copy carries the base flag,
	// and the base is untouched by composition.
	if !ov.phoneEnabled || !base.phoneEnabled {
		t.Fatalf("phone gate: want carried allow_phone, got %v / %v", ov.phoneEnabled, base.phoneEnabled)
	}

	// Overlay never mutates the base.
	if base.UrgencyFor("alerts") != UrgencyUrgent {
		t.Fatalf("base mutated: alerts urgency is %v", base.UrgencyFor("alerts"))
	}

	// An empty overlay answers exactly like the base.
	same := base.Overlay(Overlay{})
	if same.UrgencyFor("alerts") != base.UrgencyFor("alerts") ||
		same.IntensityOf("sms") != base.IntensityOf("sms") ||
		same.ModeFor("marketing", false) != base.ModeFor("marketing", false) {
		t.Fatalf("empty overlay diverged from base")
	}

	// Composed policy still plans, on the effective intensities: email
	// re-graded to L4 joins sms in one band, so the escalation chain
	// collapses to one stage carrying both — the override took.
	plan := ov.BuildPlan(ModeEscalation, []string{"email", "sms"}, 0)
	if len(plan.Stages) != 1 {
		t.Fatalf("escalation plan: want 1 band after email re-grade to L4, got %d", len(plan.Stages))
	}
	if len(plan.Stages[0].Channels) != 2 || plan.Stages[0].AckTimeout != 15*time.Minute {
		t.Fatalf("merged stage: want [email sms] at default wait, got %v", plan.Stages[0])
	}
}
