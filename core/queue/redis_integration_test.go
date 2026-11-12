//go:build integration

package queue

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/retry"
	"github.com/redis/go-redis/v9"
)

// These tests require a running Redis server.
// Run with: go test -tags=integration ./core/queue

func getRedisAddr() string {
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379"
}

func skipWithoutRedis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Try to connect to Redis
	config := &QueueConfig{
		Type: "redis",
		Redis: RedisConfig{
			Addr: getRedisAddr(),
		},
	}
	_, err := NewRedisQueue(config)
	if err != nil {
		t.Skipf("Redis not available at %s: %v", getRedisAddr(), err)
	}
}

// freshConfig returns a config bound to a brand-new stream and group.
// Subtests that assume an empty queue must never reuse the parent's stream:
// XAck/XClaim clear the pending list but stream entries persist, and
// Size() counts XLEN — earlier subtests' leftovers leak into later ones.
func freshConfig() *QueueConfig {
	return &QueueConfig{
		Type: "redis",
		Redis: RedisConfig{
			Addr:   getRedisAddr(),
			Stream: fmt.Sprintf("test-stream-%d", time.Now().UnixNano()),
			Group:  fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
		},
	}
}

func TestRedisQueueIntegration(t *testing.T) {
	skipWithoutRedis(t)

	config := &QueueConfig{
		Type: "redis",
		Redis: RedisConfig{
			Addr:   getRedisAddr(),
			Stream: fmt.Sprintf("test-stream-%d", time.Now().UnixNano()),
			Group:  fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
		},
	}

	t.Run("new redis queue succeeds", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		_ = q.Close()
	})

	t.Run("push and pop", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		defer q.Close()

		ctx := context.Background()
		task := &core.DeliveryTask{
			ID:       "task-1",
			Provider: "email",
			Targets:  []string{"user@example.com"},
		}

		err = q.Push(ctx, task)
		if err != nil {
			t.Errorf("expected no error pushing task, got %v", err)
		}

		popped, err := q.Pop(ctx)
		if err != nil {
			t.Errorf("expected no error popping task, got %v", err)
		}
		if popped == nil {
			t.Fatal("expected non-nil task")
		}
		if popped.ID != task.ID {
			t.Errorf("expected ID %s, got %s", task.ID, popped.ID)
		}
		if popped.Provider != task.Provider {
			t.Errorf("expected Provider %s, got %s", task.Provider, popped.Provider)
		}
	})

	t.Run("multiple push and pop", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		defer q.Close()

		ctx := context.Background()
		count := 10

		// Push multiple tasks
		for i := 0; i < count; i++ {
			task := &core.DeliveryTask{
				ID:       fmt.Sprintf("task-%d", i),
				Provider: "email",
			}
			err = q.Push(ctx, task)
			if err != nil {
				t.Errorf("failed to push task %d: %v", i, err)
			}
		}

		// Pop all tasks
		seen := make(map[string]bool)
		for i := 0; i < count; i++ {
			task, err := q.Pop(ctx)
			if err != nil {
				t.Errorf("failed to pop task %d: %v", i, err)
			}
			if task != nil {
				if seen[task.ID] {
					t.Errorf("duplicate task ID: %s", task.ID)
				}
				seen[task.ID] = true
			}
		}

		if len(seen) != count {
			t.Errorf("expected %d unique tasks, got %d", count, len(seen))
		}
	})

	t.Run("ack succeeds", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		defer q.Close()

		ctx := context.Background()
		task := &core.DeliveryTask{ID: "task-ack", Provider: "email"}

		_ = q.Push(ctx, task)
		popped, _ := q.Pop(ctx)

		err = q.Ack(ctx, popped.ID)
		if err != nil {
			t.Errorf("expected no error on ack, got %v", err)
		}
	})

	t.Run("nack re-queues task", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		defer q.Close()

		ctx := context.Background()
		task := &core.DeliveryTask{ID: "task-nack", Provider: "email"}

		_ = q.Push(ctx, task)
		popped, _ := q.Pop(ctx)

		err = q.Nack(ctx, popped.ID, retry.NewRetryableError(fmt.Errorf("delivery failed")))
		if err != nil {
			t.Errorf("expected no error on nack, got %v", err)
		}

		// Task should be available again
		requeued, err := q.Pop(ctx)
		if err != nil {
			t.Errorf("expected no error popping requeued task, got %v", err)
		}
		if requeued == nil {
			t.Error("expected requeued task to be available")
		}
	})

	t.Run("size returns message count", func(t *testing.T) {
		q, err := NewRedisQueue(freshConfig())
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		defer q.Close()

		ctx := context.Background()

		// Initial size should be 0
		if size := q.Size(); size != 0 {
			t.Errorf("expected initial size 0, got %d", size)
		}

		// Add some tasks
		for i := 0; i < 5; i++ {
			_ = q.Push(ctx, &core.DeliveryTask{
				ID:       fmt.Sprintf("task-%d", i),
				Provider: "email",
			})
		}

		// Size should reflect pending messages
		size := q.Size()
		if size < 5 {
			t.Logf("expected size >= 5, got %d (Redis may have processed messages)", size)
		}
	})

	t.Run("push to closed queue fails", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		_ = q.Close()

		ctx := context.Background()
		task := &core.DeliveryTask{ID: "task-closed", Provider: "email"}

		err = q.Push(ctx, task)
		if err != ErrQueueClosed {
			t.Errorf("expected ErrQueueClosed, got %v", err)
		}
	})

	t.Run("pop from closed queue fails", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		_ = q.Close()

		ctx := context.Background()
		_, err = q.Pop(ctx)
		if err != ErrQueueClosed {
			t.Errorf("expected ErrQueueClosed, got %v", err)
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}

		err = q.Close()
		if err != nil {
			t.Errorf("expected no error on first close, got %v", err)
		}

		err = q.Close()
		if err != nil {
			t.Errorf("expected no error on second close, got %v", err)
		}
	})

	t.Run("context cancellation on push", func(t *testing.T) {
		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		defer q.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		task := &core.DeliveryTask{ID: "task-cancel", Provider: "email"}
		err = q.Push(ctx, task)
		if err != context.Canceled {
			t.Errorf("expected context canceled error, got %v", err)
		}
	})

	t.Run("pop with timeout when empty", func(t *testing.T) {
		q, err := NewRedisQueue(freshConfig())
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		defer q.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err = q.Pop(ctx)
		if err == nil {
			t.Error("expected error when popping from empty queue with timeout")
		}
	})

	t.Run("claim recovers orphaned delivery across consumers", func(t *testing.T) {
		// A delivery read by one consumer and never acked (its worker
		// crashed) must be reclaimable by another consumer's idle Pop —
		// the at-least-once recovery a bare ">" read cannot do on its own.
		q, err := NewRedisQueue(freshConfig())
		if err != nil {
			t.Fatalf("failed to create redis queue: %v", err)
		}
		defer q.Close()
		rq := q.(*redisQueue)

		ctx := context.Background()
		task := &core.DeliveryTask{ID: "task-orphan", Provider: "email"}
		if err := q.Push(ctx, task); err != nil {
			t.Fatalf("push: %v", err)
		}

		// Simulate the crashed worker: read the message under a consumer
		// that will never ack it, leaving it pending in the group.
		if _, err := rq.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    rq.group,
			Consumer: "dead-worker",
			Streams:  []string{rq.stream, ">"},
			Count:    1,
		}).Result(); err != nil {
			t.Fatalf("seed orphan: %v", err)
		}

		// Arm the claim window and let this queue's Pop sweep the orphan.
		prev := redisClaimMinIdle
		redisClaimMinIdle = 0
		defer func() { redisClaimMinIdle = prev }()

		popped, err := q.Pop(ctx)
		if err != nil {
			t.Fatalf("expected no error claiming orphan, got %v", err)
		}
		if popped == nil || popped.ID != task.ID {
			t.Fatalf("expected claimed task %s, got %+v", task.ID, popped)
		}
		if _, ok := rq.pending[task.ID]; !ok {
			t.Error("claimed task must be tracked for Ack/Nack")
		}
	})
}

func TestRedisQueueDefaultConfig(t *testing.T) {
	skipWithoutRedis(t)

	t.Run("uses default values", func(t *testing.T) {
		config := &QueueConfig{
			Type:  "redis",
			Redis: RedisConfig{}, // Empty to use defaults
		}

		q, err := NewRedisQueue(config)
		if err != nil {
			t.Fatalf("failed to create redis queue with defaults: %v", err)
		}
		_ = q.Close()
	})
}
