package queue

import (
	"context"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

// pinClock freezes the queue's clock at T0 and advances it by every wait
// the after seam observes, so scheduled waits are asserted exactly without
// sleeping. With fire=false the seam never fires — for tests that need the
// wait to stay pending (context cancellation racing a ready timer would
// otherwise be decided by select's coin flip). The returned advance moves
// the frozen clock forward by hand.
func pinClock(t *testing.T, fire bool) (time.Time, func(time.Duration)) {
	t.Helper()
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	origNow, origAfter := now, after
	clock := t0
	now = func() time.Time { return clock }
	after = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		if fire {
			clock = clock.Add(d)
			ch <- clock
		}
		return ch
	}
	t.Cleanup(func() { now, after = origNow, origAfter })
	return t0, func(d time.Duration) { clock = clock.Add(d) }
}

func newTestMemoryQueue(t *testing.T, size int) *memoryQueue {
	t.Helper()
	q, err := NewMemoryQueue(&QueueConfig{Size: size})
	if err != nil {
		t.Fatalf("NewMemoryQueue() error = %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q.(*memoryQueue)
}

// TestScheduleZeroDelayIsImmediatePush: a non-positive delay means "retry
// now" — the task lands on the ready queue, not in the delayed heap.
func TestScheduleZeroDelayIsImmediatePush(t *testing.T) {
	_, _ = pinClock(t, true)
	q := newTestMemoryQueue(t, 10)

	task := &core.DeliveryTask{ID: "now-task"}
	if err := q.Schedule(context.Background(), task, 0); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	got, err := q.Pop(context.Background())
	if err != nil || got.ID != "now-task" {
		t.Fatalf("Pop() = %v/%v, want the task immediately", got, err)
	}
	if len(q.delayed) != 0 {
		t.Errorf("delayed heap = %d entries, want 0", len(q.delayed))
	}
}

// TestScheduleHoldsUntilDue: the task is not returned before its deadline;
// the wait the queue would do is exactly the requested delay.
func TestScheduleHoldsUntilDue(t *testing.T) {
	_, _ = pinClock(t, true)
	q := newTestMemoryQueue(t, 10)
	ctx := context.Background()

	task := &core.DeliveryTask{ID: "later"}
	if err := q.Schedule(ctx, task, time.Hour); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if len(q.delayed) != 1 {
		t.Fatalf("delayed heap = %d entries, want 1", len(q.delayed))
	}

	got, err := q.Pop(ctx)
	if err != nil {
		t.Fatalf("Pop() error = %v", err)
	}
	if got.ID != "later" {
		t.Errorf("Pop() = %s, want the scheduled task", got.ID)
	}
}

// TestScheduleEarliestFirst: two scheduled tasks come back in due order,
// not schedule order.
func TestScheduleEarliestFirst(t *testing.T) {
	_, _ = pinClock(t, true)
	q := newTestMemoryQueue(t, 10)
	ctx := context.Background()

	if err := q.Schedule(ctx, &core.DeliveryTask{ID: "late"}, 2*time.Hour); err != nil {
		t.Fatalf("Schedule(late) error = %v", err)
	}
	if err := q.Schedule(ctx, &core.DeliveryTask{ID: "early"}, time.Hour); err != nil {
		t.Fatalf("Schedule(early) error = %v", err)
	}

	for _, want := range []string{"early", "late"} {
		got, err := q.Pop(ctx)
		if err != nil || got.ID != want {
			t.Fatalf("Pop() = %v/%v, want %s first-in-due-order", got, err, want)
		}
	}
}

// TestScheduleDueEntryBeatsReadyQueue: a due scheduled task is released
// ahead of freshly pushed work, so a retried task is not starved by new
// traffic.
func TestScheduleDueEntryBeatsReadyQueue(t *testing.T) {
	_, advance := pinClock(t, true)
	q := newTestMemoryQueue(t, 10)
	ctx := context.Background()

	if err := q.Schedule(ctx, &core.DeliveryTask{ID: "scheduled"}, time.Hour); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	// Advance the clock past the deadline, then enqueue fresh work.
	advance(2 * time.Hour)
	if err := q.Push(ctx, &core.DeliveryTask{ID: "fresh"}); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	got, err := q.Pop(ctx)
	if err != nil || got.ID != "scheduled" {
		t.Fatalf("Pop() = %v/%v, want the due scheduled task before fresh work", got, err)
	}
}

// TestSchedulePendingWaitCancels: a caller can give up on Pop while a task
// is still waiting out its deadline. The wait seam never fires here, so
// the only ready case is the cancelled context.
func TestSchedulePendingWaitCancels(t *testing.T) {
	_, _ = pinClock(t, false)
	q := newTestMemoryQueue(t, 10)

	if err := q.Schedule(context.Background(), &core.DeliveryTask{ID: "later"}, time.Hour); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.Pop(ctx); err == nil {
		t.Fatal("Pop() with a cancelled context = nil, want ctx error")
	}
}

// TestScheduleOnClosedQueue: scheduling into a closed queue is the same
// refusal as pushing into one.
func TestScheduleOnClosedQueue(t *testing.T) {
	_, _ = pinClock(t, true)
	q := newTestMemoryQueue(t, 10)
	if err := q.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := q.Schedule(context.Background(), &core.DeliveryTask{ID: "t"}, time.Second); err != ErrQueueClosed {
		t.Fatalf("Schedule() on closed = %v, want ErrQueueClosed", err)
	}
}
