package rules

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryStateStorePutGetDelete(t *testing.T) {
	store := NewMemoryStateStore()
	ctx := context.Background()

	// Absent key reads as (nil, nil).
	state, err := store.Get(ctx, "k1")
	if err != nil || state != nil {
		t.Fatalf("absent key: got %+v err %v, want nil nil", state, err)
	}

	want := &RuleState{FirstSeen: time.Unix(1, 0), LastSeen: time.Unix(2, 0), Count: 7, Fired: true}
	if err := store.Put(ctx, "k1", want, 0); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	got, err := store.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got == nil || got.Count != 7 || !got.Fired || !got.FirstSeen.Equal(want.FirstSeen) {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, want)
	}

	// Put replaces.
	replacement := &RuleState{Count: 1}
	if err := store.Put(ctx, "k1", replacement, 0); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	got, _ = store.Get(ctx, "k1")
	if got == nil || got.Count != 1 {
		t.Errorf("expected replaced state, got %+v", got)
	}

	// Delete removes; deleting again is a no-op.
	if err := store.Delete(ctx, "k1"); err != nil {
		t.Fatalf("Delete error = %v", err)
	}
	if state, _ := store.Get(ctx, "k1"); state != nil {
		t.Errorf("expected state gone after delete, got %+v", state)
	}
	if err := store.Delete(ctx, "k1"); err != nil {
		t.Errorf("repeated Delete error = %v", err)
	}
}

func TestMemoryStateStorePutNilRejected(t *testing.T) {
	store := NewMemoryStateStore()
	if err := store.Put(context.Background(), "k", nil, 0); err == nil {
		t.Error("expected error putting nil state")
	}
}

func TestMemoryStateStoreTTLExpiry(t *testing.T) {
	store := NewMemoryStateStore()
	ctx := context.Background()

	if err := store.Put(ctx, "k-ttl", &RuleState{Count: 1}, 10*time.Millisecond); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	if state, _ := store.Get(ctx, "k-ttl"); state == nil {
		t.Fatal("expected live state before expiry")
	}
	time.Sleep(15 * time.Millisecond)
	if state, err := store.Get(ctx, "k-ttl"); err != nil || state != nil {
		t.Errorf("expected expired state to read as absent, got %+v err %v", state, err)
	}
}

func TestMemoryStateStoreNoTTLSurvives(t *testing.T) {
	store := NewMemoryStateStore()
	ctx := context.Background()

	// ttl <= 0 means no expiry; a negative ttl must not expire immediately.
	if err := store.Put(ctx, "k-forever", &RuleState{Count: 2}, -time.Hour); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if state, _ := store.Get(ctx, "k-forever"); state == nil {
		t.Error("expected non-expiring state to survive")
	}
}

func TestMemoryStateStoreDeleteRuleByPrefix(t *testing.T) {
	store := NewMemoryStateStore()
	ctx := context.Background()

	// r1 owns several group keys, including a prefix collision case: the
	// rule id "r1" must not match "r10".
	for _, key := range []string{stateKey("r1", "aaa"), stateKey("r1", "bbb"), stateKey("r10", "aaa"), stateKey("r2", "aaa")} {
		if err := store.Put(ctx, key, &RuleState{Count: 1}, 0); err != nil {
			t.Fatalf("Put error = %v", err)
		}
	}
	if err := store.DeleteRule(ctx, "r1"); err != nil {
		t.Fatalf("DeleteRule error = %v", err)
	}
	for _, key := range []string{stateKey("r10", "aaa"), stateKey("r2", "aaa")} {
		if state, _ := store.Get(ctx, key); state == nil {
			t.Errorf("unrelated key %s was deleted", key)
		}
	}
	if state, _ := store.Get(ctx, stateKey("r1", "aaa")); state != nil {
		t.Error("expected r1 state to be deleted")
	}
}

func TestMemoryStateStoreConcurrentAccess(t *testing.T) {
	store := NewMemoryStateStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k-%d", i%4)
			for j := 0; j < 50; j++ {
				_ = store.Put(ctx, key, &RuleState{Count: uint64(j)}, time.Minute)
				_, _ = store.Get(ctx, key)
				_ = store.Delete(ctx, key)
			}
		}(i)
	}
	wg.Wait()
}

// TestStateStoreContract pins the interface behavior MemoryStateStore must
// keep so the Redis backend (covered by integration tests) can be swapped
// in without semantic drift.
func TestStateStoreContract(t *testing.T) {
	var store StateStore = NewMemoryStateStore()
	ctx := context.Background()

	if state, err := store.Get(ctx, "missing"); state != nil || err != nil {
		t.Fatalf("missing key: got %+v err %v, want nil nil", state, err)
	}
	if err := store.Put(ctx, "c", &RuleState{Count: 3}, time.Hour); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	if err := store.DeleteRule(ctx, "other"); err != nil {
		t.Fatalf("DeleteRule error = %v", err)
	}
	if state, _ := store.Get(ctx, "c"); state == nil {
		t.Error("DeleteRule for another rule must not delete existing state")
	}
	if err := store.Close(); err != nil {
		t.Errorf("Close error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("repeated Close error = %v", err)
	}
}
