package queue

import (
	"time"

	"github.com/cuihairu/herald/core"
)

// QueueConfig is the configuration for a queue
type QueueConfig struct {
	Type    string        `yaml:"type"`
	Size    int           `yaml:"size"`
	Timeout time.Duration `yaml:"timeout"`
}

// Ensure memoryQueue implements core.Queue
var _ core.Queue = (*memoryQueue)(nil)
