package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

// injectDelayed plants a member in the delayed set with a score already in
// the past, standing in for a task whose Schedule wait has elapsed.
func injectDelayed(t *testing.T, fr *fakeRedis, key, member string, due time.Time) {
	t.Helper()
	fr.mu.Lock()
	defer fr.mu.Unlock()
	set := fr.zsets[key]
	i := len(set)
	for i > 0 && set[i-1].score > float64(due.UnixNano()) {
		i--
	}
	set = append(set, fakeZEntry{})
	copy(set[i+1:], set[i:])
	set[i] = fakeZEntry{score: float64(due.UnixNano()), member: member}
	fr.zsets[key] = set
}

// TestScheduleRedisHoldsTask: Schedule stores the serialized task in the
// delayed set scored by its due time; Pop before the deadline does not
// move it into the stream.
func TestScheduleRedisHoldsTask(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)
	ctx := context.Background()

	task := &core.DeliveryTask{ID: "later", Provider: "p"}
	if err := q.Schedule(ctx, task, time.Hour); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	fr.mu.Lock()
	set := fr.zsets["herald:tasks:delayed"]
	stored := len(set) == 1
	var decoded core.DeliveryTask
	if stored {
		stored = json.Unmarshal([]byte(set[0].member), &decoded) == nil
	}
	fr.mu.Unlock()
	if !stored || decoded.ID != "later" {
		t.Fatalf("delayed set = %v, want the task stored and decodable", set)
	}

	// Not due yet: Pop finds nothing (stream untouched, entry stays).
	if _, err := q.Pop(ctx); err == nil {
		t.Fatal("Pop() before the deadline = nil, want no messages")
	}
	fr.mu.Lock()
	n := len(fr.entries)
	left := len(fr.zsets["herald:tasks:delayed"])
	fr.mu.Unlock()
	if n != 0 || left != 1 {
		t.Fatalf("stream=%d delayed=%d, want 0/1 (task still held)", n, left)
	}
}

// TestScheduleRedisMovesDueEntry: once the deadline has passed, Pop moves
// the task into the stream and hands it out.
func TestScheduleRedisMovesDueEntry(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)
	ctx := context.Background()

	data, err := json.Marshal(&core.DeliveryTask{ID: "due", Provider: "p"})
	if err != nil {
		t.Fatal(err)
	}
	injectDelayed(t, fr, "herald:tasks:delayed", string(data), time.Now().Add(-time.Hour))

	got, err := q.Pop(ctx)
	if err != nil || got.ID != "due" {
		t.Fatalf("Pop() = %v/%v, want the due task", got, err)
	}
	fr.mu.Lock()
	left := len(fr.zsets["herald:tasks:delayed"])
	fr.mu.Unlock()
	if left != 0 {
		t.Fatalf("delayed set not drained (%d left)", left)
	}
}

// TestScheduleRedisZeroDelayPushes: "retry now" goes straight onto the
// stream — nothing sits in the delayed set.
func TestScheduleRedisZeroDelayPushes(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)

	if err := q.Schedule(context.Background(), &core.DeliveryTask{ID: "now", Provider: "p"}, 0); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	fr.mu.Lock()
	delayed := len(fr.zsets["herald:tasks:delayed"])
	stream := len(fr.entries)
	fr.mu.Unlock()
	if delayed != 0 || stream != 1 {
		t.Fatalf("delayed=%d stream=%d, want 0/1", delayed, stream)
	}
}

// TestScheduleRedisClosed: a closed queue refuses scheduling like a push.
func TestScheduleRedisClosed(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)
	if err := q.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	err := q.Schedule(context.Background(), &core.DeliveryTask{ID: "t"}, time.Second)
	if !errors.Is(err, ErrQueueClosed) && err == nil {
		t.Fatalf("Schedule() on closed = %v, want ErrQueueClosed", err)
	}
}

// TestScheduleRedisLostRace: when another process wins the ZREM the entry
// is theirs to move — this queue neither delivers it nor drops it.
func TestScheduleRedisLostRace(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)

	data, err := json.Marshal(&core.DeliveryTask{ID: "contested", Provider: "p"})
	if err != nil {
		t.Fatal(err)
	}
	injectDelayed(t, fr, "herald:tasks:delayed", string(data), time.Now().Add(-time.Minute))
	fr.stealZRem = true

	// The entry is reported taken, so this Pop proceeds without it.
	if _, err := q.Pop(context.Background()); err == nil {
		t.Fatal("Pop() = nil, want no messages (entry lost to the other process)")
	}
	fr.mu.Lock()
	left := len(fr.zsets["herald:tasks:delayed"])
	stream := len(fr.entries)
	fr.mu.Unlock()
	if left != 1 || stream != 0 {
		t.Fatalf("delayed=%d stream=%d, want 1/0 (entry untouched, not duplicated)", left, stream)
	}
}

// TestScheduleRedisPoisonMember: an entry whose payload no longer decodes
// is dropped instead of being re-read on every Pop forever.
func TestScheduleRedisPoisonMember(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)

	injectDelayed(t, fr, "herald:tasks:delayed", "{not json", time.Now().Add(-time.Minute))

	// The poison entry is dropped, so Pop falls through to an empty stream
	// ("no messages available") instead of surfacing the undecodable one.
	if _, err := q.Pop(context.Background()); err != nil && err.Error() != "no messages available" {
		t.Fatalf("Pop() error = %v, want no-messages after the poison entry is skipped", err)
	}
	fr.mu.Lock()
	left := len(fr.zsets["herald:tasks:delayed"])
	fr.mu.Unlock()
	if left != 0 {
		t.Fatalf("poison entry still in the delayed set (%d left)", left)
	}
}

// TestScheduleRedisBrokenLookup: a failing sorted-set read surfaces as a
// Pop error rather than a silently skipped due task.
func TestScheduleRedisBrokenLookup(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)
	fr.failZRange = true

	if _, err := q.Pop(context.Background()); err == nil {
		t.Fatal("Pop() = nil, want the ZRANGEBYSCORE failure")
	}
}

// TestScheduleRedisMarshalFailure: a task that cannot be serialized is
// refused by Schedule instead of corrupting the delayed set.
func TestScheduleRedisMarshalFailure(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)

	task := &core.DeliveryTask{
		ID:       "unserializable",
		Provider: "p",
		Payload: core.DeliveryPayload{
			Raw: map[string]any{"chan": make(chan int)},
		},
	}
	if err := q.Schedule(context.Background(), task, time.Hour); err == nil {
		t.Fatal("Schedule() = nil, want the marshal error")
	}
	fr.mu.Lock()
	left := len(fr.zsets["herald:tasks:delayed"])
	fr.mu.Unlock()
	if left != 0 {
		t.Fatalf("delayed set = %d entries, want none", left)
	}
}

// TestScheduleRedisZRemFailure: a failing ZREM surfaces as a Pop error —
// moving a due task is not silently skipped mid-move.
func TestScheduleRedisZRemFailure(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)

	data, err := json.Marshal(&core.DeliveryTask{ID: "due", Provider: "p"})
	if err != nil {
		t.Fatal(err)
	}
	injectDelayed(t, fr, "herald:tasks:delayed", string(data), time.Now().Add(-time.Minute))
	fr.failZRem = true

	if _, err := q.Pop(context.Background()); err == nil {
		t.Fatal("Pop() = nil, want the ZREM failure")
	}
}

// TestScheduleRedisMoveFailure: if the stream write for a moved due task
// fails, Pop surfaces the error — the entry left the set but the task must
// not be dropped without a trace.
func TestScheduleRedisMoveFailure(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)

	data, err := json.Marshal(&core.DeliveryTask{ID: "due", Provider: "p"})
	if err != nil {
		t.Fatal(err)
	}
	injectDelayed(t, fr, "herald:tasks:delayed", string(data), time.Now().Add(-time.Minute))
	fr.setFailFlags(false, true, false, false)

	if _, err := q.Pop(context.Background()); err == nil {
		t.Fatal("Pop() = nil, want the stream-write failure")
	}
}

// TestScheduleRedisBrokenRead: a hard XREADGROUP failure (not the empty
// reply) reaches the caller as a Pop error.
func TestScheduleRedisBrokenRead(t *testing.T) {
	fr := newFakeRedis(t)
	q := newTestRedisQueue(t, fr).(*redisQueue)
	fr.failXRead = true

	if _, err := q.Pop(context.Background()); err == nil {
		t.Fatal("Pop() = nil, want the XREADGROUP failure")
	}
}
