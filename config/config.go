package config

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/groups"
	"github.com/cuihairu/herald/core/limiter"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/rules"
	"github.com/cuihairu/herald/core/template"
	"gopkg.in/yaml.v3"
)

// DeliveryConfig overrides the §6 taxonomy tables (品类→紧急度,
// 渠道→强度, 品类→投递模式). The defaults already cover the documented
// categories; these maps exist so an operator can re-grade a category or
// demote/promote a channel without a code change. An unparseable value
// refuses to start (validated where the policy is built), never a
// silent clamp.
type DeliveryConfig struct {
	// CategoryUrgency maps a category to routine|normal|urgent|critical.
	CategoryUrgency map[string]string `yaml:"category_urgency"`
	// ChannelIntensity maps a channel name to L0..L5.
	ChannelIntensity map[string]string `yaml:"channel_intensity"`
	// CategoryMode maps a category to fixed|escalation|parallel.
	CategoryMode map[string]string `yaml:"category_mode"`
	// AllowPhone lifts the §6.2 电话默认禁用 gate: channels whose
	// effective intensity is L5 stay refused until this is set — the
	// 配置同意 half of the 配置+受众双重同意 rule (the audience half is
	// binding a phone contact surface at all).
	AllowPhone bool `yaml:"allow_phone"`
}

// Config is the herald configuration
type Config struct {
	Server    ServerConfig              `yaml:"server"`
	WebSocket WebSocketConfig           `yaml:"websocket"`
	Worker    WorkerConfig              `yaml:"worker"`
	Auth      AuthConfig                `yaml:"auth"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	// Channels seeds the channel table: a named channel mapping to the
	// provider instances that deliver for it. A plain channel reference
	// that is not itself a provider instance resolves through it.
	Channels map[string]ChannelConfig `yaml:"channels"`
	Routes   map[string][]string      `yaml:"routes"`
	// LevelRoutes is the fallback routing table consulted when the type
	// route misses: level -> providers, tried in Router.Route after the
	// type table.
	LevelRoutes map[string][]string `yaml:"level_routes"`
	Queue       QueueConfig         `yaml:"queue"`
	Retry       RetryConfig         `yaml:"retry"`
	Dedup       DedupConfig         `yaml:"dedup"`
	// Digest configures the §10 aggregator (window fold schedules and the
	// multi-instance leader lease). Disabled (the zero value) keeps every
	// event direct.
	Digest DigestConfig `yaml:"digest"`
	// Feeds configures the §9 RSS pull channel (边界审计 §4): per-category
	// public feeds and per-audience private feeds readers pull at their
	// own cadence. Disabled (the zero value) keeps the feed endpoints off
	// and the rss channel behaving as an unknown provider.
	Feeds FeedsConfig `yaml:"feeds"`
	// Delivery configures the §6 强度×紧急度×模式策略件 (batch 9 scaffolding):
	// category→urgency, channel→intensity, category→mode override tables.
	// Invalid values refuse to start; missing tables fall back to taxonomy defaults.
	Delivery DeliveryConfig `yaml:"delivery"`
	// Sources configures the §8 来源适配器 (关系详设): the platform entry
	// points that converge follow/unfollow/check actions onto the
	// registries. Disabled (the zero value) keeps every source endpoint
	// off and the registries untouched by external events.
	Sources   SourcesConfig                      `yaml:"sources"`
	Templates map[string]template.TemplateConfig `yaml:"templates"`
	Rules     []rules.Rule                       `yaml:"rules"`
	// RulesStore points at the persistent rules file. Empty keeps rules
	// in memory only (seeds from Rules are still honored).
	RulesStore string `yaml:"rules_store"`
	// RulesDefaultPolicy decides what happens to notifications no active
	// rule matched: "allow" (default) falls back to static routing,
	// "deny" withholds them (whitelist mode). Empty means "allow".
	RulesDefaultPolicy string `yaml:"rules_default_policy"`
	// RulesState configures the external store for stateful rule semantics
	// (the "for" duration). Nil keeps state in-process (single instance).
	RulesState *RulesStateConfig `yaml:"rules_state"`
	// Groups seeds notification groups (named delivery audiences that
	// "group:" channel references expand to).
	Groups []groups.Group `yaml:"groups"`
	// GroupsStore points at the persistent groups file. Empty keeps groups
	// in memory only (seeds from Groups are still honored).
	GroupsStore string `yaml:"groups_store"`
	// Audiences seeds the user-level audience table (core/audience): named
	// sets of recipient ids that "user:" channel references can name.
	Audiences map[string]audience.Audience `yaml:"audiences"`
	// Recipients seeds the user-level recipient table: each named person
	// holds the provider endpoints they receive on. "user:<id>" in any
	// channel list resolves through Audiences first, then Recipients.
	Recipients map[string]audience.Recipient `yaml:"recipients"`
	// RostersStore points at the persistent duty-roster file (the push
	// target of the silence schedule placeholder). Empty keeps rosters in
	// memory only.
	RostersStore string `yaml:"rosters_store"`
	// EscalationStore points at the persistent pending-upgrade file for
	// ack-gated escalation. Empty keeps pending upgrades in memory only
	// (a restart then silently drops them instead of escalating).
	EscalationStore string `yaml:"escalation_store"`
	// IncidentLimit caps the in-memory incident ledger. Zero picks the
	// default (1000); open incidents are never evicted.
	IncidentLimit int `yaml:"incident_limit"`
	// CardCallback configures the Feishu interactive-card acknowledge
	// button callback (POST /api/v1/callbacks/feishu).
	CardCallback *CardCallbackConfig `yaml:"card_callback"`
}

// CardCallbackConfig holds the provider-side secrets of card button
// callbacks. EncryptKey is the Feishu console's "Encrypt Key": callbacks
// that arrive encrypted are decrypted with it; empty keeps encrypted
// callbacks rejected (plaintext and the URL challenge still work).
type CardCallbackConfig struct {
	EncryptKey string `yaml:"encrypt_key"`
}

// RulesStateConfig selects where rule evaluation state (for windows) lives.
// Type "memory" (default) keeps state in-process; "redis" shares state
// across instances and restarts.
type RulesStateConfig struct {
	// Type is "memory" (default) or "redis".
	Type string `yaml:"type"`
	// Redis connection settings, used when Type is "redis".
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// ServerConfig is the server configuration
type ServerConfig struct {
	Addr    string        `yaml:"addr"`
	Timeout time.Duration `yaml:"timeout"`
}

// WebSocketConfig is the websocket worker server configuration
type WebSocketConfig struct {
	Addr           string        `yaml:"addr"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	PingTimeout    time.Duration `yaml:"ping_timeout"`
	PingInterval   time.Duration `yaml:"ping_interval"`
	AllowedOrigins []string      `yaml:"allowed_origins"`
}

// WorkerConfig is the remote worker control-plane configuration.
type WorkerConfig struct {
	ID                string        `yaml:"id"`
	ServerURL         string        `yaml:"server_url"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	ReconnectDelay    time.Duration `yaml:"reconnect_delay"`
	Capabilities      []string      `yaml:"capabilities"`
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
	// RateLimit optionally rate-limits deliveries through this provider
	// (token bucket). Nil means unlimited.
	RateLimit *limiter.Config `yaml:"rate_limit"`
}

// ChannelConfig maps one named channel onto the provider instances that
// deliver for it (design §27: `channels: {ci: {providers: [...]}}`).
type ChannelConfig struct {
	Providers []string `yaml:"providers"`
}

// QueueConfig is the queue configuration
type QueueConfig struct {
	Type    string        `yaml:"type"`
	Size    int           `yaml:"size"`
	Workers int           `yaml:"workers"`
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

// ToQueueConfig converts to queue.QueueConfig
func (c QueueConfig) ToQueueConfig() *queue.QueueConfig {
	return &queue.QueueConfig{
		Type:    c.Type,
		Size:    c.Size,
		Timeout: c.Timeout,
		Redis: queue.RedisConfig{
			Addr:     c.Redis.Addr,
			Password: c.Redis.Password,
			DB:       c.Redis.DB,
			Stream:   c.Redis.Stream,
			Group:    c.Redis.Group,
		},
	}
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
	// CategoryTiers re-grades a category's §11.2 frequency tier
	// ("once"|"throttle"|"always"); missing categories keep the default
	// table (system=once, alerts=throttle, others window-throttled).
	// Unparseable values refuse to start (Validate).
	CategoryTiers map[string]string `yaml:"category_tiers"`
	// CategoryWindows overrides the fold window per category (§11.2
	// 「节点告警 30 分钟一条」). Non-positive values refuse to start.
	CategoryWindows map[string]time.Duration `yaml:"category_windows"`
}

// DigestConfig configures the digest flip loop (关系详设 §10): when the
// fold schedules run and — for multi-instance fleets — where the leader
// lease lives. All flip times are parsed as clock times in Location.
type DigestConfig struct {
	Enabled bool `yaml:"enabled"`
	// Interval is the flip-loop tick (how often due windows are checked).
	// Defaults to 1m when zero.
	Interval time.Duration `yaml:"interval"`
	// Daily is the daily flip time "HH:MM" (default "09:00").
	Daily string `yaml:"daily"`
	// Weekly is the weekly flip time "Weekday HH:MM" (default
	// "Mon 09:00").
	Weekly string `yaml:"weekly"`
	// Location names the IANA timezone for the flip times (default
	// "Asia/Shanghai").
	Location string `yaml:"location"`
	// RedisAddr points at redis for the leader lease (§10 redis 锁选主):
	// empty keeps the loop unlocked, which assumes a single instance.
	RedisAddr     string `yaml:"redis_addr"`
	RedisPassword string `yaml:"redis_password"`
	RedisDB       int    `yaml:"redis_db"`
	// LeaseTTL bounds one leader lease; renewed every tick while held.
	// Defaults to 60s when zero.
	LeaseTTL time.Duration `yaml:"lease_ttl"`
}

// FeedsConfig configures the §9 RSS pull channel: what the rendered
// feeds call themselves and how much history each keeps. The feed store
// is in-memory — a restart starts from an empty feed, which readers
// treat as "no new items", not as data loss.
type FeedsConfig struct {
	Enabled bool `yaml:"enabled"`
	// Title is the RSS channel title rendered into every feed. Defaults
	// to "Herald 通知" when empty (the renderer refuses an empty title).
	Title string `yaml:"title"`
	// Link is the feed's home link. Empty omits the <link> element.
	Link string `yaml:"link"`
	// Description is the feed's channel description. Empty omits it.
	Description string `yaml:"description"`
	// MaxItems caps each category's item log (FIFO — the oldest drop
	// first). Defaults to 500 when zero.
	MaxItems int `yaml:"max_items"`
}

// SourcesConfig configures the §8 来源适配器: the platform entry points
// (telegram bot webhook, 公众号 server callback) that turn external
// follow/unfollow actions into registry changes, and the periodic
// external-state sweep of rule 3 (Herald 只信自己登记的关系，外部状态
// 定期对账). Each entry keeps its own off switch: an empty secret or
// token means that endpoint never opens, even with enabled: true.
type SourcesConfig struct {
	Enabled bool `yaml:"enabled"`
	// Bot is the telegram bot entry.
	Bot SourceBotConfig `yaml:"bot"`
	// WeChatMP is the 公众号 entry.
	WeChatMP SourceWeChatMPConfig `yaml:"wechat_mp"`
	// Reconcile sweeps active surfaces against the platforms (rule 3).
	Reconcile SourceReconcileConfig `yaml:"reconcile"`
}

// SourceBotConfig is the telegram bot entry: the webhook secret token
// Telegram sends on every update (X-Telegram-Bot-Api-Secret-Token), and
// the default subscription group a successful `/start <token>` lays down.
type SourceBotConfig struct {
	Secret string `yaml:"secret"`
	// DefaultCategories are subscribed on a successful bind (source
	// bot, unsubscribable). Empty binds the surface only.
	DefaultCategories []string `yaml:"default_categories"`
}

// SourceWeChatMPConfig is the 公众号 entry: the console verification
// token request signatures are checked against, and the default
// subscription group a follow event lays down.
type SourceWeChatMPConfig struct {
	Token string `yaml:"token"`
	// DefaultCategories are subscribed on a follow event (source
	// wechat_mp, unsubscribable). Empty registers the surface only.
	DefaultCategories []string `yaml:"default_categories"`
}

// SourceReconcileConfig configures the periodic external-state sweep
// (rule 3 对账): active surfaces are probed ("does the platform still
// know this target?") and dead ones are invalidated with their channel
// subscriptions terminated. Probe errors skip — 对账 never guesses.
type SourceReconcileConfig struct {
	Enabled bool `yaml:"enabled"`
	// Interval between sweeps. Defaults to 1h when zero.
	Interval time.Duration `yaml:"interval"`
	// LeaseTTL is the redis leader lease (锁选主) so a multi-instance
	// fleet sweeps exactly once per round. Defaults to 1m when zero.
	// The lease shares digest's redis connection settings (digest
	// redis_addr / redis_password / redis_db); sweep state itself is
	// never persisted — each round re-reads the registries.
	LeaseTTL time.Duration `yaml:"lease_ttl"`
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
	if cfg.LevelRoutes == nil {
		cfg.LevelRoutes = make(map[string][]string)
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]ProviderConfig)
	}
	if cfg.Channels == nil {
		cfg.Channels = make(map[string]ChannelConfig)
	}
	if cfg.Audiences == nil {
		cfg.Audiences = make(map[string]audience.Audience)
	}
	if cfg.Recipients == nil {
		cfg.Recipients = make(map[string]audience.Recipient)
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
	if cfg.Worker.HeartbeatInterval == 0 {
		cfg.Worker.HeartbeatInterval = 20 * time.Second
	}
	if cfg.Worker.ReconnectDelay == 0 {
		cfg.Worker.ReconnectDelay = 5 * time.Second
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
		Worker: WorkerConfig{
			HeartbeatInterval: 20 * time.Second,
			ReconnectDelay:    5 * time.Second,
			Capabilities:      []string{"*"},
		},
		Auth: AuthConfig{
			Enabled:   false,
			APIKeys:   make(map[string]string),
			SecretKey: "",
			AdminUser: map[string]string{
				"admin": "admin",
			},
		},
		Providers:  make(map[string]ProviderConfig),
		Channels:   make(map[string]ChannelConfig),
		Audiences:  make(map[string]audience.Audience),
		Recipients: make(map[string]audience.Recipient),
		Routes: map[string][]string{
			"error": {},
		},
		LevelRoutes: make(map[string][]string),
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

// ChannelRoutes flattens the channels block into the router's
// name -> providers shape (route.Config.Channels).
func (c *Config) ChannelRoutes() map[string][]string {
	out := make(map[string][]string, len(c.Channels))
	for name, ch := range c.Channels {
		out[name] = ch.Providers
	}
	return out
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
	// The channels block is one level deep by construction: its entries are
	// provider instances, never other channels — an unknown name is
	// configuration drift and refuses to start, like the audience tables.
	for name, channel := range c.Channels {
		if len(channel.Providers) == 0 {
			return fmt.Errorf("channel %s has no providers", name)
		}
		for _, p := range channel.Providers {
			if _, ok := c.Providers[p]; !ok {
				return fmt.Errorf("channel %s references unknown provider %s", name, p)
			}
		}
	}
	// §11.2 override tables: a mis-graded tier refuses to start (the
	// gate would otherwise silently deliver differently than graded),
	// and a non-positive window is a configuration drift, not a clamp.
	for category, tier := range c.Dedup.CategoryTiers {
		if _, err := dedup.ParseTier(tier); err != nil {
			return fmt.Errorf("dedup category_tiers[%s]: %w", category, err)
		}
	}
	for category, window := range c.Dedup.CategoryWindows {
		if window <= 0 {
			return fmt.Errorf("dedup category_windows[%s]: window must be positive, got %v", category, window)
		}
	}
	return nil
}
