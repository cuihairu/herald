package audience

import (
	"context"
	"time"
)

type Plan struct {
	Mode   Mode
	Stages []Stage
}

type Stage struct {
	Channels   []string
	AckTimeout time.Duration
}

type Result struct {
	Acked     bool
	Aborted   bool
	LastStage int
}

type DeliverFunc func(ctx context.Context, channels []string) error
type AckFunc func() bool
type WaitFunc func(ctx context.Context, d time.Duration) bool

func (d *DeliveryPolicy) BuildPlan(mode Mode, channels []string, ackTimeout time.Duration) Plan {
	if ackTimeout <= 0 {
		ackTimeout = 15 * time.Minute
	}
	switch mode {
	case ModeParallel:
		return Plan{Mode: mode, Stages: []Stage{{Channels: channels, AckTimeout: ackTimeout}}}
	case ModeEscalation:
		ordered := d.sortByIntensity(channels)
		stages := make([]Stage, 0, len(ordered))
		for i, channel := range ordered {
			if i > 0 && d.IntensityOf(channel) == d.IntensityOf(ordered[i-1]) {
				stages[len(stages)-1].Channels = append(stages[len(stages)-1].Channels, channel)
			} else {
				stages = append(stages, Stage{Channels: []string{channel}, AckTimeout: ackTimeout})
			}
		}
		return Plan{Mode: mode, Stages: stages}
	default: // ModeFixed and any future mode default to the designated set
		return Plan{Mode: ModeFixed, Stages: []Stage{{Channels: channels, AckTimeout: ackTimeout}}}
	}
}

func RunPlan(ctx context.Context, plan Plan, deliver DeliverFunc, acked AckFunc, wait WaitFunc) Result {
	for i, stage := range plan.Stages {
		if err := deliver(ctx, stage.Channels); err != nil {
			return Result{Aborted: true, LastStage: i}
		}
		if acked != nil && acked() {
			return Result{Acked: true, LastStage: i}
		}
		if i == len(plan.Stages)-1 {
			return Result{LastStage: i}
		}
		if wait == nil || !wait(ctx, stage.AckTimeout) {
			if acked != nil && acked() {
				return Result{Acked: true, LastStage: i}
			}
			continue
		}
	}
	return Result{}
}

func WaitTimer(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
