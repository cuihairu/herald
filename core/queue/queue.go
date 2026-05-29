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
	Redis   RedisConfig   `yaml:"redis"`
}

// RedisConfig is the redis queue configuration
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	Stream   string `yaml:"stream"`
	Group    string `yaml:"group"`
}

// Ensure memoryQueue implements core.Queue
var _ core.Queue = (*memoryQueue)(nil)
