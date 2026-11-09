package main

import (
	"context"
	"errors"
	"github.com/cuihairu/herald/api"
	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/ack"
	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/audit"
	"github.com/cuihairu/herald/core/auth"
	"github.com/cuihairu/herald/core/callback"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/digest"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/feeds"
	"github.com/cuihairu/herald/core/groups"
	"github.com/cuihairu/herald/core/incident"
	"github.com/cuihairu/herald/core/metrics"
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
	"github.com/cuihairu/herald/internal/store"
	builtinregistry "github.com/cuihairu/herald/providers/builtin/registry"
	"github.com/cuihairu/herald/providers/builtin/telegram"
	"github.com/cuihairu/herald/providers/builtin/wechatmp"
	"time"

	"github.com/redis/go-redis/v9"
)

// errServeSetup is the sentinel assembleServe returns on any failure: the
// specific diagnostic is already logged at the failure site (each return
// keeps its own logger.Error), and serveCmd only needs the boolean.
var errServeSetup = errors.New("serve setup failed")

// serveDeps holds every component assembled by assembleServe that the
// start/shutdown steps need. Components that are only inputs to the API or
// websocket wiring stay local to assembleServe and never escape.
type serveDeps struct {
	cfg            *config.Config
	q              core.Queue
	manager        *coreruntime.Manager
	pool           *worker.Pool
	srv            *api.Server
	wsServer       *websocket.Server
	metricsSampler *metrics.Sampler
	escalations    *escalation.Manager
	digestAgg      *digest.Aggregator
	digestLock     *digest.LeaderLock
	reconciler     *audience.Reconciler
	reconcileLock  *digest.LeaderLock

	// closers run in reverse order when the serve session ends or assembly
	// fails, so every opened resource (queue, credential store) is released
	// exactly once on either path.
	closers []func()
}

// closeAll runs the registered closers. Assembly registers a closer the
// moment it opens a resource, so a failure mid-assembly still releases
// everything created so far.
func (d *serveDeps) closeAll() {
	for i := len(d.closers) - 1; i >= 0; i-- {
		d.closers[i]()
	}
}

// fail releases every resource opened so far and reports the setup
// sentinel. Call at each assembly failure site after logging the specific
// diagnostic.
func (d *serveDeps) fail() (*serveDeps, error) {
	d.closeAll()
	return nil, errServeSetup
}

// assembleServe builds the queue, runtime manager, providers, stores, API
// server and websocket management channel from a validated config. Every
// refusal path logs its own diagnostic and returns errServeSetup via fail.
func assembleServe(cfg *config.Config) (*serveDeps, error) {
	deps := &serveDeps{cfg: cfg}
	// Create queue
	q, err := queue.NewQueue(cfg.Queue.ToQueueConfig())
	if err != nil {
		logger.Error("failed to create queue", "error", err)
		return deps.fail()
	}
	deps.q = q
	deps.closers = append(deps.closers, func() { _ = q.Close() })

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
	deps.manager = manager

	// Register builtin provider factories
	builtinregistry.RegisterBuiltinProviders(manager)

	// §13.5 webhook 回调: one dispatcher serves every app namespace —
	// destination, event and signature all travel on the task. Registering
	// before config providers run means a config entry claiming the same
	// name refuses startup (the duplicate guard fires in the loop below).
	callbackProvider := callback.NewProvider()
	if err := registerProvider(manager, callback.ProviderName, callbackProvider, true); err != nil {
		logger.Error("failed to register callback provider", "error", err)
		return deps.fail()
	}

	// Initialize providers from config
	for name, providerCfg := range cfg.Providers {
		provider, err := manager.CreateProvider(providerCfg.Type, providerCfg.Config)
		if err != nil {
			logger.Error("failed to create provider", "name", name, "error", err)
			return deps.fail()
		}
		enabled := true
		if providerCfg.Enabled != nil {
			enabled = *providerCfg.Enabled
		}
		// Defensive: the duplicate-name guard cannot fire — builtins only
		// register factories, and each configured name is registered once.
		if err := registerProvider(manager, name, provider, enabled); err != nil {
			logger.Error("failed to register provider", "name", name, "error", err)
			return deps.fail()
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

	// Create auth. AdminUser and SecretKey must ride along: the docs
	// promise config-file Dashboard accounts and a configured JWT key,
	// and dropping either makes /auth/login a permanent 401 (or falls
	// back to a random per-process key, so sessions die on restart).
	a := auth.New(&auth.Config{
		Enabled:    cfg.Auth.Enabled,
		APIKeys:    cfg.Auth.APIKeys,
		SecretKey:  cfg.Auth.SecretKey,
		AdminUser:  cfg.Auth.AdminUser,
		LoginRate:  cfg.Auth.LoginRate,
		LoginBurst: cfg.Auth.LoginBurst,
	})

	// Credential store (optional): dashboard-minted API keys and rotated
	// password hashes persist in sqlite when auth.store_path is set. The
	// rotated password wins over the config seed on the next start.
	var credStore *store.DB
	if cfg.Auth.StorePath != "" {
		cs, err := store.Open(cfg.Auth.StorePath)
		if err != nil {
			logger.Error("failed to open credential store", "path", cfg.Auth.StorePath, "error", err)
			return deps.fail()
		}
		deps.closers = append(deps.closers, func() { _ = cs.Close() })
		a.SetPasswordStore(cs)
		a.SetAPIKeyHashes(cs)
		credStore = cs
	}

	// Create template manager
	templateMgr := template.NewManager()
	if len(cfg.Templates) > 0 {
		if err := templateMgr.LoadFromMap(cfg.Templates); err != nil {
			logger.Error("failed to load templates", "error", err)
			return deps.fail()
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
			return deps.fail()
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
		return deps.fail()
	}
	// Rule state (for windows) defaults to in-process; redis shares it
	// across restarts and instances.
	if cfg.RulesState != nil && cfg.RulesState.Type != "" && cfg.RulesState.Type != "memory" {
		if cfg.RulesState.Type != "redis" {
			logger.Error("unknown rules_state type", "type", cfg.RulesState.Type)
			return deps.fail()
		}
		ss, err := rules.NewRedisStateStore(cfg.RulesState.Addr, cfg.RulesState.Password, cfg.RulesState.DB)
		if err != nil {
			logger.Error("failed to open rules state store", "error", err)
			return deps.fail()
		}
		rulesEngine.SetStateStore(ss)
	}
	for i := range cfg.Rules {
		rule := cfg.Rules[i]
		if err := rulesEngine.Put(context.Background(), &rule); err != nil {
			logger.Error("failed to load rule", "index", i, "error", err)
			return deps.fail()
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
			return deps.fail()
		}
		groupStore = store
	}
	groupsManager := groups.NewManager(groupStore)
	if groupStore != nil {
		if err := groupsManager.Reload(context.Background()); err != nil {
			logger.Error("failed to load groups", "error", err)
			return deps.fail()
		}
	}
	for i := range cfg.Groups {
		group := cfg.Groups[i]
		if err := groupsManager.Put(context.Background(), &group); err != nil {
			logger.Error("failed to load group", "index", i, "error", err)
			return deps.fail()
		}
	}
	if len(cfg.Groups) > 0 {
		logger.Info("groups loaded", "count", len(cfg.Groups))
	}

	// Create the user-level audience tables: "user:" channel references
	// resolve through audiences/recipients to provider endpoints.
	// Audiences carry a runtime API backed by audiences_store (config
	// seeds win same-id collisions, mirroring groups); recipients stay
	// config-only. An invalid table is configuration drift and refuses to
	// start.
	var audienceStore audience.Store
	if cfg.AudiencesStore != "" {
		store, err := audience.NewFileStore(cfg.AudiencesStore)
		if err != nil {
			logger.Error("failed to open audiences store", "path", cfg.AudiencesStore, "error", err)
			return deps.fail()
		}
		audienceStore = store
	}
	audienceManager, err := audience.NewManager(audienceStore, cfg.Audiences, cfg.Recipients)
	if err != nil {
		logger.Error("failed to load audiences/recipients", "error", err)
		return deps.fail()
	}
	if audienceStore != nil {
		if err := audienceManager.Reload(context.Background()); err != nil {
			logger.Error("failed to load audiences", "error", err)
			return deps.fail()
		}
	}
	for id, aud := range cfg.Audiences {
		if err := audienceManager.Put(context.Background(), id, aud); err != nil {
			logger.Error("failed to load audience", "id", id, "error", err)
			return deps.fail()
		}
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
			return deps.fail()
		}
		rosterStore = store
	}
	rostersManager := roster.NewManager(rosterStore)
	if rosterStore != nil {
		if err := rostersManager.Reload(context.Background()); err != nil {
			logger.Error("failed to load rosters", "error", err)
			return deps.fail()
		}
	}

	// Create worker registry and pool
	registry := worker.NewRegistry()
	pool := worker.NewPool(q, manager, registry, cfg.Queue.Workers)
	deps.pool = pool

	// Ack store and escalation manager share the alert identity space:
	// the ack arriving in time cancels the pending upgrade. The incident
	// ledger records the episodes those alerts live through.
	ackStore := ack.NewMemoryStore()
	escalations := escalation.NewManager(ackStore, nil, cfg.EscalationStore)
	deps.escalations = escalations
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
			return deps.fail()
		}
		dailySpec, err := digest.ParseDaily(orDefault(cfg.Digest.Daily, "09:00"), loc)
		if err != nil {
			logger.Error("digest daily schedule invalid", "error", err)
			return deps.fail()
		}
		weeklySpec, err := digest.ParseWeekly(orDefault(cfg.Digest.Weekly, "Mon 09:00"), loc)
		if err != nil {
			logger.Error("digest weekly schedule invalid", "error", err)
			return deps.fail()
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
				return deps.fail()
			}
			leaseTTL := cfg.Digest.LeaseTTL
			if leaseTTL <= 0 {
				leaseTTL = time.Minute
			}
			digestLock = digest.NewLeaderLock(client, "herald:digest:leader", leaseTTL)
		}
	}
	deps.digestAgg = digestAgg
	deps.digestLock = digestLock

	// The audience registries (关系详设 §2/§3) are shared by every
	// 触发面/拉取面 feature: feeds resolve private tokens and check
	// relations at read time, source entries write follows/unfollows,
	// the reconciler sweeps. One pair, always constructed; the audit
	// trail is always attached, so every registry change answers
	// 谁在何时通过哪条入口改了什么.
	surfaces := audience.NewSurfaceRegistry()
	relations := audience.NewRegistry()
	// §12 审计流水: always on — the §13.4 app query face reads the
	// namespace trail from it, so recording cannot depend on which
	// entry adapters happen to be enabled.
	auditStore := audit.New(0)
	surfaces.SetRecorder(auditStore)
	relations.SetRecorder(auditStore)

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

	// §6 strategy件: category urgency/mode and channel intensity
	// overrides on taxonomy defaults. An unparseable override refuses
	// to start rather than silently re-grading a channel. The policy
	// rides into api.Config for the trigger face; the direct /notify
	// path carries no category dimension yet, so matching is not
	// enforced on that path until the expandRef 触发面 (批次 11).
	deliveryPolicy, err := audience.NewDeliveryPolicy(cfg.Delivery.CategoryUrgency, cfg.Delivery.ChannelIntensity, cfg.Delivery.CategoryMode, cfg.Delivery.AllowPhone)
	if err != nil {
		logger.Error("invalid delivery policy", "error", err)
		return deps.fail()
	}

	// §13.1 集成者命名空间: config-seeded apps with scoped tokens. The
	// registry constructor is the validation point; a bad seed refuses
	// to start. The app face rides into api.Config for its endpoints.
	appRegistry, err := apps.NewRegistry(seedApps(cfg.Apps))
	if err != nil {
		logger.Error("invalid apps config", "error", err)
		return deps.fail()
	}

	// §13.5 webhook 回调: delivery settles and unsubscribe backflow flow
	// to the app's registered URL through the same queue→worker pipeline;
	// apps without callback config simply never see tasks.
	callbackProvider.SetAppRegistry(appRegistry)
	emitter := callback.NewEmitter(appRegistry, q)
	manager.SetCallbackSink(emitter)
	relations.SetUnsubscribeHook(emitter.Unsubscribed)

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
				return deps.fail()
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
				return deps.fail()
			}
			leaseTTL := cfg.Sources.Reconcile.LeaseTTL
			if leaseTTL <= 0 {
				leaseTTL = time.Minute
			}
			reconcileLock = digest.NewLeaderLock(client, "herald:sources:reconcile:leader", leaseTTL)
		}
	}
	deps.reconciler = reconciler
	deps.reconcileLock = reconcileLock

	// Process metrics sampler (dashboard trend source): samples procfs
	// CPU/RSS, goroutines and the online worker count into a bounded
	// ring. Zero config values fall back to the built-in defaults; a
	// negative value is a configuration error and refuses to start.
	metricsSampler, err := newMetricsSampler(cfg, registry)
	deps.metricsSampler = metricsSampler
	if err != nil {
		logger.Error("failed to configure metrics sampler", "error", err)
		return deps.fail()
	}

	// Create API server
	srv := api.NewServer(&api.Config{
		Addr:                cfg.Server.Addr,
		Timeout:             cfg.Server.Timeout,
		MaxRequestBodyBytes: cfg.Server.MaxRequestBodyBytes,
		RequestBodyLimits:   cfg.Server.RequestBodyLimits,
		Router:              router,
		Queue:               q,
		Runtime:             manager,
		Dedup:               d,
		Auth:                a,
		Keys:                credStore,
		TemplateManager:     templateMgr,
		WorkerRegistry:      registry,
		Metrics:             metricsSampler,
		Rules:               rulesEngine,
		Groups:              groupsManager,
		Users:               audienceManager,
		Rosters:             rostersManager,
		AckStore:            ackStore,
		Escalation:          escalations,
		Incidents:           incidents,
		Digest:              digestAgg,
		DigestPrefs:         audience.NewPreferenceRegistry(),
		Feeds:               feedStore,
		FeedMeta:            feedMeta,
		FeedSurfaces:        surfaces,
		FeedRelations:       relations,
		Sources:             sourceAdapter,
		Delivery:            deliveryPolicy,
		Filter:              audience.NewFilter(relations, surfaces),
		Audit:               auditStore,
		Apps:                appRegistry,
		SourceSurfaces:      surfaces,
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
		MaxMessageSize: cfg.WebSocket.MaxMessageSize,
		AuthToken:      cfg.WebSocket.AuthToken,
	}, nil)
	hub := websocket.NewHub(wsServer, registry)
	wsServer.SetHandler(hub)
	srv.SetWebSocketServer(wsServer)

	deps.srv = srv
	deps.wsServer = wsServer
	return deps, nil
}

// start launches the API server, websocket management channel, worker pool
// and the optional digest/reconcile background loops. ctx is owned by
// serveCmd; cancel aborts the API server on a fatal start error.
func (d *serveDeps) start(ctx context.Context, cancel context.CancelFunc) {
	go func() {
		if err := d.srv.Start(ctx); err != nil {
			logger.Error("server error", "error", err)
			cancel()
		}
	}()

	go func() {
		// Start launches ListenAndServe in its own goroutine and always
		// returns nil; bind failures are logged inside Start.
		_ = d.wsServer.Start()
	}()

	go d.pool.Run(ctx)

	// Digest flip loop (关系详设 §10): the aggregator was wired into the
	// API server above; this loop flips due windows. The redis lease
	// (锁选主) keeps multi-instance fleets flipping each window exactly
	// once; without redis_addr the loop runs unlocked, assuming a single
	// instance.
	if d.digestAgg != nil {
		interval := d.cfg.Digest.Interval
		if interval <= 0 {
			interval = time.Minute
		}
		go digest.FlipLoop(ctx, d.digestAgg, d.digestLock, interval, flushDigest(d.srv))
		logger.Info("digest flip loop started", "daily", orDefault(d.cfg.Digest.Daily, "09:00"),
			"weekly", orDefault(d.cfg.Digest.Weekly, "Mon 09:00"), "interval", interval,
			"leader_lease", d.digestLock != nil)
	}

	// Source reconcile loop (rule 3 对账): sweeps active surfaces against
	// the platforms on a schedule. The redis lease (锁选主) keeps a
	// multi-instance fleet sweeping exactly once per round; probe errors
	// skip — 对账 never guesses.
	if d.reconciler != nil {
		interval := d.cfg.Sources.Reconcile.Interval
		if interval <= 0 {
			interval = time.Hour
		}
		go reconcileLoop(ctx, d.reconciler, d.reconcileLock, interval)
		logger.Info("source reconcile loop started", "channels", d.reconciler.Channels(),
			"interval", interval, "leader_lease", d.reconcileLock != nil)
	}
}

// shutdown stops every background component in reverse start order and
// releases the leader leases so a replacement instance can take over
// immediately instead of idling until they lapse.
func (d *serveDeps) shutdown() {
	logger.Info("shutting down...")
	d.metricsSampler.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), d.cfg.Server.Timeout)
	defer shutdownCancel()

	_ = d.srv.Shutdown(shutdownCtx)
	_ = d.manager.Close(shutdownCtx)
	_ = d.wsServer.Stop()
	// Stops upgrade timers; pending records stay persisted for the next
	// run to restore and judge.
	_ = d.escalations.Close()
	// The flip loop exits with ctx; an immediate lease handover keeps
	// another instance from idling until the lease lapses.
	if d.digestLock != nil {
		d.digestLock.Release(shutdownCtx)
	}
	// Same for the reconcile sweep's lease.
	if d.reconcileLock != nil {
		d.reconcileLock.Release(shutdownCtx)
	}

	logger.Info("shutdown complete")
}
