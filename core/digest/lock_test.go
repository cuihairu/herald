package digest

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestLock(t *testing.T, key string, ttl time.Duration) (*LeaderLock, *LeaderLock, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewLeaderLock(client, key, ttl), NewLeaderLock(client, key, ttl), mr
}

func TestLeaderLockExclusiveTakeAndRelease(t *testing.T) {
	ctx := context.Background()
	leader, other, _ := newTestLock(t, "test:leader", time.Minute)

	if !leader.Acquire(ctx) {
		t.Fatal("first Acquire() = false, want the lease taken")
	}
	if other.Acquire(ctx) {
		t.Fatal("second Acquire() = true, want the lease exclusive")
	}
	if !leader.Renew(ctx) {
		t.Error("holder Renew() = false, want the lease extended")
	}
	if other.Renew(ctx) {
		t.Error("non-holder Renew() = true, want it refused")
	}
	leader.Release(ctx)
	if !other.Acquire(ctx) {
		t.Fatal("Acquire() after release = false, want the lease free")
	}
	// A stale leader cannot delete the new holder's lease.
	leader.Release(ctx)
	if !other.Renew(ctx) {
		t.Error("new holder Renew() = false after stale Release(), want the lease intact")
	}
}

func TestLeaderLockLeaseExpiryFreesTheKey(t *testing.T) {
	ctx := context.Background()
	leader, other, mr := newTestLock(t, "test:leader", time.Second)

	if !leader.Acquire(ctx) {
		t.Fatal("Acquire() = false")
	}
	mr.FastForward(2 * time.Second)
	if !other.Acquire(ctx) {
		t.Fatal("Acquire() after lease lapse = false, want it taken over")
	}
	if leader.Renew(ctx) {
		t.Error("expired holder Renew() = true, want it refused")
	}
}

func TestFlipLoopFlushesDueWindowsAndStops(t *testing.T) {
	loc := mustLoc(t, "UTC")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)
	opened := at(2026, time.October, 7, 9, 1, loc) // just after the day's flip
	agg.SetClock(func() time.Time { return opened })
	agg.Add(ev("alice", "bills", "e1", opened), Daily) // window flips Oct 8 09:01

	// The clock now sits past the flip: the loop's first tick flushes.
	due := at(2026, time.October, 8, 9, 2, loc)
	agg.SetClock(func() time.Time { return due })

	flushed := make(chan Batch, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go FlipLoop(ctx, agg, nil, 5*time.Millisecond, func(b Batch) {
		flushed <- b
	})

	select {
	case b := <-flushed:
		if b.Key.AudienceID != "alice" || len(b.Events) != 1 {
			t.Errorf("flushed batch = %+v, want alice/e1", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FlipLoop never flushed the due window")
	}

	// Cancel stops the loop: the next window (e2, flips Oct 9) never
	// flushes after the cancel.
	cancel()
	agg.Add(ev("alice", "bills", "e2", due), Daily)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case <-flushed:
			t.Fatal("flush after cancel")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestFlipLoopHonorsTheLease(t *testing.T) {
	loc := mustLoc(t, "UTC")
	daily, _ := ParseDaily("09:00", loc)
	weekly, _ := ParseWeekly("Mon 09:00", loc)
	agg := NewAggregator(daily, weekly)
	opened := at(2026, time.October, 7, 9, 1, loc)
	agg.SetClock(func() time.Time { return opened })
	agg.Add(ev("alice", "bills", "e1", opened), Daily)
	// The window is due: without the lease the loop would flush at once.
	agg.SetClock(func() time.Time { return at(2026, time.October, 8, 9, 2, loc) })

	leader, other, _ := newTestLock(t, "test:leader", time.Minute)

	flushed := make(chan Batch, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The other instance holds the lease; this loop must skip its ticks.
	if !leader.Acquire(ctx) {
		t.Fatal("lease setup failed")
	}
	go FlipLoop(ctx, agg, other, 5*time.Millisecond, func(b Batch) { flushed <- b })

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case b := <-flushed:
			t.Fatalf("leased-out loop flushed: %+v", b)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Hand the lease over: now the loop flushes.
	leader.Release(ctx)
	select {
	case b := <-flushed:
		if b.Key.AudienceID != "alice" {
			t.Errorf("flushed batch = %+v", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FlipLoop never flushed after taking the lease")
	}
}
