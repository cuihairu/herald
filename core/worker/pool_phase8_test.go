package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/service"
	"github.com/cuihairu/herald/core/template"
)

// countingProvider counts Deliver calls and can fail the first n attempts
// with a retryable error — the §31 "feishu" that must be allowed to retry
// without dragging its fan-out siblings down.
type countingProvider struct {
	name      string
	failFirst int
	failErr   error
	calls     int
}

func (p *countingProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	p.calls++
	if p.calls <= p.failFirst {
		return p.failErr
	}
	return nil
}

func (p *countingProvider) Name() string { return p.name }

func (p *countingProvider) Type() string { return "counting" }

func (p *countingProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: p.name, Status: "available"}
}

// TestPoolMultiProviderFanOutIsolation settles design §31 "Multi
// Provider": one notification fans out to two providers with independent
// fates — telegram delivers on the first attempt exactly once (a failing
// sibling must not duplicate it), feishu fails its first attempt with a
// retryable error and lands on the retry: delivered, not dead. The full
// chain is real: service routing → task plan → queue → worker → provider.
func TestPoolMultiProviderFanOutIsolation(t *testing.T) {
	q := newMemQueue(false)
	m := runtime.NewManager(10, &retry.Config{
		Max:          2,
		Backoff:      "fixed",
		InitialDelay: time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
	})
	telegram := &countingProvider{name: "telegram"}
	feishu := &countingProvider{
		name:      "feishu",
		failFirst: 1,
		failErr:   retry.NewRetryableError(errors.New("feishu throttled")),
	}
	for name, p := range map[string]*countingProvider{"telegram": telegram, "feishu": feishu} {
		if err := m.RegisterProvider(name, p, true); err != nil {
			t.Fatal(err)
		}
	}
	runPool(t, q, m)

	svc := service.NewNotificationService(
		template.NewManager(), route.NewRouter(&route.Config{}), m, nil, q,
	)
	res, err := svc.Process(context.Background(), &core.Notification{
		Type:     "alert",
		Level:    "error",
		Content:  &core.DirectContent{Title: "phase8", Body: "multi provider"},
		Channels: []string{"telegram", "feishu"},
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(res.Accepted) != 2 || res.Accepted[0] != "telegram" || res.Accepted[1] != "feishu" {
		t.Fatalf("accepted = %v, want [telegram feishu] (failed=%v)", res.Accepted, res.Failed)
	}
	if len(res.TaskIDs) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(res.TaskIDs))
	}

	// Both settle delivered: feishu's retry happens inside the worker's
	// single Pop, so both tasks end up acked.
	settleTasks(t, q, res.TaskIDs, 2, 0)

	for _, id := range res.TaskIDs {
		task := q.byID[id]
		if task == nil {
			t.Fatalf("task %s not found in queue", id)
		}
		switch task.Provider {
		case "telegram":
			if telegram.calls != 1 {
				t.Errorf("telegram calls = %d, want exactly 1 (no duplication)", telegram.calls)
			}
			if task.Status != core.StatusDelivered || task.RetryCount != 0 {
				t.Errorf("telegram task = %+v, want delivered with no retries", task)
			}
			if task.LastError != "" {
				t.Errorf("telegram LastError = %q, want empty", task.LastError)
			}
		case "feishu":
			if feishu.calls != 2 {
				t.Errorf("feishu calls = %d, want 2 (one retry after the throttled attempt)", feishu.calls)
			}
			if task.Status != core.StatusDelivered || task.RetryCount != 1 {
				t.Errorf("feishu task = %+v, want delivered after 1 retry", task)
			}
			if task.LastError != "" {
				t.Errorf("feishu LastError = %q, want empty (cleared on delivery)", task.LastError)
			}
		default:
			t.Errorf("unexpected task provider %q", task.Provider)
		}
		// Each task logged its own episode, in the wire vocabulary.
		if log := m.GetLogByID(id); log == nil || log.Status != "success" {
			t.Errorf("task %s log = %+v, want success", id, log)
		}
	}
}