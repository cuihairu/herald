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
	"github.com/cuihairu/herald/core/ack"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/audit"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/digest"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/feeds"
	"github.com/cuihairu/herald/core/groups"
	"github.com/cuihairu/herald/core/incident"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/roster"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/rules"
	coreruntime "github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/websocket"
	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/internal/logger"
	"github.com/cuihairu/herald/protocol"
	builtinregistry "github.com/cuihairu/herald/providers/builtin/registry"
	"github.com/cuihairu/herald/providers/builtin/telegram"
	"github.com/cuihairu/herald/providers/builtin/wechatmp"
	gws "github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// main terminates the process with run's exit code; run() is the testable
// entry point. Returning (instead of exiting) on success lets a
// `go build -cover` binary flush its GOCOVERDIR profile, so the success
// path stays measurable end to end.
//
// Coverage note: `go test` never executes this function — a main function
// is only callable by the OS starting the process — so these statements
// stay at zero in the go-test profile. TestMainProcessSuccessPath measures
// them for real by running a `go build -cover` binary of this package as a
// child process and asserting main's statements in the child's
// GOCOVERDIR dump.
func main() {
	if code := run(os.Args); code != 0 {
		os.Exit(code)
	}
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

// registerProvider is a seam over Manager.RegisterProvider: the
// duplicate-name guards in the registration loops below cannot fire in
// production — factories and instances are separate namespaces and config
// map keys are unique — so tests swap this to make registration fail and
// exercise both error branches deterministically.
var registerProvider = (*coreruntime.Manager).RegisterProvider

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
	router := route.NewRouter(&route.Config{Routes: cfg.Routes, LevelRoutes: cfg.LevelRoutes, Channels: cfg.ChannelRoutes()})
	if len(cfg.Channels) > 0 {
		logger.Info("channels loaded", "count", len(cfg.Channels))
	}

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
		// Defensive: the duplicate-name guard cannot fire — builtins only
		// register factories, and each configured name is registered once.
		if err := registerProvider(manager, name, provider, enabled); err != nil {
			logger.Error("failed to register provider", "name", name, "error", err)
			return 1
		}
		logger.Info("provider registered", "name", name, "type", provider.Type(), "enabled", enabled)
		if providerCfg.RateLimit != nil {
			// The limiter factory falls back to token_bucket instead of
			// failing, so configuring a rate limit cannot error.
			_ = manager.SetProviderLimiter(name, providerCfg.RateLimit)
		}
	}

	// Create dedup
	var d *dedup.Dedup
	if cfg.Dedup.Enabled {
		d = dedup.NewDedup(&dedup.Config{
			Window:          cfg.Dedup.Window,
			CategoryTiers:   cfg.Dedup.CategoryTiers,
			CategoryWindows: cfg.Dedup.CategoryWindows,
		})
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
	// The default policy decides the fate of notifications no active rule
	// matched: allow (the default) keeps static routing, deny withholds.
	switch cfg.RulesDefaultPolicy {
	case "", string(rules.PolicyAllow):
		rulesEngine.SetDefaultPolicy(rules.PolicyAllow)
	case string(rules.PolicyDeny):
		rulesEngine.SetDefaultPolicy(rules.PolicyDeny)
	default:
		logger.Error("unknown rules_default_policy", "policy", cfg.RulesDefaultPolicy)
		return 1
	}
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

	// Create notification groups: named audiences that "group:" channel
	// references expand to; persisted via groups_store, resolved from the
	// in-memory live table.
	var groupStore groups.Store
	if cfg.GroupsStore != "" {
		store, err := groups.NewFileStore(cfg.GroupsStore)
		if err != nil {
			logger.Error("failed to open groups store", "path", cfg.GroupsStore, "error", err)
			return 1
		}
		groupStore = store
	}
	groupsManager := groups.NewManager(groupStore)
	if groupStore != nil {
		if err := groupsManager.Reload(context.Background()); err != nil {
			logger.Error("failed to load groups", "error", err)
			return 1
		}
	}
	for i := range cfg.Groups {
		group := cfg.Groups[i]
		if err := groupsManager.Put(context.Background(), &group); err != nil {
			logger.Error("failed to load group", "index", i, "error", err)
			return 1
		}
	}
	if len(cfg.Groups) > 0 {
		logger.Info("groups loaded", "count", len(cfg.Groups))
	}

	// Create the user-level audience tables: "user:" channel references
	// resolve through audiences/recipients to provider endpoints. The MVP
	// is config-only — unlike groups there is no runtime API yet. An
	// invalid table is configuration drift and refuses to start.
	audienceManager, err := audience.NewManager(cfg.Audiences, cfg.Recipients)
	if err != nil {
		logger.Error("failed to load audiences/recipients", "error", err)
		return 1
	}
	if len(cfg.Audiences) > 0 || len(cfg.Recipients) > 0 {
		logger.Info("audiences loaded", "audiences", len(cfg.Audiences), "recipients", len(cfg.Recipients))
	}

	// Create duty rosters: externally pushed schedules (值班表) that
	// silence rules can name; persisted via rosters_store, read by the
	// silence gate from the in-memory live table.
	var rosterStore roster.Store
	if cfg.RostersStore != "" {
		store, err := roster.NewFileStore(cfg.RostersStore)
		if err != nil {
			logger.Error("failed to open rosters store", "path", cfg.RostersStore, "error", err)
			return 1
		}
		rosterStore = store
	}
	rostersManager := roster.NewManager(rosterStore)
	if rosterStore != nil {
		if err := rostersManager.Reload(context.Background()); err != nil {
			logger.Error("failed to load rosters", "error", err)
			return 1
		}
	}

	// Create worker registry and pool
	registry := worker.NewRegistry()
	pool := worker.NewPool(q, manager, registry, cfg.Queue.Workers)

	// Ack store and escalation manager share the alert identity space:
	// the ack arriving in time cancels the pending upgrade. The incident
	// ledger records the episodes those alerts live through.
	ackStore := ack.NewMemoryStore()
	escalations := escalation.NewManager(ackStore, nil, cfg.EscalationStore)
	incidents := incident.New(cfg.IncidentLimit)

	// Digest aggregator (关系详设 §10): built here so the API server
	// serves with folding active from the first request. Invalid
	// schedules are configuration errors and refuse to start.
	var digestAgg *digest.Aggregator
	var digestLock *digest.LeaderLock
	if cfg.Digest.Enabled {
		// The flip timezone defaults to Asia/Shanghai (关系详设 §10).
		loc, err := time.LoadLocation(orDefault(cfg.Digest.Location, "Asia/Shanghai"))
		if err != nil {
			logger.Error("digest location invalid", "error", err)
			return 1
		}
		dailySpec, err := digest.ParseDaily(orDefault(cfg.Digest.Daily, "09:00"), loc)
		if err != nil {
			logger.Error("digest daily schedule invalid", "error", err)
			return 1
		}
		weeklySpec, err := digest.ParseWeekly(orDefault(cfg.Digest.Weekly, "Mon 09:00"), loc)
		if err != nil {
			logger.Error("digest weekly schedule invalid", "error", err)
			return 1
		}
		digestAgg = digest.NewAggregator(dailySpec, weeklySpec)
		if cfg.Digest.RedisAddr != "" {
			client := redis.NewClient(&redis.Options{
				Addr:     cfg.Digest.RedisAddr,
				Password: cfg.Digest.RedisPassword,
				DB:       cfg.Digest.RedisDB,
			})
			if err := client.Ping(context.Background()).Err(); err != nil {
				logger.Error("digest redis lease unavailable", "error", err)
				return 1
			}
			leaseTTL := cfg.Digest.LeaseTTL
			if leaseTTL <= 0 {
				leaseTTL = time.Minute
			}
			digestLock = digest.NewLeaderLock(client, "herald:digest:leader", leaseTTL)
		}
	}

	// The audience registries (关系详设 §2/§3) are shared by every
	// 触发面/拉取面 feature: feeds resolve private tokens and check
	// relations at read time, source entries write follows/unfollows,
	// the reconciler sweeps. One pair, always constructed; the audit
	// trail attaches once either half is on, so every registry change
	// from then on answers 谁在何时通过哪条入口改了什么.
	surfaces := audience.NewSurfaceRegistry()
	relations := audience.NewRegistry()
	if cfg.Feeds.Enabled || cfg.Sources.Enabled {
		auditStore := audit.New(0)
		surfaces.SetRecorder(auditStore)
		relations.SetRecorder(auditStore)
	}

	// RSS pull channel (§9, 边界审计 §4): the store receives pull
	// projections when an rss-classified channel is routed; the API
	// serves them at /feeds. The registries populate once the 触发面
	// issue bindings and subscriptions — until then private feeds answer
	// not-found and public feeds carry anonymous items only.
	var feedStore *feeds.Store
	feedMeta := feeds.ChannelMeta{Title: "Herald 通知"}
	if cfg.Feeds.Enabled {
		feedStore = feeds.NewStore(cfg.Feeds.MaxItems)
		feedMeta = feeds.ChannelMeta{
			Title:       orDefault(cfg.Feeds.Title, "Herald 通知"),
			Link:        cfg.Feeds.Link,
			Description: cfg.Feeds.Description,
		}
	}

	// 来源适配器 (关系详设 §8): platform entries converge external
	// follow/unfollow/check actions onto the registries above. The bot
	// and MP endpoints open only when their secret/token is configured —
	// an empty credential keeps that endpoint 404 even with enabled: true.
	var sourceAdapter *audience.SourceAdapter
	if cfg.Sources.Enabled {
		sourceAdapter = audience.NewSourceAdapter(surfaces, relations)
		if cfg.Sources.Bot.Secret != "" {
			logger.Info("bot source entry enabled")
		}
		if cfg.Sources.WeChatMP.Token != "" {
			logger.Info("wechat-mp source entry enabled")
		}
	}

	// External-state reconciler (rule 3 对账): probes are built from the
	// configured providers — a provider skipped via enabled: false never
	// gets probed, and a probe whose config cannot build (missing token)
	// is configuration drift and refuses to start. The redis lease
	// (锁选主, digest's connection settings) keeps a multi-instance fleet
	// sweeping once per round; without redis_addr the loop runs
	// unlocked, assuming a single instance.
	var reconciler *audience.Reconciler
	var reconcileLock *digest.LeaderLock
	if sourceAdapter != nil && cfg.Sources.Reconcile.Enabled {
		var probes []audience.SurfaceProbe
		for name, providerCfg := range cfg.Providers {
			if providerCfg.Enabled != nil && !*providerCfg.Enabled {
				continue
			}
			var probe audience.SurfaceProbe
			var err error
			switch providerCfg.Type {
			case "telegram":
				probe, err = telegram.NewProbe(providerCfg.Config)
			case "wechatmp":
				probe, err = wechatmp.NewProbe(providerCfg.Config)
			default:
				continue
			}
			if err != nil {
				logger.Error("failed to build source reconcile probe", "provider", name, "type", providerCfg.Type, "error", err)
				return 1
			}
			probes = append(probes, probe)
		}
		reconciler = audience.NewReconciler(surfaces, relations, probes...)
		if len(reconciler.Channels()) == 0 {
			logger.Warn("source reconcile enabled but no probeable provider (telegram/wechatmp) is configured")
		}
		if cfg.Digest.RedisAddr != "" {
			client := redis.NewClient(&redis.Options{
				Addr:     cfg.Digest.RedisAddr,
				Password: cfg.Digest.RedisPassword,
				DB:       cfg.Digest.RedisDB,
			})
			if err := client.Ping(context.Background()).Err(); err != nil {
				logger.Error("source reconcile redis lease unavailable", "error", err)
				return 1
			}
			leaseTTL := cfg.Sources.Reconcile.LeaseTTL
			if leaseTTL <= 0 {
				leaseTTL = time.Minute
			}
			reconcileLock = digest.NewLeaderLock(client, "herald:sources:reconcile:leader", leaseTTL)
		}
	}

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
		Groups:          groupsManager,
		Users:           audienceManager,
		Rosters:         rostersManager,
		AckStore:        ackStore,
		Escalation:      escalations,
		Incidents:       incidents,
		Digest:          digestAgg,
		DigestPrefs:     audience.NewPreferenceRegistry(),
		Feeds:           feedStore,
		FeedMeta:        feedMeta,
		FeedSurfaces:    surfaces,
		FeedRelations:   relations,
		Sources:         sourceAdapter,
		SourceSurfaces:  surfaces,
		SourceBot: api.BotSourceConfig{
			Secret:   cfg.Sources.Bot.Secret,
			Defaults: cfg.Sources.Bot.DefaultCategories,
		},
		SourceWeChatMP: api.WeChatMPSourceConfig{
			Token:    cfg.Sources.WeChatMP.Token,
			Defaults: cfg.Sources.WeChatMP.DefaultCategories,
		},
	})
	// Feishu card-callback encryption key (optional): acknowledge buttons
	// on interactive cards ack alerts through the same stores the ack API
	// uses.
	if cfg.CardCallback != nil && cfg.CardCallback.EncryptKey != "" {
		srv.SetCardCallbackKey(cfg.CardCallback.EncryptKey)
	}

	// Re-arm pending upgrades from the previous run. A failure here must
	// not keep the alert system down — pending upgrades are an add-on —
	// so it is logged and the in-memory table starts empty.
	if err := escalations.Restore(context.Background()); err != nil {
		logger.Error("failed to restore pending escalations", "error", err)
	}

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
		// Start launches ListenAndServe in its own goroutine and always
		// returns nil; bind failures are logged inside Start.
		_ = wsServer.Start()
	}()

	go pool.Run(ctx)

	// Digest flip loop (关系详设 §10): the aggregator was wired into the
	// API server above; this loop flips due windows. The redis lease
	// (锁选主) keeps multi-instance fleets flipping each window exactly
	// once; without redis_addr the loop runs unlocked, assuming a single
	// instance.
	if digestAgg != nil {
		interval := cfg.Digest.Interval
		if interval <= 0 {
			interval = time.Minute
		}
		go digest.FlipLoop(ctx, digestAgg, digestLock, interval, flushDigest(srv))
		logger.Info("digest flip loop started", "daily", orDefault(cfg.Digest.Daily, "09:00"),
			"weekly", orDefault(cfg.Digest.Weekly, "Mon 09:00"), "interval", interval,
			"leader_lease", digestLock != nil)
	}

	// Source reconcile loop (rule 3 对账): sweeps active surfaces against
	// the platforms on a schedule. The redis lease (锁选主) keeps a
	// multi-instance fleet sweeping exactly once per round; probe errors
	// skip — 对账 never guesses.
	if reconciler != nil {
		interval := cfg.Sources.Reconcile.Interval
		if interval <= 0 {
			interval = time.Hour
		}
		go reconcileLoop(ctx, reconciler, reconcileLock, interval)
		logger.Info("source reconcile loop started", "channels", reconciler.Channels(),
			"interval", interval, "leader_lease", reconcileLock != nil)
	}

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
	// Stops upgrade timers; pending records stay persisted for the next
	// run to restore and judge.
	_ = escalations.Close()
	// The flip loop exits with ctx; an immediate lease handover keeps
	// another instance from idling until the lease lapses.
	if digestLock != nil {
		digestLock.Release(shutdownCtx)
	}
	// Same for the reconcile sweep's lease.
	if reconcileLock != nil {
		reconcileLock.Release(shutdownCtx)
	}

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
		logger.Error("remote worker requires a shared queue backend (redis), got: memory")
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
		// Defensive: the duplicate-name guard cannot fire — the manager is
		// fresh (see serveCmd), builtins only register factories, and each
		// configured name registers once.
		if err := registerProvider(manager, name, provider, true); err != nil {
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

// setReadDeadline is a package-level seam (same pattern as worker-sdk's
// writeControl): on a live conn SetReadDeadline forwards to net.Conn and only
// fails once the conn is closed, which WriteMessage would catch first — so
// without injecting a failing implementation the ack-read deadline error arm
// in registerRemoteWorker can never be executed.
var setReadDeadline = func(conn *gws.Conn, t time.Time) error { return conn.SetReadDeadline(t) }

func registerRemoteWorker(conn *gws.Conn, workerID string, capabilities []string) error {
	msg := &protocol.RegisterMessage{
		WorkerID:     workerID,
		Mode:         string(worker.Remote),
		Platform:     stdruntime.GOOS,
		Version:      "dev",
		Capabilities: capabilities,
	}
	// The register message has only concrete fields, so marshalling
	// cannot fail; gorilla's SetWriteDeadline only records the timestamp
	// and never touches the network.
	payload, _ := protocol.MarshalMessage(msg)
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(gws.TextMessage, payload); err != nil {
		return err
	}

	// A real SetReadDeadline cannot fail here (the conn is still open after
	// WriteMessage), but the failure arm is exercised through the
	// setReadDeadline seam; kept explicit so a contract change fails loudly.
	if err := setReadDeadline(conn, time.Now().Add(10*time.Second)); err != nil {
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
			// The heartbeat message has only concrete fields, so
			// marshalling cannot fail; gorilla's SetWriteDeadline only
			// records the timestamp.
			payload, _ := protocol.MarshalMessage(msg)
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
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

// digestFlusher is the slice of the API server the flip loop needs —
// a seam so the flush-error path is testable without a live server.
type digestFlusher interface {
	FlushDigestBatch(b digest.Batch) error
}

// flushDigest hands one flipped window to the delivery pipeline. The
// window has already advanced when the flush runs, so a failed flush
// drops the batch: it is logged loudly (the flip loop was the last
// place those events were held) and the next window proceeds.
func flushDigest(f digestFlusher) func(digest.Batch) {
	return func(b digest.Batch) {
		if err := f.FlushDigestBatch(b); err != nil {
			logger.Error("digest summary flush failed", "audience", b.Key.AudienceID, "category", b.Key.Category, "error", err)
		}
	}
}

// reconcileLoop sweeps active surfaces against the platforms on a
// schedule (rule 3 对账): the leader lease keeps a multi-instance fleet
// sweeping exactly once per round, and RunOnce skips any probe that
// cannot answer — 对账 never guesses a platform's state. A nil lock
// runs unlocked, like the digest flip loop without redis.
func reconcileLoop(ctx context.Context, rec *audience.Reconciler, lock *digest.LeaderLock, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if lock != nil && !lock.Acquire(ctx) && !lock.Renew(ctx) {
				continue
			}
			report := rec.RunOnce(ctx)
			logger.Info("source reconcile sweep", "channels", rec.Channels(),
				"checked", report.Checked, "corrected", report.Corrected,
				"skipped", report.Skipped, "errors", report.Errors)
		}
	}
}

// orDefault fills a blank config string with its fallback.
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
