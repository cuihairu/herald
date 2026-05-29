package config

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/cuihairu/herald/core/template"
	"gopkg.in/yaml.v3"
)

// Config is the herald configuration
type Config struct {
	Server    ServerConfig                       `yaml:"server"`
	WebSocket WebSocketConfig                    `yaml:"websocket"`
	Auth      AuthConfig                         `yaml:"auth"`
	Providers map[string]ProviderConfig          `yaml:"providers"`
	Routes    map[string][]string                `yaml:"routes"`
	Queue     QueueConfig                        `yaml:"queue"`
	Retry     RetryConfig                        `yaml:"retry"`
	Dedup     DedupConfig                        `yaml:"dedup"`
	Templates map[string]template.TemplateConfig `yaml:"templates"`
}

// ServerConfig is the server configuration
type ServerConfig struct {
	Addr    string        `yaml:"addr"`
	Timeout time.Duration `yaml:"timeout"`
}

// WebSocketConfig is the websocket worker server configuration
type WebSocketConfig struct {
	Addr          string        `yaml:"addr"`
	ReadTimeout   time.Duration `yaml:"read_timeout"`
	WriteTimeout  time.Duration `yaml:"write_timeout"`
	PingTimeout   time.Duration `yaml:"ping_timeout"`
	PingInterval  time.Duration `yaml:"ping_interval"`
}

// AuthConfig is the authentication configuration
type AuthConfig struct {
	Enabled   bool              `yaml:"enabled"`
	APIKeys   map[string]string `yaml:"api_keys"`
	SecretKey string            `yaml:"secret_key"`
	AdminUser map[string]string `yaml:"admin_user"` // username -> password
}

// ProviderConfig is a provider configuration
type ProviderConfig struct {
	Type    string                 `yaml:"type"`
	Config  map[string]interface{} `yaml:"config"`
	Enabled *bool                  `yaml:"enabled"` // nil means true (default enabled)
}

// QueueConfig is the queue configuration
type QueueConfig struct {
	Type    string        `yaml:"type"`
	Size    int           `yaml:"size"`
	Workers int           `yaml:"workers"`
	Timeout time.Duration `yaml:"timeout"`
}

// RetryConfig is the retry configuration
type RetryConfig struct {
	Max          int           `yaml:"max"`
	Backoff      string        `yaml:"backoff"`
	InitialDelay time.Duration `yaml:"initial_delay"`
	MaxDelay     time.Duration `yaml:"max_delay"`
}

// DedupConfig is the dedup configuration
type DedupConfig struct {
	Enabled bool          `yaml:"enabled"`
	Window  time.Duration `yaml:"window"`
}

// Load loads configuration from a file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Server.Timeout == 0 {
		cfg.Server.Timeout = 30 * time.Second
	}
	if cfg.WebSocket.Addr == "" {
		cfg.WebSocket.Addr = ":8081"
	}
	if cfg.WebSocket.ReadTimeout == 0 {
		cfg.WebSocket.ReadTimeout = 60 * time.Second
	}
	if cfg.WebSocket.WriteTimeout == 0 {
		cfg.WebSocket.WriteTimeout = 60 * time.Second
	}
	if cfg.WebSocket.PingTimeout == 0 {
		cfg.WebSocket.PingTimeout = 30 * time.Second
	}
	if cfg.WebSocket.PingInterval == 0 {
		cfg.WebSocket.PingInterval = 20 * time.Second
	}
	if cfg.Queue.Type == "" {
		cfg.Queue.Type = "memory"
	}
	if cfg.Queue.Size == 0 {
		cfg.Queue.Size = 10000
	}
	if cfg.Queue.Workers == 0 {
		cfg.Queue.Workers = runtime.NumCPU()*2 + 1
	}
	if cfg.Routes == nil {
		cfg.Routes = make(map[string][]string)
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]ProviderConfig)
	}
	if cfg.Templates == nil {
		cfg.Templates = make(map[string]template.TemplateConfig)
	}
	if cfg.Dedup.Window == 0 {
		cfg.Dedup.Window = 5 * time.Minute
	}
	if cfg.Retry.Max == 0 {
		cfg.Retry.Max = 3
	}
	if cfg.Retry.Backoff == "" {
		cfg.Retry.Backoff = "exponential"
	}
	if cfg.Retry.InitialDelay == 0 {
		cfg.Retry.InitialDelay = time.Second
	}
	if cfg.Retry.MaxDelay == 0 {
		cfg.Retry.MaxDelay = time.Minute
	}

	return &cfg, nil
}

// Default returns default configuration
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Addr:    ":8080",
			Timeout: 30 * time.Second,
		},
		WebSocket: WebSocketConfig{
			Addr:         ":8081",
			ReadTimeout:  60 * time.Second,
			WriteTimeout: 60 * time.Second,
			PingTimeout:  30 * time.Second,
			PingInterval: 20 * time.Second,
		},
		Auth: AuthConfig{
			Enabled:   false,
			APIKeys:   make(map[string]string),
			SecretKey: "",
			AdminUser: map[string]string{
				"admin": "admin",
			},
		},
		Providers: make(map[string]ProviderConfig),
		Routes: map[string][]string{
			"error": {},
		},
		Queue: QueueConfig{
			Type:    "memory",
			Size:    10000,
			Timeout: 5 * time.Second,
		},
		Retry: RetryConfig{
			Max:          3,
			Backoff:      "exponential",
			InitialDelay: time.Second,
			MaxDelay:     time.Minute,
		},
		Dedup: DedupConfig{
			Enabled: true,
			Window:  5 * time.Minute,
		},
	}
}

// ExpandEnv expands environment variables in config
func (c *Config) ExpandEnv() {
	for name, p := range c.Providers {
		if p.Config == nil {
			p.Config = make(map[string]interface{})
		}
		for k, v := range p.Config {
			if s, ok := v.(string); ok {
				p.Config[k] = expandEnv(s)
			}
		}
		c.Providers[name] = p
	}
}

func expandEnv(s string) string {
	if len(s) > 0 && s[0] == '$' {
		key := s[1:]
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return s
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Server.Addr == "" {
		return fmt.Errorf("server addr is required")
	}
	if c.Auth.Enabled && len(c.Auth.APIKeys) == 0 {
		return fmt.Errorf("auth enabled but no api_keys configured")
	}
	for name, provider := range c.Providers {
		if provider.Type == "" {
			return fmt.Errorf("provider %s type is required", name)
		}
	}
	return nil
}
