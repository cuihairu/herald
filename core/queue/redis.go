package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/redis/go-redis/v9"
)

type pendingTask struct {
	StreamID string
	Task     *core.DeliveryTask
}

type redisQueue struct {
	client   *redis.Client
	stream   string
	group    string
	consumer string

	mu      sync.Mutex
	closed  bool
	pending map[string]string // taskID → streamEntryID (for Ack/Nack)
}

// NewRedisQueue creates a queue backed by Redis Streams (requires Redis 5.0+)
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
		client.Close()
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	// Create consumer group (MKSTREAM creates the stream if needed, requires Redis 5.0+)
	if err := client.XGroupCreateMkStream(ctx, stream, group, "0").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			client.Close()
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

func (q *redisQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, ErrQueueClosed
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

	// XACK first to remove from pending, then re-add to stream for retry
	_ = q.client.XAck(ctx, q.stream, q.group, streamID).Err()

	// Read original message and re-add to stream
	msgs, err := q.client.XRange(ctx, q.stream, streamID, streamID).Result()
	if err != nil || len(msgs) == 0 {
		return nil
	}

	// Re-push the original message
	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.stream,
		Values: msgs[0].Values,
	}).Err()
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
