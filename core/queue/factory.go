package queue

import (
	"fmt"

	"github.com/cuihairu/herald/core"
)

// NewQueue creates a new queue
func NewQueue(config *QueueConfig) (core.Queue, error) {
	switch config.Type {
	case "memory", "":
		return NewMemoryQueue(config)
	case "redis":
		return NewRedisQueue(config)
	default:
		return nil, fmt.Errorf("unknown queue type: %s", config.Type)
	}
}
