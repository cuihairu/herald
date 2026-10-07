package digest

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// LeaderLock is the multi-instance guard for the digest flip loop
// (关系详设 §10 定时器, redis 锁选主): one SET NX PX lease keyed in
// redis. A non-leader instance skips its flip ticks, so each window
// flips exactly once across the fleet. Renewal and release compare the
// holder id, so a lagging instance cannot cut a live lease; the
// residual GET-then-write race is tolerable here — its worst case is
// one skipped or duplicated flip check, and Due() flips everything
// whose time has passed, so the next tick self-heals.
type LeaderLock struct {
	client *redis.Client
	key    string
	ttl    time.Duration
	id     string
}

// NewLeaderLock builds a lease over one redis key. The lease id is a
// fresh UUID per lock instance.
func NewLeaderLock(client *redis.Client, key string, ttl time.Duration) *LeaderLock {
	return &LeaderLock{client: client, key: key, ttl: ttl, id: uuid.New().String()}
}

// Acquire takes the lease if free. It is also the renewal path: an
// existing holder's SetNX misses and Renew refreshes instead, so the
// loop can call Acquire every tick without distinguishing first take
// from renewal.
func (l *LeaderLock) Acquire(ctx context.Context) bool {
	ok, err := l.client.SetNX(ctx, l.key, l.id, l.ttl).Result()
	return err == nil && ok
}

// Renew extends the lease, but only when this instance still holds it.
func (l *LeaderLock) Renew(ctx context.Context) bool {
	id, err := l.client.Get(ctx, l.key).Result()
	if err != nil || id != l.id {
		return false
	}
	return l.client.PExpire(ctx, l.key, l.ttl).Err() == nil
}

// Release drops the lease, but only when this instance holds it.
func (l *LeaderLock) Release(ctx context.Context) {
	id, err := l.client.Get(ctx, l.key).Result()
	if err != nil || id != l.id {
		return
	}
	_ = l.client.Del(ctx, l.key).Err()
}
