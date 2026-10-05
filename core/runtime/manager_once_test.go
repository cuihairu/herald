package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/errclass"
	"github.com/cuihairu/herald/core/logstore"
	"github.com/cuihairu/herald/core/retry"
)

// TestDeliverOnceDefersRetryableFailure: the async-path attempt does not
// sleep — a retryable failure with budget left returns a Deferred, leaves
// the task retrying with NextRetryAt stamped, and keeps the log row
// pending until a later attempt settles it.
func TestDeliverOnceDefersRetryableFailure(t *testing.T) {
	m := newRetryTestManager()
	fail := &failingProvider{name: "flaky", err: errclass.New(errclass.RateLimited, errors.New("429"))}
	if err := m.RegisterProvider("flaky", fail); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{ID: "t-defer", Provider: "flaky"}
	err := m.DeliverOnce(context.Background(), task)
	var deferred *retry.Deferred
	if !errors.As(err, &deferred) {
		t.Fatalf("DeliverOnce = %v, want a *Deferred", err)
	}
	if task.Status != core.StatusRetrying {
		t.Errorf("task status = %q, want retrying", task.Status)
	}
	if task.RetryCount != 1 {
		t.Errorf("task retry = %d, want 1", task.RetryCount)
	}
	if task.NextRetryAt == nil || !task.NextRetryAt.After(time.Now()) {
		t.Errorf("NextRetryAt = %v, want a future stamp", task.NextRetryAt)
	}
	if task.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want the policy total 3", task.MaxAttempts)
	}

	// Nothing settled: exactly one log row, still pending.
	logs := m.GetLogs(0, 10, nil)
	if len(logs) != 1 {
		t.Fatalf("log rows = %d, want 1 for the deferred task", len(logs))
	}
	if logs[0].Status != "pending" {
		t.Errorf("log status = %q, want pending until the final attempt", logs[0].Status)
	}

	// The next attempt succeeds: same row settles to success, the stamp
	// clears, and the retry count stays (attempts = 2 of 3).
	fail.err = nil
	if err := m.DeliverOnce(context.Background(), task); err != nil {
		t.Fatalf("second DeliverOnce = %v, want delivered", err)
	}
	if task.Status != core.StatusDelivered {
		t.Errorf("task status = %q, want delivered", task.Status)
	}
	if task.NextRetryAt != nil {
		t.Errorf("NextRetryAt = %v, want cleared once the attempt runs", task.NextRetryAt)
	}
	if task.RetryCount != 1 {
		t.Errorf("task retry = %d, want 1 after recovery", task.RetryCount)
	}
	logs = m.GetLogs(0, 10, nil)
	if len(logs) != 1 || logs[0].Status != "success" {
		t.Fatalf("logs = %+v, want the single row settled to success", logs)
	}
}

// TestDeliverOnceRejectsBadInput: a nil task is rejected outright, and a
// task whose provider is not registered fails in the prologue — before any
// attempt, with no log row and no settlement.
func TestDeliverOnceRejectsBadInput(t *testing.T) {
	m := NewManager(100)

	if err := m.DeliverOnce(context.Background(), nil); err == nil {
		t.Fatal("DeliverOnce(nil) = nil, want an error")
	}

	task := &core.DeliveryTask{ID: "t-missing", Provider: "ghost"}
	if err := m.DeliverOnce(context.Background(), task); err == nil {
		t.Fatal("DeliverOnce = nil, want the provider lookup error")
	}
	if task.Status == core.StatusFailed || task.Status == core.StatusDead {
		t.Errorf("task settled to %q on a lookup error, want no settlement", task.Status)
	}
	if logs := m.GetLogs(0, 10, nil); len(logs) != 0 {
		t.Errorf("log rows = %d, want none before any attempt", len(logs))
	}
}

// TestDeliverOnceSettlesTerminalFailures: a non-retryable error is failed,
// a spent budget is dead, and without a retryer even a retryable class
// fails outright.
func TestDeliverOnceSettlesTerminalFailures(t *testing.T) {
	cases := []struct {
		name    string
		retryer *retry.Config
		setup   func(*core.DeliveryTask)
		err     error
		want    core.DeliveryStatus
	}{
		{
			name:    "permanent fails immediately",
			retryer: &retry.Config{Max: 2, Backoff: "fixed", InitialDelay: time.Millisecond},
			err:     errclass.New(errclass.Permanent, errors.New("template rejected")),
			want:    core.StatusFailed,
		},
		{
			name:    "spent budget is dead",
			retryer: &retry.Config{Max: 1, Backoff: "fixed", InitialDelay: time.Millisecond},
			setup:   func(task *core.DeliveryTask) { task.RetryCount = 1 },
			err:     errclass.New(errclass.Temporary, errors.New("503")),
			want:    core.StatusDead,
		},
		{
			name: "no retryer: even retryable classes fail",
			err:  errclass.New(errclass.RateLimited, errors.New("429")),
			want: core.StatusFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m *Manager
			if tc.retryer != nil {
				m = NewManager(100, tc.retryer)
			} else {
				m = NewManager(100)
			}
			if err := m.RegisterProvider("bad", &failingProvider{name: "bad", err: tc.err}); err != nil {
				t.Fatal(err)
			}

			task := &core.DeliveryTask{ID: "t-term", Provider: "bad"}
			if tc.setup != nil {
				tc.setup(task)
			}
			if err := m.DeliverOnce(context.Background(), task); err == nil {
				t.Fatal("DeliverOnce = nil, want the failure")
			}
			if task.Status != tc.want {
				t.Errorf("task status = %q, want %q", task.Status, tc.want)
			}
			logs := m.GetLogs(0, 10, &logstore.Filter{Status: "failed"})
			if len(logs) != 1 {
				t.Fatalf("failed log rows = %d, want 1", len(logs))
			}
		})
	}
}
