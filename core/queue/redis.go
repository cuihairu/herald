package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/redis/go-redis/v9"
)

type redisQueue struct {
	client   *redis.Client
	stream   string
	group    string
	consumer string

	mu      sync.Mutex
	closed  bool
	pending map[string]string // taskID → streamEntryID (for Ack/Nack)
}

// NewRedisQueue creates a queue backed by Redis Streams (Redis 5.0+ for
// streams and consumer groups; scheduled retry waits additionally need
// 6.2+ for ZRANGE BYSCORE)
func NewRedisQueue(config *QueueConfig) (core.Queue, error) {
	addr := config.Redis.Addr
	if addr == "" {
		addr = "localhost:6379"
	}
	stream := config.Redis.Stream
	if stream == "" {
		stream = "herald:tasks"
	}
	group := config.Redis.Group
	if group == "" {
		group = "herald-workers"
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	// Create consumer group (MKSTREAM creates the stream if needed, requires Redis 5.0+)
	if err := client.XGroupCreateMkStream(ctx, stream, group, "0").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			_ = client.Close()
			return nil, fmt.Errorf("failed to create consumer group: %w", err)
		}
	}

	consumerName := fmt.Sprintf("worker-%d", time.Now().UnixNano())

	return &redisQueue{
		client:   client,
		stream:   stream,
		group:    group,
		consumer: consumerName,
		pending:  make(map[string]string),
	}, nil
}

func (q *redisQueue) Push(ctx context.Context, task *core.DeliveryTask) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueClosed
	}

	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	_, err = q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.stream,
		Values: map[string]interface{}{
			"task_id": task.ID,
			"data":    data,
		},
	}).Result()
	return err
}

// delayedKey is the sorted set holding tasks scheduled for the future,
// scored by due time (unix nanoseconds).
func (q *redisQueue) delayedKey() string {
	return q.stream + ":delayed"
}

// Schedule holds the task aside until delay has passed: the serialized task
// lands in a sorted set scored by its due time, and Pop moves due entries
// into the stream just ahead of its XREADGROUP. Because the set lives in
// Redis, a scheduled wait survives a process restart — the property that
// makes async re-enqueue safe on the Redis queue. A non-positive delay
// pushes straight onto the stream.
func (q *redisQueue) Schedule(ctx context.Context, task *core.DeliveryTask, delay time.Duration) error {
	q.mu.Lock()
	closed := q.closed
	q.mu.Unlock()
	if closed {
		return ErrQueueClosed
	}

	if delay <= 0 {
		return q.Push(ctx, task)
	}

	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	due := time.Now().Add(delay)
	return q.client.ZAdd(ctx, q.delayedKey(), redis.Z{
		Score:  float64(due.UnixNano()),
		Member: string(data),
	}).Err()
}

// moveDueScheduled transfers scheduled tasks whose wait has elapsed into
// the stream, so the normal read path picks them up. ZREM first arbitrates
// between processes sharing the queue: only the remover moves the entry (a
// crash between ZREM and XADD drops one due task — the same at-most-once
// window Nack's requeue already accepts). An entry whose payload no longer
// decodes is dropped rather than returned: it would otherwise be re-read
// and re-dropped on every Pop forever.
func (q *redisQueue) moveDueScheduled(ctx context.Context) error {
	key := q.delayedKey()
	due := strconv.FormatInt(time.Now().UnixNano(), 10)
	// ZRANGE BYSCORE (Redis 6.2+) — the delayed floor is 6.2 for this
	// feature, up from the Streams-only floor of 5.0.
	members, err := q.client.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key: key, Start: "-inf", Stop: due, ByScore: true, Offset: 0, Count: 16,
	}).Result()
	if err != nil || len(members) == 0 {
		return err
	}

	for _, member := range members {
		removed, err := q.client.ZRem(ctx, key, member).Result()
		if err != nil {
			return err
		}
		if removed == 0 {
			// Another process won the entry and is moving it.
			continue
		}
		var task core.DeliveryTask
		if err := json.Unmarshal([]byte(member), &task); err != nil {
			continue
		}
		if _, err := q.client.XAdd(ctx, &redis.XAddArgs{
			Stream: q.stream,
			Values: map[string]interface{}{
				"task_id": task.ID,
				"data":    member,
			},
		}).Result(); err != nil {
			return err
		}
	}
	return nil
}

func (q *redisQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, ErrQueueClosed
	}

	// Due scheduled tasks enter the stream ahead of the read, so a wait
	// that elapsed while the worker was blocked comes back immediately.
	if moveErr := q.moveDueScheduled(ctx); moveErr != nil {
		return nil, moveErr
	}

	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    q.group,
		Consumer: q.consumer,
		Streams:  []string{q.stream, ">"},
		Count:    1,
		Block:    2 * time.Second,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				return nil, fmt.Errorf("no messages available")
			}
		}
		return nil, err
	}

	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			return nil, fmt.Errorf("no messages available")
		}
	}

	msg := streams[0].Messages[0]
	return q.decodeAndTrack(msg)
}

func (q *redisQueue) Ack(ctx context.Context, taskID string) error {
	q.mu.Lock()
	streamID, ok := q.pending[taskID]
	if ok {
		delete(q.pending, taskID)
	}
	q.mu.Unlock()

	if !ok {
		return nil
	}

	return q.client.XAck(ctx, q.stream, q.group, streamID).Err()
}

func (q *redisQueue) Nack(ctx context.Context, taskID string, _ error) error {
	q.mu.Lock()
	streamID, ok := q.pending[taskID]
	if ok {
		delete(q.pending, taskID)
	}
	q.mu.Unlock()

	if !ok {
		return nil
	}

	// Claim the message back so another worker can pick it up
	// XPENDING + XCLAIM is the standard pattern for retry
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Read original message before acking so a failed requeue does not lose payload.
	msgs, err := q.client.XRange(ctx, q.stream, streamID, streamID).Result()
	if err != nil || len(msgs) == 0 {
		return fmt.Errorf("failed to load message for retry: %w", err)
	}

	// Re-push the original message before acking the pending entry.
	if _, err := q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.stream,
		Values: msgs[0].Values,
	}).Result(); err != nil {
		return fmt.Errorf("failed to requeue message: %w", err)
	}

	if err := q.client.XAck(ctx, q.stream, q.group, streamID).Err(); err != nil {
		return fmt.Errorf("failed to ack nacked message: %w", err)
	}

	return nil
}

func (q *redisQueue) Size() int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	info, err := q.client.XInfoStream(ctx, q.stream).Result()
	if err != nil {
		return 0
	}
	return int(info.Length)
}

func (q *redisQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}
	q.closed = true
	return q.client.Close()
}

func (q *redisQueue) decodeAndTrack(msg redis.XMessage) (*core.DeliveryTask, error) {
	data, ok := msg.Values["data"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid message format: missing data field")
	}

	var task core.DeliveryTask
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// Map task ID → stream entry ID for Ack/Nack
	q.pending[task.ID] = msg.ID

	return &task, nil
}
