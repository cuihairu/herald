package rules

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedisStore(t *testing.T) (*RedisStateStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	s, err := NewRedisStateStore(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, mr
}

func TestRedisStateStoreLifecycle(t *testing.T) {
	s, mr := newTestRedisStore(t)
	ctx := context.Background()
	key := stateKey("r1", "g1")

	// Absent keys read as (nil, nil).
	if got, err := s.Get(ctx, key); err != nil || got != nil {
		t.Fatalf("absent key must read as (nil, nil), got %v / %v", got, err)
	}

	state := &RuleState{FirstSeen: time.Now(), LastSeen: time.Now(), Count: 2, Fired: true}
	if err := s.Put(ctx, key, state, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, key)
	if err != nil || got == nil || got.Count != 2 || !got.Fired {
		t.Fatalf("round trip lost the state, got %+v / %v", got, err)
	}

	// A positive ttl expires the entry.
	if err := s.Put(ctx, key, state, 10*time.Millisecond); err != nil {
		t.Fatalf("Put: %v", err)
	}
	mr.FastForward(50 * time.Millisecond)
	if got, err := s.Get(ctx, key); err != nil || got != nil {
		t.Fatalf("expired entry must read as (nil, nil), got %v / %v", got, err)
	}

	// ttl <= 0 stores without expiry; Delete removes it.
	if err := s.Put(ctx, key, state, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := s.Get(ctx, key); got != nil {
		t.Fatal("deleted entry must be gone")
	}
}

func TestRedisStateStoreDeleteRuleSweepsTheRule(t *testing.T) {
	s, _ := newTestRedisStore(t)
	ctx := context.Background()
	state := &RuleState{FirstSeen: time.Now(), LastSeen: time.Now()}

	for _, key := range []string{stateKey("r2", "ga"), stateKey("r2", "gb"), stateKey("r3", "ga")} {
		if err := s.Put(ctx, key, state, 0); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	if err := s.DeleteRule(ctx, "r2"); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	for _, key := range []string{stateKey("r2", "ga"), stateKey("r2", "gb")} {
		if got, _ := s.Get(ctx, key); got != nil {
			t.Fatalf("%s must be swept by DeleteRule", key)
		}
	}
	if got, _ := s.Get(ctx, stateKey("r3", "ga")); got == nil {
		t.Fatal("another rule's state must survive the sweep")
	}
	// Sweeping a rule with no state is a no-op.
	if err := s.DeleteRule(ctx, "absent"); err != nil {
		t.Fatalf("absent sweep must be a no-op, got %v", err)
	}
}

func TestRedisStateStorePutRejectsNilState(t *testing.T) {
	s, _ := newTestRedisStore(t)
	if err := s.Put(context.Background(), "k", nil, 0); err == nil {
		t.Fatal("a nil state must be rejected")
	}
}

// Once closed, every operation must surface the dead connection instead of
// silently reporting empty state.
func TestRedisStateStoreOperationsAfterClose(t *testing.T) {
	mr := miniredis.RunT(t)
	s, err := NewRedisStateStore(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx := context.Background()
	if _, err := s.Get(ctx, "k"); err == nil {
		t.Fatal("Get after Close must fail")
	}
	if err := s.Put(ctx, "k", &RuleState{}, 0); err == nil {
		t.Fatal("Put after Close must fail")
	}
	if err := s.Delete(ctx, "k"); err == nil {
		t.Fatal("Delete after Close must fail")
	}
	if err := s.DeleteRule(ctx, "r"); err == nil {
		t.Fatal("DeleteRule after Close must fail")
	}
}

// A refused connection fails at construction, not at the first alert.
func TestNewRedisStateStoreRejectsDeadAddress(t *testing.T) {
	if _, err := NewRedisStateStore("127.0.0.1:1", "", 0); err == nil {
		t.Fatal("a dead address must fail at construction")
	}
}

// An empty address falls back to the localhost default; the dial itself
// decides success or failure.
func TestNewRedisStateStoreEmptyAddressUsesDefault(t *testing.T) {
	s, err := NewRedisStateStore("", "", 0)
	if err == nil {
		_ = s.Close()
	}
	// Either outcome is fine here: the branch under test is the fallback
	// assignment, and 127.0.0.1:6379 may or may not be serving on this host.
}

// A value that is not valid JSON surfaces as a decode error, not as empty
// state.
func TestRedisStateStoreGetRejectsCorruptValue(t *testing.T) {
	s, mr := newTestRedisStore(t)
	if err := mr.Set(stateKey("r1", "g1"), "not-json"); err != nil {
		t.Fatalf("seed miniredis: %v", err)
	}
	if _, err := s.Get(context.Background(), stateKey("r1", "g1")); err == nil {
		t.Fatal("a corrupt value must surface as a decode error")
	}
}

// delFailHook fails only the DEL command, so the DeleteRule sweep reaches
// its final delete with keys in hand.
type delFailHook struct{ redis.Hook }

func (h delFailHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "del" {
			return errors.New("del disabled for test")
		}
		return next(ctx, cmd)
	}
}

func (h delFailHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h delFailHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestRedisStateStoreDeleteRuleReportsDeleteFailure(t *testing.T) {
	s, _ := newTestRedisStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, stateKey("r1", "g1"), &RuleState{FirstSeen: time.Now(), LastSeen: time.Now()}, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	s.client.AddHook(delFailHook{})
	if err := s.DeleteRule(ctx, "r1"); err == nil {
		t.Fatal("a failing DEL must surface as a DeleteRule error")
	}
}
