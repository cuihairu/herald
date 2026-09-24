// Package herald exposes the library form of Herald: an embeddable,
// event-driven notification dispatcher.
//
// The zero-configuration path runs on an in-memory queue with a local worker
// pool:
//
//	app, err := herald.New(nil)
//	if err != nil { ... }
//	defer app.Close()
//
//	res, err := app.Dispatch(ctx, &core.Notification{
//	    Type:     "deploy",
//	    Channels: []string{"feishu-ops"},
//	})
//
// The CLI (cmd/heraldd) composes the same components for server mode; use the
// facade when Herald is embedded in another process instead.
package herald

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/ack"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/rules"
	coreruntime "github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/service"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/worker"
	builtinregistry "github.com/cuihairu/herald/providers/builtin/registry"
)

// App is an embedded Herald instance: queue, delivery pool and the
// notification pipeline bound together. Create one with New and stop it with
// Close; an App is safe for concurrent Dispatch calls.
type App struct {
	cfg        *config.Config
	queue      *awaitingQueue
	backend    core.Queue
	manager    *coreruntime.Manager
	svc        *service.NotificationService
	rules      *rules.Engine
	acks       *ack.MemoryStore
	escalation *escalation.Manager
	cancel     context.CancelFunc
	done       chan struct{}
}

// New builds an App from cfg and starts its delivery pool in the background.
// A nil cfg (or empty fields) selects the defaults from applyDefaults:
// an in-memory queue, dedup with a 5-minute window and exponential retry.
// Providers listed in cfg.Providers are created and registered before New
// returns; a failure there aborts construction.
func New(cfg *config.Config) (*App, error) {
	if cfg == nil {
		cfg = config.Default()
	}
	cfg = applyDefaults(cfg)

	backend, err := queue.NewQueue(cfg.Queue.ToQueueConfig())
	if err != nil {
		return nil, fmt.Errorf("create queue: %w", err)
	}
	aq := newAwaitingQueue(backend)

	retryCfg := &retry.Config{
		Max:          cfg.Retry.Max,
		Backoff:      cfg.Retry.Backoff,
		InitialDelay: cfg.Retry.InitialDelay,
		MaxDelay:     cfg.Retry.MaxDelay,
	}
	manager := coreruntime.NewManager(10000, retryCfg)
	builtinregistry.RegisterBuiltinProviders(manager)

	for name, providerCfg := range cfg.Providers {
		provider, err := manager.CreateProvider(providerCfg.Type, providerCfg.Config)
		if err != nil {
			_ = backend.Close()
			return nil, fmt.Errorf("create provider %s: %w", name, err)
		}
		enabled := providerCfg.Enabled == nil || *providerCfg.Enabled
		if err := manager.RegisterProvider(name, provider, enabled); err != nil {
			_ = backend.Close()
			return nil, fmt.Errorf("register provider %s: %w", name, err)
		}
		if providerCfg.RateLimit != nil {
			if err := manager.SetProviderLimiter(name, providerCfg.RateLimit); err != nil {
				_ = backend.Close()
				return nil, fmt.Errorf("rate limit for %s: %w", name, err)
			}
		}
	}

	router := route.NewRouter(&route.Config{Routes: cfg.Routes})

	var d *dedup.Dedup
	if cfg.Dedup.Enabled {
		d = dedup.NewDedup(&dedup.Config{Window: cfg.Dedup.Window})
	}

	templateMgr := template.NewManager()
	if len(cfg.Templates) > 0 {
		if err := templateMgr.LoadFromMap(cfg.Templates); err != nil {
			_ = backend.Close()
			return nil, fmt.Errorf("load templates: %w", err)
		}
	}

	svc := service.NewNotificationService(templateMgr, router, manager, d, aq)

	// Rule engine: seeded from cfg.Rules (empty by default), evaluated
	// after dedup and before routing. Shadow hits are observed by the
	// manager into the delivery log, sampled per rule.
	var ruleStore rules.Store = rules.NewMemoryStore()
	if cfg.RulesStore != "" {
		fs, err := rules.NewFileStore(cfg.RulesStore)
		if err != nil {
			_ = backend.Close()
			return nil, fmt.Errorf("open rules store: %w", err)
		}
		ruleStore = fs
	}
	rulesEngine := rules.NewEngine(ruleStore)
	// Rule state (for windows) defaults to in-process; redis shares it
	// across restarts and instances.
	if cfg.RulesState != nil && cfg.RulesState.Type != "" && cfg.RulesState.Type != "memory" {
		if cfg.RulesState.Type != "redis" {
			_ = backend.Close()
			return nil, fmt.Errorf("unknown rules_state type %q (want memory or redis)", cfg.RulesState.Type)
		}
		ss, err := rules.NewRedisStateStore(cfg.RulesState.Addr, cfg.RulesState.Password, cfg.RulesState.DB)
		if err != nil {
			_ = backend.Close()
			return nil, fmt.Errorf("open rules state store: %w", err)
		}
		rulesEngine.SetStateStore(ss)
	}
	for i := range cfg.Rules {
		rule := cfg.Rules[i]
		if err := rulesEngine.Validate(&rule); err != nil {
			_ = backend.Close()
			return nil, fmt.Errorf("rule %d: %w", i, err)
		}
		if err := rulesEngine.Put(context.Background(), &rule); err != nil {
			_ = backend.Close()
			return nil, fmt.Errorf("put rule %d: %w", i, err)
		}
	}
	svc.SetRuleEngine(rulesEngine)
	svc.SetRuleObserver(manager)

	// Ack-gated escalation: the ack store and the pending-upgrade manager
	// share the alert identity space; the service both arms upgrades on
	// routed deliveries and delivers them when one fires.
	acks := ack.NewMemoryStore()
	escalations := escalation.NewManager(acks, svc, cfg.EscalationStore)
	svc.SetEscalationScheduler(escalations)

	pool := worker.NewPool(aq, manager, worker.NewRegistry(), cfg.Queue.Workers)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.Run(ctx)
	}()

	// Restore pending upgrades from a previous run (no-op without
	// escalation_store); a corrupt file is a construction failure — the
	// library caller decides what to do with the data on disk.
	if err := escalations.Restore(context.Background()); err != nil {
		_ = backend.Close()
		cancel()
		return nil, fmt.Errorf("restore escalations: %w", err)
	}

	return &App{
		cfg:        cfg,
		queue:      aq,
		backend:    backend,
		manager:    manager,
		svc:        svc,
		rules:      rulesEngine,
		acks:       acks,
		escalation: escalations,
		cancel:     cancel,
		done:       done,
	}, nil
}

// Dispatch enqueues the notification and returns once it has been accepted
// onto the queue; actual delivery happens in the background pool. Channel
// resolution, template rendering and dedup run synchronously, so
// routing/rendering errors surface here. Per-channel failures are reported in
// the returned ProcessResult.
func (a *App) Dispatch(ctx context.Context, n *core.Notification) (*service.ProcessResult, error) {
	return a.svc.Process(ctx, n)
}

// DispatchSync is Dispatch plus delivery: it blocks until every task created
// for this notification has been acknowledged by the pool, or until ctx is
// done. If any delivery fails, the first error is returned alongside the
// ProcessResult.
func (a *App) DispatchSync(ctx context.Context, n *core.Notification) (*service.ProcessResult, error) {
	res, err := a.svc.Process(ctx, n)
	if err != nil {
		return res, err
	}
	for _, id := range res.TaskIDs {
		if waitErr := a.queue.wait(ctx, id); waitErr != nil {
			return res, waitErr
		}
	}
	return res, nil
}

// Runtime exposes the provider manager so embedders can register custom
// providers beyond those created from cfg.Providers.
func (a *App) Runtime() *coreruntime.Manager {
	return a.manager
}

// Rules exposes the rule engine so embedders can add or update rules at
// runtime (Put/Delete take effect on the next Dispatch; they compile at
// save time and reject invalid expressions).
func (a *App) Rules() *rules.Engine {
	return a.rules
}

// Queue returns the queue the App dispatches through.
func (a *App) Queue() core.Queue {
	return a.backend
}

// Close stops the delivery pool, waits for it to drain, then closes the queue
// and the registered providers. It is safe to call once; Dispatch after Close
// fails with a queue-closed error.
func (a *App) Close() error {
	a.cancel()
	<-a.done
	// The pool has fully exited, so closing the backend cannot race an
	// in-flight Pop (the memory queue returns zero-value tasks once closed).
	_ = a.escalation.Close()
	err := a.backend.Close()
	_ = a.manager.Close(context.Background())
	return err
}

// Acks returns the app's alert acknowledgement store — the same store the
// escalation timers consult before firing.
func (a *App) Acks() *ack.MemoryStore {
	return a.acks
}

// Escalation returns the app's pending-upgrade manager.
func (a *App) Escalation() *escalation.Manager {
	return a.escalation
}

// applyDefaults returns a copy of cfg with the library-relevant zero values
// filled in. Map fields are shared with the caller, but only scalar fields
// are written here.
func applyDefaults(cfg *config.Config) *config.Config {
	c := *cfg
	if c.Queue.Type == "" {
		c.Queue.Type = "memory"
	}
	if c.Queue.Size == 0 {
		c.Queue.Size = 10000
	}
	if c.Queue.Workers == 0 {
		c.Queue.Workers = runtime.NumCPU()*2 + 1
	}
	if c.Dedup.Window == 0 {
		c.Dedup.Window = 5 * time.Minute
	}
	if c.Retry.Max == 0 {
		c.Retry.Max = 3
	}
	if c.Retry.Backoff == "" {
		c.Retry.Backoff = "exponential"
	}
	if c.Retry.InitialDelay == 0 {
		c.Retry.InitialDelay = time.Second
	}
	if c.Retry.MaxDelay == 0 {
		c.Retry.MaxDelay = time.Minute
	}
	return &c
}

// awaitingQueue decorates a core.Queue with per-task completion signals so
// DispatchSync can block on delivery. The pool calls Ack/Nack when a task is
// delivered or fails; the outcome is handed to a registered listener, or
// cached until DispatchSync asks for it (the task may finish before it waits).
type awaitingQueue struct {
	core.Queue

	mu           sync.Mutex
	listeners    map[string][]chan error
	resolved     map[string]error
	resolveOrder []string // FIFO eviction order for resolved
}

// maxResolvedResults bounds the outcome cache. Outcomes for tasks nobody ever
// waits on (plain async Dispatch) are evicted oldest-first; a DispatchSync
// would need this many other completions to slip between Push and wait for
// its own outcome to be evicted, which is not a realistic window.
const maxResolvedResults = 4096

// resolvedResultsLimit is the mutable seam tests use to exercise eviction.
var resolvedResultsLimit = maxResolvedResults

func newAwaitingQueue(inner core.Queue) *awaitingQueue {
	return &awaitingQueue{
		Queue:     inner,
		listeners: make(map[string][]chan error),
		resolved:  make(map[string]error),
	}
}

func (q *awaitingQueue) Ack(ctx context.Context, taskID string) error {
	err := q.Queue.Ack(ctx, taskID)
	q.resolve(taskID, nil)
	return err
}

func (q *awaitingQueue) Nack(ctx context.Context, taskID string, reason error) error {
	err := q.Queue.Nack(ctx, taskID, reason)
	q.resolve(taskID, reason)
	return err
}

func (q *awaitingQueue) resolve(taskID string, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if listeners := q.listeners[taskID]; len(listeners) > 0 {
		delete(q.listeners, taskID)
		for _, ch := range listeners {
			ch <- err
		}
		return
	}

	// Nobody is waiting yet: cache the outcome.
	if _, exists := q.resolved[taskID]; !exists {
		q.resolveOrder = append(q.resolveOrder, taskID)
	}
	q.resolved[taskID] = err
	for len(q.resolveOrder) > resolvedResultsLimit {
		oldest := q.resolveOrder[0]
		q.resolveOrder = q.resolveOrder[1:]
		delete(q.resolved, oldest)
	}
}

// wait blocks until the task is acknowledged (nil) or nacked (its reason).
func (q *awaitingQueue) wait(ctx context.Context, taskID string) error {
	q.mu.Lock()
	if err, ok := q.resolved[taskID]; ok {
		delete(q.resolved, taskID)
		q.mu.Unlock()
		return err
	}
	ch := make(chan error, 1)
	q.listeners[taskID] = append(q.listeners[taskID], ch)
	q.mu.Unlock()

	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
