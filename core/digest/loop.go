package digest

import (
	"context"
	"time"
)

// FlipLoop drives the windows: every tick it (re)takes the leader lease
// — when a lock is wired; nil means single-instance and always flips —
// and flushes each due batch through flush. It returns when ctx is
// done; callers release the lock themselves on shutdown if they need
// the handover to be immediate rather than lease-bound. The interval
// should sit well under the schedule granularity; a missed or skipped
// tick self-heals because Due() flips everything whose time has passed.
func FlipLoop(ctx context.Context, agg *Aggregator, lock *LeaderLock, interval time.Duration, flush func(Batch)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if lock != nil && !lock.Acquire(ctx) && !lock.Renew(ctx) {
				continue
			}
			for _, b := range agg.Due() {
				flush(b)
			}
		}
	}
}
