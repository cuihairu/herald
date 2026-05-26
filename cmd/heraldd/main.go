package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/cuihairu/herald/api"
	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/internal/config"
	"github.com/cuihairu/herald/internal/logger"
	builtinregistry "github.com/cuihairu/herald/providers/builtin/registry"
)

var (
	configPath = flag.String("config", "config.yaml", "config file path")
)

func main() {
	flag.Parse()

	// Load config
	cfg, err := config.Load(*configPath)
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
	q, err := queue.NewQueue(&core.QueueConfig{
		Type:    cfg.Queue.Type,
		Size:    cfg.Queue.Size,
		Timeout: cfg.Queue.Timeout,
	})
	if err != nil {
		logger.Error("failed to create queue", "error", err)
		os.Exit(1)
	}
	defer q.Close()

	// Create router
	router := route.NewRouter(&route.Config{
		Routes: cfg.Routes,
	})

	// Create runtime manager
	manager := runtime.NewManager(10000) // Store up to 10000 logs

	// Register builtin provider factories
	builtinregistry.RegisterBuiltinProviders(manager)

	// Initialize providers from config
	for name, providerCfg := range cfg.Providers {
		provider, err := manager.CreateProvider(name, providerCfg.Config)
		if err != nil {
			logger.Error("failed to create provider", "name", name, "error", err)
			os.Exit(1)
		}
		if err := manager.RegisterProvider(provider); err != nil {
			logger.Error("failed to register provider", "name", name, "error", err)
			os.Exit(1)
		}
		logger.Info("provider registered", "name", name, "type", provider.Type())
	}

	// Create dedup
	var d *dedup.Dedup
	if cfg.Dedup.Enabled {
		d = dedup.NewDedup(&dedup.Config{
			Window: cfg.Dedup.Window,
		})
	}

	// Create auth
	a := auth.New(&auth.Config{
		Enabled: cfg.Auth.Enabled,
		APIKeys: cfg.Auth.APIKeys,
	})

	// Create template manager and load templates from config
	templateMgr := template.NewManager()
	if len(cfg.Templates) > 0 {
		if err := templateMgr.LoadFromMap(cfg.Templates); err != nil {
			logger.Error("failed to load templates", "error", err)
			os.Exit(1)
		}
		logger.Info("templates loaded", "count", len(cfg.Templates))
	}

	// Create server
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

	// Start server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := srv.Start(ctx); err != nil {
			logger.Error("server error", "error", err)
			cancel()
		}
	}()

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// Shutdown
	logger.Info("shutting down...")
	ctx, cancel = context.WithTimeout(context.Background(), cfg.Server.Timeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}

	if err := manager.Close(ctx); err != nil {
		logger.Error("runtime close error", "error", err)
	}

	logger.Info("shutdown complete")
}
