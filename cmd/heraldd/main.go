package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	stdruntime "runtime"
	"strings"
	"syscall"
	"time"

	"github.com/cuihairu/herald/api"
	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/rules"
	coreruntime "github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/websocket"
	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/internal/logger"
	"github.com/cuihairu/herald/protocol"
	builtinregistry "github.com/cuihairu/herald/providers/builtin/registry"
	gws "github.com/gorilla/websocket"
)

func main() {
	os.Exit(run(os.Args))
}

// run dispatches on the command name and returns the process exit code.
func run(args []string) int {
	if len(args) < 2 {
		fmt.Println("Usage: heraldd <command> [options]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  serve    Start as scheduler (default mode)")
		fmt.Println("  worker   Start as remote worker")
		fmt.Println()
		return 1
	}

	switch args[1] {
	case "serve":
		return serveCmd(args[2:])
	case "worker":
		return workerCmd(args[2:])
	default:
		fmt.Printf("Unknown command: %s\n\n", args[1])
		fmt.Println("Commands: serve, worker")
		return 1
	}
}

// serveCmd runs Herald in scheduler mode: API + Queue + local workers.
// It returns the process exit code.
func serveCmd(args []string) int {
	configPath := parseFlags("serve", args)

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		return 1
	}
	cfg.ExpandEnv()
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		return 1
	}

	// Create queue
	q, err := queue.NewQueue(cfg.Queue.ToQueueConfig())
	if err != nil {
		logger.Error("failed to create queue", "error", err)
		return 1
	}
	defer func() { _ = q.Close() }()

	// Create router
	router := route.NewRouter(&route.Config{Routes: cfg.Routes})

	// Create runtime manager with retry
	retryCfg := &retry.Config{
		Max:          cfg.Retry.Max,
		Backoff:      cfg.Retry.Backoff,
		InitialDelay: cfg.Retry.InitialDelay,
		MaxDelay:     cfg.Retry.MaxDelay,
	}
	manager := coreruntime.NewManager(10000, retryCfg)

	// Register builtin provider factories
	builtinregistry.RegisterBuiltinProviders(manager)

	// Initialize providers from config
	for name, providerCfg := range cfg.Providers {
		provider, err := manager.CreateProvider(providerCfg.Type, providerCfg.Config)
		if err != nil {
			logger.Error("failed to create provider", "name", name, "error", err)
			return 1
		}
		enabled := true
		if providerCfg.Enabled != nil {
			enabled = *providerCfg.Enabled
		}
		if err := manager.RegisterProvider(name, provider, enabled); err != nil {
			logger.Error("failed to register provider", "name", name, "error", err)
			return 1
		}
		logger.Info("provider registered", "name", name, "type", provider.Type(), "enabled", enabled)
		if providerCfg.RateLimit != nil {
			if err := manager.SetProviderLimiter(name, providerCfg.RateLimit); err != nil {
				logger.Error("failed to configure rate limit", "name", name, "error", err)
				return 1
			}
		}
	}

	// Create dedup
	var d *dedup.Dedup
	if cfg.Dedup.Enabled {
		d = dedup.NewDedup(&dedup.Config{Window: cfg.Dedup.Window})
	}

	// Create auth
	a := auth.New(&auth.Config{
		Enabled: cfg.Auth.Enabled,
		APIKeys: cfg.Auth.APIKeys,
	})

	// Create template manager
	templateMgr := template.NewManager()
	if len(cfg.Templates) > 0 {
		if err := templateMgr.LoadFromMap(cfg.Templates); err != nil {
			logger.Error("failed to load templates", "error", err)
			return 1
		}
		logger.Info("templates loaded", "count", len(cfg.Templates))
	}

	// Create rule engine: store seeded from cfg.Rules (persistent JSON
	// file when rules_store is set), evaluated after dedup and before
	// routing; shadow hits are logged by the manager.
	var ruleStore rules.Store
	if cfg.RulesStore != "" {
		store, err := rules.NewFileStore(cfg.RulesStore)
		if err != nil {
			logger.Error("failed to open rules store", "path", cfg.RulesStore, "error", err)
			return 1
		}
		ruleStore = store
	} else {
		ruleStore = rules.NewMemoryStore()
	}
	rulesEngine := rules.NewEngine(ruleStore)
	// Rule state (for windows) defaults to in-process; redis shares it
	// across restarts and instances.
	if cfg.RulesState != nil && cfg.RulesState.Type != "" && cfg.RulesState.Type != "memory" {
		if cfg.RulesState.Type != "redis" {
			logger.Error("unknown rules_state type", "type", cfg.RulesState.Type)
			return 1
		}
		ss, err := rules.NewRedisStateStore(cfg.RulesState.Addr, cfg.RulesState.Password, cfg.RulesState.DB)
		if err != nil {
			logger.Error("failed to open rules state store", "error", err)
			return 1
		}
		rulesEngine.SetStateStore(ss)
	}
	for i := range cfg.Rules {
		rule := cfg.Rules[i]
		if err := rulesEngine.Put(context.Background(), &rule); err != nil {
			logger.Error("failed to load rule", "index", i, "error", err)
			return 1
		}
	}
	if len(cfg.Rules) > 0 {
		logger.Info("rules loaded", "count", len(cfg.Rules))
	}

	// Create worker registry and pool
	registry := worker.NewRegistry()
	pool := worker.NewPool(q, manager, registry, cfg.Queue.Workers)

	// Create API server
	srv := api.NewServer(&api.Config{
		Addr:            cfg.Server.Addr,
		Timeout:         cfg.Server.Timeout,
		Router:          router,
		Queue:           q,
		Runtime:         manager,
		Dedup:           d,
		Auth:            a,
		TemplateManager: templateMgr,
		WorkerRegistry:  registry,
		Rules:           rulesEngine,
	})

	// Create WebSocket server (management channel)
	wsServer := websocket.NewServer(&websocket.Config{
		Addr:           cfg.WebSocket.Addr,
		ReadTimeout:    cfg.WebSocket.ReadTimeout,
		WriteTimeout:   cfg.WebSocket.WriteTimeout,
		PingTimeout:    cfg.WebSocket.PingTimeout,
		PingInterval:   cfg.WebSocket.PingInterval,
		AllowedOrigins: cfg.WebSocket.AllowedOrigins,
	}, nil)
	hub := websocket.NewHub(wsServer, registry)
	wsServer.SetHandler(hub)
	srv.SetWebSocketServer(wsServer)

	// Start everything
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := srv.Start(ctx); err != nil {
			logger.Error("server error", "error", err)
			cancel()
		}
	}()

	go func() {
		if err := wsServer.Start(); err != nil {
			logger.Error("websocket server error", "error", err)
			cancel()
		}
	}()

	go pool.Run(ctx)

	logger.Info("herald scheduler started", "addr", cfg.Server.Addr, "workers", cfg.Queue.Workers)

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// Shutdown
	logger.Info("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.Timeout)
	defer shutdownCancel()

	_ = srv.Shutdown(shutdownCtx)
	_ = manager.Close(shutdownCtx)
	_ = wsServer.Stop()

	logger.Info("shutdown complete")
	return 0
}

// workerCmd runs Herald in remote worker mode: connects to queue + registers via WebSocket.
// It returns the process exit code.
func workerCmd(args []string) int {
	configPath := parseFlags("worker", args)

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		return 1
	}
	cfg.ExpandEnv()

	if cfg.Queue.Type == "memory" {
		logger.Error("remote worker requires a shared queue backend (redis/nats), got: memory")
		return 1
	}

	// Create queue (must be a shared backend like redis)
	q, err := queue.NewQueue(cfg.Queue.ToQueueConfig())
	if err != nil {
		logger.Error("failed to create queue", "error", err)
		return 1
	}
	defer func() { _ = q.Close() }()

	// Create runtime manager for local provider execution
	manager := coreruntime.NewManager(10000)
	builtinregistry.RegisterBuiltinProviders(manager)

	// Initialize providers from config (worker may have its own providers)
	for name, providerCfg := range cfg.Providers {
		provider, err := manager.CreateProvider(providerCfg.Type, providerCfg.Config)
		if err != nil {
			logger.Error("failed to create provider", "name", name, "error", err)
			return 1
		}
		if err := manager.RegisterProvider(name, provider, true); err != nil {
			logger.Error("failed to register provider", "name", name, "error", err)
			return 1
		}
		logger.Info("provider registered", "name", name, "type", provider.Type())
	}

	// Create worker registry
	registry := worker.NewRegistry()

	// Create worker pool consuming from shared queue
	pool := worker.NewPool(q, manager, registry, cfg.Queue.Workers)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register signal handlers before blocking on the pool so SIGTERM/SIGINT
	// trigger a graceful shutdown instead of the default process termination.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go runRemoteWorkerControlPlane(ctx, cfg, registry)
	poolDone := make(chan struct{})
	go func() {
		defer close(poolDone)
		pool.Run(ctx)
	}()

	logger.Info("remote worker starting", "queue_type", cfg.Queue.Type, "workers", cfg.Queue.Workers)

	<-sigCh

	logger.Info("remote worker shutting down...")
	cancel()
	<-poolDone
	logger.Info("shutdown complete")
	return 0
}

func parseFlags(command string, args []string) string {
	var configPath string
	switch command {
	case "serve", "worker":
		configPath = "config.yaml"
	default:
		configPath = "config.yaml"
	}

	// Simple flag parsing
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" || args[i] == "-c" {
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		}
	}

	return configPath
}

func runRemoteWorkerControlPlane(ctx context.Context, cfg *config.Config, registry *worker.Registry) {
	wsURL := buildWorkerWebSocketURL(cfg)
	if wsURL == "" {
		logger.Warn("remote worker websocket disabled: worker.server_url is not configured")
		return
	}

	workerID := cfg.Worker.ID
	if workerID == "" {
		workerID = fmt.Sprintf("worker-%d", time.Now().UnixNano())
	}

	reconnectDelay := cfg.Worker.ReconnectDelay
	if reconnectDelay <= 0 {
		reconnectDelay = 5 * time.Second
	}
	heartbeatInterval := cfg.Worker.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = 20 * time.Second
	}
	capabilities := cfg.Worker.Capabilities
	if len(capabilities) == 0 {
		capabilities = []string{"*"}
	}

	dialer := gws.Dialer{}
	for {
		if ctx.Err() != nil {
			return
		}

		conn, _, err := dialer.DialContext(ctx, wsURL, nil)
		if err != nil {
			logger.Error("remote worker websocket dial failed", "url", wsURL, "error", err)
			if !sleepOrDone(ctx, reconnectDelay) {
				return
			}
			continue
		}

		logger.Info("remote worker websocket connected", "worker_id", workerID, "url", wsURL)
		if err := registerRemoteWorker(conn, workerID, capabilities); err != nil {
			logger.Error("remote worker registration failed", "worker_id", workerID, "error", err)
			_ = conn.Close()
			if !sleepOrDone(ctx, reconnectDelay) {
				return
			}
			continue
		}

		_ = registry.Register(&worker.Info{
			ID:           workerID,
			Mode:         worker.Remote,
			Capabilities: capabilities,
		})

		done := make(chan struct{})
		go func() {
			defer close(done)
			readRemoteWorkerMessages(ctx, conn)
		}()

		hbErr := heartbeatRemoteWorker(ctx, conn, workerID, heartbeatInterval, registry)
		_ = conn.Close()
		<-done
		registry.Deregister(workerID)

		if ctx.Err() != nil {
			return
		}
		if hbErr != nil {
			logger.Error("remote worker control plane disconnected", "worker_id", workerID, "error", hbErr)
		}
		if !sleepOrDone(ctx, reconnectDelay) {
			return
		}
	}
}

func buildWorkerWebSocketURL(cfg *config.Config) string {
	if cfg.Worker.ServerURL != "" {
		return strings.TrimRight(cfg.Worker.ServerURL, "/")
	}
	if cfg.WebSocket.Addr == "" {
		return ""
	}
	addr := cfg.WebSocket.Addr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	u := url.URL{Scheme: "ws", Host: addr, Path: "/worker"}
	return u.String()
}

func registerRemoteWorker(conn *gws.Conn, workerID string, capabilities []string) error {
	msg := &protocol.RegisterMessage{
		WorkerID:     workerID,
		Mode:         string(worker.Remote),
		Platform:     stdruntime.GOOS,
		Version:      "dev",
		Capabilities: capabilities,
	}
	payload, err := protocol.MarshalMessage(msg)
	if err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if err := conn.WriteMessage(gws.TextMessage, payload); err != nil {
		return err
	}

	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	var ack protocol.RegisterAckMessage
	if err := json.Unmarshal(data, &ack); err != nil {
		return err
	}
	if !ack.Success {
		return fmt.Errorf("register rejected: %s", ack.Error)
	}
	return conn.SetReadDeadline(time.Time{})
}

func heartbeatRemoteWorker(ctx context.Context, conn *gws.Conn, workerID string, interval time.Duration, registry *worker.Registry) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			msg := &protocol.HeartbeatMessage{
				WorkerID:  workerID,
				Timestamp: time.Now().Unix(),
			}
			payload, err := protocol.MarshalMessage(msg)
			if err != nil {
				return err
			}
			if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return err
			}
			if err := conn.WriteMessage(gws.TextMessage, payload); err != nil {
				return err
			}
			_ = registry.Heartbeat(workerID)
		}
	}
}

func readRemoteWorkerMessages(ctx context.Context, conn *gws.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
