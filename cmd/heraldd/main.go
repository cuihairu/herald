package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cuihairu/herald/api"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/dispatch"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/core/websocket"
	"github.com/cuihairu/herald/internal/config"
	"github.com/cuihairu/herald/internal/logger"
	builtinregistry "github.com/cuihairu/herald/providers/builtin/registry"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: heraldd <command> [options]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  serve    Start as scheduler (default mode)")
		fmt.Println("  worker   Start as remote worker")
		fmt.Println()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		serveCmd(os.Args[2:])
	case "worker":
		workerCmd(os.Args[2:])
	default:
		fmt.Printf("Unknown command: %s\n\n", os.Args[1])
		fmt.Println("Commands: serve, worker")
		os.Exit(1)
	}
}

// serveCmd runs Herald in scheduler mode: API + Queue + local workers
func serveCmd(args []string) {
	configPath := parseFlags("serve", args)

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	cfg.ExpandEnv()
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(1)
	}

	// Create queue
	q, err := queue.NewQueue(&queue.QueueConfig{
		Type:    cfg.Queue.Type,
		Size:    cfg.Queue.Size,
		Timeout: cfg.Queue.Timeout,
	})
	if err != nil {
		logger.Error("failed to create queue", "error", err)
		os.Exit(1)
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
	manager := runtime.NewManager(10000, retryCfg)

	// Register builtin provider factories
	builtinregistry.RegisterBuiltinProviders(manager)

	// Initialize providers from config
	for name, providerCfg := range cfg.Providers {
		provider, err := manager.CreateProvider(providerCfg.Type, providerCfg.Config)
		if err != nil {
			logger.Error("failed to create provider", "name", name, "error", err)
			os.Exit(1)
		}
		enabled := true
		if providerCfg.Enabled != nil {
			enabled = *providerCfg.Enabled
		}
		if err := manager.RegisterProvider(name, provider, enabled); err != nil {
			logger.Error("failed to register provider", "name", name, "error", err)
			os.Exit(1)
		}
		logger.Info("provider registered", "name", name, "type", provider.Type(), "enabled", enabled)
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
			os.Exit(1)
		}
		logger.Info("templates loaded", "count", len(cfg.Templates))
	}

	// Create worker registry and pool
	registry := worker.NewRegistry()
	dispatcher := dispatch.New(q, manager, registry, cfg.Queue.Workers)

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
	})

	// Create WebSocket server (management channel)
	wsServer := websocket.NewServer(&websocket.Config{
		Addr:         cfg.WebSocket.Addr,
		ReadTimeout:  cfg.WebSocket.ReadTimeout,
		WriteTimeout: cfg.WebSocket.WriteTimeout,
		PingTimeout:  cfg.WebSocket.PingTimeout,
		PingInterval: cfg.WebSocket.PingInterval,
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

	go dispatcher.Run(ctx)

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
}

// workerCmd runs Herald in remote worker mode: connects to queue + registers via WebSocket
func workerCmd(args []string) {
	configPath := parseFlags("worker", args)

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	cfg.ExpandEnv()

	if cfg.Queue.Type == "memory" {
		logger.Error("remote worker requires a shared queue backend (redis/nats), got: memory")
		os.Exit(1)
	}

	// Create queue (must be a shared backend like redis)
	q, err := queue.NewQueue(&queue.QueueConfig{
		Type:    cfg.Queue.Type,
		Size:    cfg.Queue.Size,
		Timeout: cfg.Queue.Timeout,
	})
	if err != nil {
		logger.Error("failed to create queue", "error", err)
		os.Exit(1)
	}
	defer func() { _ = q.Close() }()

	// Create runtime manager for local provider execution
	manager := runtime.NewManager(10000)
	builtinregistry.RegisterBuiltinProviders(manager)

	// Initialize providers from config (worker may have its own providers)
	for name, providerCfg := range cfg.Providers {
		provider, err := manager.CreateProvider(providerCfg.Type, providerCfg.Config)
		if err != nil {
			logger.Error("failed to create provider", "name", name, "error", err)
			os.Exit(1)
		}
		if err := manager.RegisterProvider(name, provider, true); err != nil {
			logger.Error("failed to register provider", "name", name, "error", err)
			os.Exit(1)
		}
		logger.Info("provider registered", "name", name, "type", provider.Type())
	}

	// Create worker registry
	registry := worker.NewRegistry()

	// Create worker pool consuming from shared queue
	pool := worker.NewPool(q, manager, registry, cfg.Queue.Workers)

	// TODO: Register with scheduler via WebSocket
	logger.Info("remote worker starting", "queue_type", cfg.Queue.Type, "workers", cfg.Queue.Workers)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Run(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("remote worker shutting down...")
	cancel()
	logger.Info("shutdown complete")
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
