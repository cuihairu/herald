package queue

import (
	"fmt"

	"github.com/cuihaitao/herald/core"
)

// NewQueue creates a new queue
func NewQueue(config *core.QueueConfig) (core.Queue, error) {
	switch config.Type {
	case "memory", "":
		return NewMemoryQueue(&QueueConfig{
			Size:    config.Size,
			Timeout: config.Timeout,
		})
	default:
		return nil, fmt.Errorf("unknown queue type: %s", config.Type)
	}
}
