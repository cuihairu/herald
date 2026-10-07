package audience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func policy(t *testing.T, allowPhone bool) *DeliveryPolicy {
	t.Helper()
	d, err := NewDeliveryPolicy(nil, nil, nil, allowPhone)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestBuildPlanParallel: parallel is one stage holding every channel.
func TestBuildPlanParallel(t *testing.T) {
	p := policy(t, false).BuildPlan(ModeParallel, []string{"email", "sms"}, 30*time.Second)
	if p.Mode != ModeParallel || len(p.Stages) != 1 {
		t.Fatalf("plan = %+v, want one parallel stage", p)
	}
	if len(p.Stages[0].Channels) != 2 || p.Stages[0].AckTimeout != 30*time.Second {
		t.Errorf("stage = %+v, want both channels and a 30s ack timeout", p.Stages[0])
	}
}

// TestBuildPlanEscalationGroupsByIntensity: stages ascend the ladder and
// equal-intensity channels share a stage; a non-positive timeout falls
// back to the 15m default.
func TestBuildPlanEscalationGroupsByIntensity(t *testing.T) {
	p := policy(t, false).BuildPlan(ModeEscalation, []string{"email", "sms", "rss", "mail"}, 0)
	if p.Mode != ModeEscalation {
		t.Fatalf("mode = %v, want escalation", p.Mode)
	}
	if len(p.Stages) != 3 {
		t.Fatalf("stages = %d, want 3 (rss, email+mail, sms): %+v", len(p.Stages), p.Stages)
	}
	if p.Stages[0].Channels[0] != "rss" {
		t.Errorf("first stage = %v, want the L0 channel first", p.Stages[0].Channels)
	}
	if len(p.Stages[1].Channels) != 2 || p.Stages[1].Channels[0] != "email" || p.Stages[1].Channels[1] != "mail" {
		t.Errorf("middle stage = %v, want email+mail grouped (both L1)", p.Stages[1].Channels)
	}
	if p.Stages[2].Channels[0] != "sms" {
		t.Errorf("last stage = %v, want the L4 channel last", p.Stages[2].Channels)
	}
	if p.Stages[0].AckTimeout != 15*time.Minute {
		t.Errorf("ack timeout = %v, want the 15m default", p.Stages[0].AckTimeout)
	}
}

// TestBuildPlanHonoursIntensityOverride: the escalation order reads the
// policy's re-graded ladder, not the raw channel names.
func TestBuildPlanHonoursIntensityOverride(t *testing.T) {
	d, err := NewDeliveryPolicy(nil, map[string]string{"telegram": "L0"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	p := d.BuildPlan(ModeEscalation, []string{"email", "telegram"}, time.Minute)
	if len(p.Stages) != 2 || p.Stages[0].Channels[0] != "telegram" {
		t.Errorf("stages = %+v, want the re-graded telegram channel first", p.Stages)
	}
}

// TestBuildPlanFixedDefault: ModeFixed — and any mode the switch does
// not know — is one stage with the designated set.
func TestBuildPlanFixedDefault(t *testing.T) {
	d := policy(t, false)
	for _, mode := range []Mode{ModeFixed, Mode(99)} {
		p := d.BuildPlan(mode, []string{"email", "sms"}, time.Minute)
		if p.Mode != ModeFixed || len(p.Stages) != 1 || len(p.Stages[0].Channels) != 2 {
			t.Errorf("BuildPlan(%d) = %+v, want a single fixed stage with both channels", mode, p)
		}
	}
}

// TestRunPlan walks the state machine: abort on deliver error, ack stops
// the chain at any stage, wait decides escalation vs. re-check, and the
// last stage's non-ack settles the result.
func TestRunPlan(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("deliver failed")
	deliverOK := func(context.Context, []string) error { return nil }
	deliverFail := func(context.Context, []string) error { return boom }
	never := func() bool { return false }
	waitYes := func(context.Context, time.Duration) bool { return true }
	waitNo := func(context.Context, time.Duration) bool { return false }

	t.Run("deliver error aborts at that stage", func(t *testing.T) {
		res := RunPlan(ctx, Plan{Stages: []Stage{{Channels: []string{"email"}}, {Channels: []string{"sms"}}}},
			deliverFail, never, waitYes)
		if !res.Aborted || res.LastStage != 0 || res.Acked {
			t.Errorf("result = %+v, want abort at stage 0", res)
		}
	})

	t.Run("immediate ack stops the chain", func(t *testing.T) {
		res := RunPlan(ctx, Plan{Stages: []Stage{{Channels: []string{"email"}}, {Channels: []string{"sms"}}}},
			deliverOK, func() bool { return true }, waitYes)
		if !res.Acked || res.LastStage != 0 || res.Aborted {
			t.Errorf("result = %+v, want ack at stage 0", res)
		}
	})

	t.Run("no ack walks to the last stage", func(t *testing.T) {
		res := RunPlan(ctx, Plan{Stages: []Stage{
			{Channels: []string{"email"}}, {Channels: []string{"im"}}, {Channels: []string{"sms"}},
		}}, deliverOK, never, waitYes)
		if res.Acked || res.Aborted || res.LastStage != 2 {
			t.Errorf("result = %+v, want settle at stage 2", res)
		}
	})

	t.Run("nil wait re-checks the ack before advancing", func(t *testing.T) {
		// False on the post-deliver check, true on the re-check — the ack
		// that lands during the (absent) wait still stops the chain.
		calls := 0
		res := RunPlan(ctx, Plan{Stages: []Stage{{Channels: []string{"email"}}, {Channels: []string{"sms"}}}},
			deliverOK, func() bool { calls++; return calls == 2 }, nil)
		if !res.Acked || res.LastStage != 0 {
			t.Errorf("result = %+v calls = %d, want ack at stage 0 on the re-check", res, calls)
		}
	})

	t.Run("nil wait without ack advances", func(t *testing.T) {
		res := RunPlan(ctx, Plan{Stages: []Stage{{Channels: []string{"email"}}, {Channels: []string{"sms"}}}},
			deliverOK, never, nil)
		if res.Acked || res.LastStage != 1 {
			t.Errorf("result = %+v, want settle at stage 1", res)
		}
	})

	t.Run("wait timeout re-checks the ack before advancing", func(t *testing.T) {
		calls := 0
		res := RunPlan(ctx, Plan{Stages: []Stage{{Channels: []string{"email"}}, {Channels: []string{"sms"}}}},
			deliverOK, func() bool { calls++; return calls == 2 }, waitNo)
		if !res.Acked || res.LastStage != 0 {
			t.Errorf("result = %+v calls = %d, want ack at stage 0 after the wait expired", res, calls)
		}
	})

	t.Run("wait granted advances without a re-check", func(t *testing.T) {
		calls := 0
		res := RunPlan(ctx, Plan{Stages: []Stage{{Channels: []string{"email"}}, {Channels: []string{"sms"}}}},
			deliverOK, func() bool { calls++; return false }, waitYes)
		if res.Acked || res.LastStage != 1 {
			t.Errorf("result = %+v, want settle at stage 1", res)
		}
		if calls != 2 {
			t.Errorf("ack checks = %d, want one per stage (the wait is granted, no re-check)", calls)
		}
	})

	t.Run("nil ack never stops the chain", func(t *testing.T) {
		res := RunPlan(ctx, Plan{Stages: []Stage{{Channels: []string{"email"}}}}, deliverOK, nil, nil)
		if res.Acked || res.LastStage != 0 {
			t.Errorf("result = %+v, want settle at stage 0", res)
		}
	})

	t.Run("empty plan settles with no stage", func(t *testing.T) {
		res := RunPlan(ctx, Plan{}, deliverOK, never, waitYes)
		if res.Acked || res.Aborted || res.LastStage != 0 {
			t.Errorf("result = %+v, want the zero result", res)
		}
	})
}

// TestWaitTimer: the escalation clock fires on its own and yields to a
// canceled context first.
func TestWaitTimer(t *testing.T) {
	if !WaitTimer(context.Background(), time.Millisecond) {
		t.Error("WaitTimer(fired) = false, want true")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if WaitTimer(ctx, time.Hour) {
		t.Error("WaitTimer(canceled ctx) = true, want false")
	}
}
