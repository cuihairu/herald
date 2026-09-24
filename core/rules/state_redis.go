package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStateStore persists rule state in Redis so "for" duration judgements
// survive restarts and are shared across herald instances. Keys follow
// stateKey and every entry carries the TTL handed to Put.
type RedisStateStore struct {
	client *redis.Client
}

// NewRedisStateStore connects to Redis and verifies the connection, so a
// misconfigured address fails at startup instead of at the first alert.
func NewRedisStateStore(addr, password string, db int) (*RedisStateStore, error) {
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("rules: connect redis state store: %w", err)
	}
	return &RedisStateStore{client: client}, nil
}

// Get returns the state under key, or (nil, nil) when absent.
func (s *RedisStateStore) Get(ctx context.Context, key string) (*RuleState, error) {
	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rules: redis state get %s: %w", key, err)
	}
	var state RuleState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("rules: redis state decode %s: %w", key, err)
	}
	return &state, nil
}

// Put stores state under key with the given ttl (ttl <= 0 means no expiry).
func (s *RedisStateStore) Put(ctx context.Context, key string, state *RuleState, ttl time.Duration) error {
	if state == nil {
		return fmt.Errorf("rules: redis state store: put nil state")
	}
	// RuleState has only marshalable fields, so encoding cannot fail.
	data, _ := json.Marshal(state)
	if err := s.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("rules: redis state put %s: %w", key, err)
	}
	return nil
}

// Delete removes the entry under key; absent keys are a no-op.
func (s *RedisStateStore) Delete(ctx context.Context, key string) error {
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("rules: redis state delete %s: %w", key, err)
	}
	return nil
}

// DeleteRule removes every entry belonging to ruleID via a SCAN sweep.
// Rule changes are rare operator actions, so the sweep cost is irrelevant.
func (s *RedisStateStore) DeleteRule(ctx context.Context, ruleID string) error {
	pattern := stateKey(ruleID, "*")
	var keys []string
	var cursor uint64
	for {
		batch, next, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("rules: redis state scan rule %s: %w", ruleID, err)
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if err := s.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("rules: redis state delete rule %s: %w", ruleID, err)
	}
	return nil
}

// Close releases the Redis connection.
func (s *RedisStateStore) Close() error {
	return s.client.Close()
}
