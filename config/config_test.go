package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	return path
}

func TestLoad(t *testing.T) {
	t.Run("file not found", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.yaml")
		_, err := Load(path)
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := writeConfigFile(t, "server: [unclosed")
		_, err := Load(path)
		if err == nil {
			t.Error("expected error for invalid yaml")
		}
	})

	t.Run("empty file applies defaults", func(t *testing.T) {
		path := writeConfigFile(t, "")
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if cfg.Server.Addr != ":8080" {
			t.Errorf("expected server addr :8080, got %s", cfg.Server.Addr)
		}
		if cfg.Server.Timeout != 30*time.Second {
			t.Errorf("expected server timeout 30s, got %v", cfg.Server.Timeout)
		}
		if cfg.WebSocket.Addr != ":8081" {
			t.Errorf("expected websocket addr :8081, got %s", cfg.WebSocket.Addr)
		}
		if cfg.WebSocket.ReadTimeout != 60*time.Second {
			t.Errorf("expected websocket read timeout 60s, got %v", cfg.WebSocket.ReadTimeout)
		}
		if cfg.WebSocket.WriteTimeout != 60*time.Second {
			t.Errorf("expected websocket write timeout 60s, got %v", cfg.WebSocket.WriteTimeout)
		}
		if cfg.WebSocket.PingTimeout != 30*time.Second {
			t.Errorf("expected websocket ping timeout 30s, got %v", cfg.WebSocket.PingTimeout)
		}
		if cfg.WebSocket.PingInterval != 20*time.Second {
			t.Errorf("expected websocket ping interval 20s, got %v", cfg.WebSocket.PingInterval)
		}
		if cfg.Queue.Type != "memory" {
			t.Errorf("expected queue type memory, got %s", cfg.Queue.Type)
		}
		if cfg.Queue.Size != 10000 {
			t.Errorf("expected queue size 10000, got %d", cfg.Queue.Size)
		}
		expectedWorkers := runtime.NumCPU()*2 + 1
		if cfg.Queue.Workers != expectedWorkers {
			t.Errorf("expected queue workers %d, got %d", expectedWorkers, cfg.Queue.Workers)
		}
		if cfg.Routes == nil {
			t.Error("expected non-nil routes")
		}
		if len(cfg.Routes) != 0 {
			t.Errorf("expected empty routes, got %v", cfg.Routes)
		}
		if cfg.Providers == nil {
			t.Error("expected non-nil providers")
		}
		if cfg.Templates == nil {
			t.Error("expected non-nil templates")
		}
		if cfg.Dedup.Window != 5*time.Minute {
			t.Errorf("expected dedup window 5m, got %v", cfg.Dedup.Window)
		}
		if cfg.Retry.Max != 3 {
			t.Errorf("expected retry max 3, got %d", cfg.Retry.Max)
		}
		if cfg.Retry.Backoff != "exponential" {
			t.Errorf("expected retry backoff exponential, got %s", cfg.Retry.Backoff)
		}
		if cfg.Retry.InitialDelay != time.Second {
			t.Errorf("expected retry initial delay 1s, got %v", cfg.Retry.InitialDelay)
		}
		if cfg.Retry.MaxDelay != time.Minute {
			t.Errorf("expected retry max delay 1m, got %v", cfg.Retry.MaxDelay)
		}
		if cfg.Worker.HeartbeatInterval != 20*time.Second {
			t.Errorf("expected worker heartbeat interval 20s, got %v", cfg.Worker.HeartbeatInterval)
		}
		if cfg.Worker.ReconnectDelay != 5*time.Second {
			t.Errorf("expected worker reconnect delay 5s, got %v", cfg.Worker.ReconnectDelay)
		}
	})

	t.Run("partial config fills unset defaults", func(t *testing.T) {
		path := writeConfigFile(t, "server:\n  addr: \":9090\"\n")
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cfg.Server.Addr != ":9090" {
			t.Errorf("expected server addr :9090, got %s", cfg.Server.Addr)
		}
		if cfg.Server.Timeout != 30*time.Second {
			t.Errorf("expected default server timeout 30s, got %v", cfg.Server.Timeout)
		}
		if cfg.Queue.Type != "memory" {
			t.Errorf("expected default queue type memory, got %s", cfg.Queue.Type)
		}
	})

	t.Run("full config", func(t *testing.T) {
		content := `
server:
  addr: ":9090"
  timeout: 10s

websocket:
  addr: ":9091"
  read_timeout: 11s
  write_timeout: 12s
  ping_timeout: 13s
  ping_interval: 14s
  allowed_origins:
    - http://example.com

worker:
  id: worker-1
  server_url: http://localhost:8080
  heartbeat_interval: 15s
  reconnect_delay: 16s
  capabilities:
    - log

auth:
  enabled: true
  api_keys:
    admin: secret-key
  secret_key: jwt-secret
  admin_user:
    admin: password

providers:
  webhook:
    type: webhook
    enabled: false
    config:
      url: http://example.com/hook
      method: POST

routes:
  error:
    - webhook

queue:
  type: redis
  size: 100
  workers: 4
  timeout: 6s
  redis:
    addr: localhost:6379
    password: redis-pass
    db: 1
    stream: herald-stream
    group: herald-group

retry:
  max: 5
  backoff: fixed
  initial_delay: 2s
  max_delay: 3m

dedup:
  enabled: true
  window: 10m

templates:
  server_alert:
    name: "server alert"
    title: "alert"
    level: "error"
    fields:
      - label: "server"
        value: "web-1"
        type: "text"
`
		path := writeConfigFile(t, content)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if cfg.Server.Addr != ":9090" {
			t.Errorf("expected server addr :9090, got %s", cfg.Server.Addr)
		}
		if cfg.Server.Timeout != 10*time.Second {
			t.Errorf("expected server timeout 10s, got %v", cfg.Server.Timeout)
		}
		if cfg.WebSocket.Addr != ":9091" {
			t.Errorf("expected websocket addr :9091, got %s", cfg.WebSocket.Addr)
		}
		if cfg.WebSocket.ReadTimeout != 11*time.Second {
			t.Errorf("expected websocket read timeout 11s, got %v", cfg.WebSocket.ReadTimeout)
		}
		if cfg.WebSocket.WriteTimeout != 12*time.Second {
			t.Errorf("expected websocket write timeout 12s, got %v", cfg.WebSocket.WriteTimeout)
		}
		if cfg.WebSocket.PingTimeout != 13*time.Second {
			t.Errorf("expected websocket ping timeout 13s, got %v", cfg.WebSocket.PingTimeout)
		}
		if cfg.WebSocket.PingInterval != 14*time.Second {
			t.Errorf("expected websocket ping interval 14s, got %v", cfg.WebSocket.PingInterval)
		}
		if len(cfg.WebSocket.AllowedOrigins) != 1 || cfg.WebSocket.AllowedOrigins[0] != "http://example.com" {
			t.Errorf("expected allowed origins [http://example.com], got %v", cfg.WebSocket.AllowedOrigins)
		}
		if cfg.Worker.ID != "worker-1" {
			t.Errorf("expected worker id worker-1, got %s", cfg.Worker.ID)
		}
		if cfg.Worker.ServerURL != "http://localhost:8080" {
			t.Errorf("expected worker server url http://localhost:8080, got %s", cfg.Worker.ServerURL)
		}
		if cfg.Worker.HeartbeatInterval != 15*time.Second {
			t.Errorf("expected worker heartbeat interval 15s, got %v", cfg.Worker.HeartbeatInterval)
		}
		if cfg.Worker.ReconnectDelay != 16*time.Second {
			t.Errorf("expected worker reconnect delay 16s, got %v", cfg.Worker.ReconnectDelay)
		}
		if len(cfg.Worker.Capabilities) != 1 || cfg.Worker.Capabilities[0] != "log" {
			t.Errorf("expected worker capabilities [log], got %v", cfg.Worker.Capabilities)
		}
		if !cfg.Auth.Enabled {
			t.Error("expected auth enabled")
		}
		if cfg.Auth.APIKeys["admin"] != "secret-key" {
			t.Errorf("expected api key admin=secret-key, got %v", cfg.Auth.APIKeys)
		}
		if cfg.Auth.SecretKey != "jwt-secret" {
			t.Errorf("expected secret key jwt-secret, got %s", cfg.Auth.SecretKey)
		}
		if cfg.Auth.AdminUser["admin"] != "password" {
			t.Errorf("expected admin user admin=password, got %v", cfg.Auth.AdminUser)
		}
		provider, ok := cfg.Providers["webhook"]
		if !ok {
			t.Fatal("expected webhook provider")
		}
		if provider.Type != "webhook" {
			t.Errorf("expected provider type webhook, got %s", provider.Type)
		}
		if provider.Enabled == nil || *provider.Enabled {
			t.Errorf("expected provider enabled false, got %v", provider.Enabled)
		}
		if provider.Config["url"] != "http://example.com/hook" {
			t.Errorf("expected provider config url, got %v", provider.Config)
		}
		if len(cfg.Routes["error"]) != 1 || cfg.Routes["error"][0] != "webhook" {
			t.Errorf("expected routes error=[webhook], got %v", cfg.Routes)
		}
		if cfg.Queue.Type != "redis" {
			t.Errorf("expected queue type redis, got %s", cfg.Queue.Type)
		}
		if cfg.Queue.Size != 100 {
			t.Errorf("expected queue size 100, got %d", cfg.Queue.Size)
		}
		if cfg.Queue.Workers != 4 {
			t.Errorf("expected queue workers 4, got %d", cfg.Queue.Workers)
		}
		if cfg.Queue.Timeout != 6*time.Second {
			t.Errorf("expected queue timeout 6s, got %v", cfg.Queue.Timeout)
		}
		if cfg.Queue.Redis.Addr != "localhost:6379" {
			t.Errorf("expected redis addr localhost:6379, got %s", cfg.Queue.Redis.Addr)
		}
		if cfg.Queue.Redis.Password != "redis-pass" {
			t.Errorf("expected redis password redis-pass, got %s", cfg.Queue.Redis.Password)
		}
		if cfg.Queue.Redis.DB != 1 {
			t.Errorf("expected redis db 1, got %d", cfg.Queue.Redis.DB)
		}
		if cfg.Queue.Redis.Stream != "herald-stream" {
			t.Errorf("expected redis stream herald-stream, got %s", cfg.Queue.Redis.Stream)
		}
		if cfg.Queue.Redis.Group != "herald-group" {
			t.Errorf("expected redis group herald-group, got %s", cfg.Queue.Redis.Group)
		}
		if cfg.Retry.Max != 5 {
			t.Errorf("expected retry max 5, got %d", cfg.Retry.Max)
		}
		if cfg.Retry.Backoff != "fixed" {
			t.Errorf("expected retry backoff fixed, got %s", cfg.Retry.Backoff)
		}
		if cfg.Retry.InitialDelay != 2*time.Second {
			t.Errorf("expected retry initial delay 2s, got %v", cfg.Retry.InitialDelay)
		}
		if cfg.Retry.MaxDelay != 3*time.Minute {
			t.Errorf("expected retry max delay 3m, got %v", cfg.Retry.MaxDelay)
		}
		if !cfg.Dedup.Enabled {
			t.Error("expected dedup enabled")
		}
		if cfg.Dedup.Window != 10*time.Minute {
			t.Errorf("expected dedup window 10m, got %v", cfg.Dedup.Window)
		}
		tmpl, ok := cfg.Templates["server_alert"]
		if !ok {
			t.Fatal("expected server_alert template")
		}
		if tmpl.Name != "server alert" {
			t.Errorf("expected template name server alert, got %s", tmpl.Name)
		}
		if tmpl.Level != "error" {
			t.Errorf("expected template level error, got %s", tmpl.Level)
		}
		if len(tmpl.Fields) != 1 || tmpl.Fields[0].Label != "server" {
			t.Errorf("expected template fields [server], got %v", tmpl.Fields)
		}
	})
}

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Server.Addr != ":8080" {
		t.Errorf("expected server addr :8080, got %s", cfg.Server.Addr)
	}
	if cfg.Server.Timeout != 30*time.Second {
		t.Errorf("expected server timeout 30s, got %v", cfg.Server.Timeout)
	}
	if cfg.WebSocket.Addr != ":8081" {
		t.Errorf("expected websocket addr :8081, got %s", cfg.WebSocket.Addr)
	}
	if cfg.WebSocket.PingTimeout != 30*time.Second {
		t.Errorf("expected websocket ping timeout 30s, got %v", cfg.WebSocket.PingTimeout)
	}
	if cfg.Worker.HeartbeatInterval != 20*time.Second {
		t.Errorf("expected worker heartbeat interval 20s, got %v", cfg.Worker.HeartbeatInterval)
	}
	if cfg.Worker.ReconnectDelay != 5*time.Second {
		t.Errorf("expected worker reconnect delay 5s, got %v", cfg.Worker.ReconnectDelay)
	}
	if cfg.Auth.Enabled {
		t.Error("expected auth disabled")
	}
	if cfg.Auth.AdminUser["admin"] != "admin" {
		t.Errorf("expected admin user admin=admin, got %v", cfg.Auth.AdminUser)
	}
	if cfg.Queue.Type != "memory" {
		t.Errorf("expected queue type memory, got %s", cfg.Queue.Type)
	}
	if cfg.Queue.Size != 10000 {
		t.Errorf("expected queue size 10000, got %d", cfg.Queue.Size)
	}
	if cfg.Queue.Timeout != 5*time.Second {
		t.Errorf("expected queue timeout 5s, got %v", cfg.Queue.Timeout)
	}
	if cfg.Retry.Max != 3 {
		t.Errorf("expected retry max 3, got %d", cfg.Retry.Max)
	}
	if !cfg.Dedup.Enabled {
		t.Error("expected dedup enabled")
	}
	if cfg.Dedup.Window != 5*time.Minute {
		t.Errorf("expected dedup window 5m, got %v", cfg.Dedup.Window)
	}
	if len(cfg.Routes["error"]) != 0 {
		t.Errorf("expected empty error route, got %v", cfg.Routes["error"])
	}
}

func TestExpandEnv(t *testing.T) {
	t.Run("expands env variable", func(t *testing.T) {
		t.Setenv("HERALD_TEST_TOKEN", "tok123")
		cfg := &Config{
			Providers: map[string]ProviderConfig{
				"webhook": {
					Type: "webhook",
					Config: map[string]interface{}{
						"url": "$HERALD_TEST_TOKEN",
					},
				},
			},
		}
		cfg.ExpandEnv()
		if cfg.Providers["webhook"].Config["url"] != "tok123" {
			t.Errorf("expected url tok123, got %v", cfg.Providers["webhook"].Config["url"])
		}
	})

	t.Run("unset env variable unchanged", func(t *testing.T) {
		cfg := &Config{
			Providers: map[string]ProviderConfig{
				"webhook": {
					Type: "webhook",
					Config: map[string]interface{}{
						"url": "$HERALD_UNSET_VAR",
					},
				},
			},
		}
		cfg.ExpandEnv()
		if cfg.Providers["webhook"].Config["url"] != "$HERALD_UNSET_VAR" {
			t.Errorf("expected url unchanged, got %v", cfg.Providers["webhook"].Config["url"])
		}
	})

	t.Run("non env prefix unchanged", func(t *testing.T) {
		cfg := &Config{
			Providers: map[string]ProviderConfig{
				"webhook": {
					Type: "webhook",
					Config: map[string]interface{}{
						"method": "POST",
					},
				},
			},
		}
		cfg.ExpandEnv()
		if cfg.Providers["webhook"].Config["method"] != "POST" {
			t.Errorf("expected method POST, got %v", cfg.Providers["webhook"].Config["method"])
		}
	})

	t.Run("empty string unchanged", func(t *testing.T) {
		cfg := &Config{
			Providers: map[string]ProviderConfig{
				"webhook": {
					Config: map[string]interface{}{
						"url": "",
					},
				},
			},
		}
		cfg.ExpandEnv()
		if cfg.Providers["webhook"].Config["url"] != "" {
			t.Errorf("expected empty url, got %v", cfg.Providers["webhook"].Config["url"])
		}
	})

	t.Run("non string value unchanged", func(t *testing.T) {
		cfg := &Config{
			Providers: map[string]ProviderConfig{
				"email": {
					Type: "email",
					Config: map[string]interface{}{
						"port": 587,
					},
				},
			},
		}
		cfg.ExpandEnv()
		if cfg.Providers["email"].Config["port"] != 587 {
			t.Errorf("expected port 587, got %v", cfg.Providers["email"].Config["port"])
		}
	})

	t.Run("nil config initialized", func(t *testing.T) {
		cfg := &Config{
			Providers: map[string]ProviderConfig{
				"log": {
					Type:   "log",
					Config: nil,
				},
			},
		}
		cfg.ExpandEnv()
		if cfg.Providers["log"].Config == nil {
			t.Error("expected non-nil config map")
		}
	})
}

func TestValidate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := Default()
		cfg.Auth.Enabled = true
		cfg.Auth.APIKeys["admin"] = "secret"
		cfg.Providers["webhook"] = ProviderConfig{Type: "webhook"}
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("missing server addr", func(t *testing.T) {
		cfg := Default()
		cfg.Server.Addr = ""
		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for missing server addr")
		}
	})

	t.Run("auth enabled without api keys", func(t *testing.T) {
		cfg := Default()
		cfg.Auth.Enabled = true
		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for auth enabled without api keys")
		}
	})

	t.Run("provider missing type", func(t *testing.T) {
		cfg := Default()
		cfg.Providers["log"] = ProviderConfig{Type: ""}
		err := cfg.Validate()
		if err == nil {
			t.Error("expected error for provider missing type")
		}
	})
}

func TestToQueueConfig(t *testing.T) {
	cfg := QueueConfig{
		Type:    "redis",
		Size:    100,
		Timeout: 6 * time.Second,
		Redis: RedisConfig{
			Addr:     "localhost:6379",
			Password: "pass",
			DB:       2,
			Stream:   "stream",
			Group:    "group",
		},
	}
	qc := cfg.ToQueueConfig()
	if qc.Type != "redis" {
		t.Errorf("expected type redis, got %s", qc.Type)
	}
	if qc.Size != 100 {
		t.Errorf("expected size 100, got %d", qc.Size)
	}
	if qc.Timeout != 6*time.Second {
		t.Errorf("expected timeout 6s, got %v", qc.Timeout)
	}
	if qc.Redis.Addr != "localhost:6379" {
		t.Errorf("expected redis addr localhost:6379, got %s", qc.Redis.Addr)
	}
	if qc.Redis.Password != "pass" {
		t.Errorf("expected redis password pass, got %s", qc.Redis.Password)
	}
	if qc.Redis.DB != 2 {
		t.Errorf("expected redis db 2, got %d", qc.Redis.DB)
	}
	if qc.Redis.Stream != "stream" {
		t.Errorf("expected redis stream stream, got %s", qc.Redis.Stream)
	}
	if qc.Redis.Group != "group" {
		t.Errorf("expected redis group group, got %s", qc.Redis.Group)
	}
}
