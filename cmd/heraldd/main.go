package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/digest"
	"github.com/cuihairu/herald/core/metrics"
	"github.com/cuihairu/herald/core/queue"
	coreruntime "github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/internal/logger"
	"github.com/cuihairu/herald/protocol"
	builtinregistry "github.com/cuihairu/herald/providers/builtin/registry"
	gws "github.com/gorilla/websocket"
	"net/url"
	"os"
	"os/signal"
	stdruntime "runtime"
	"sort"
	"strings"
	"syscall"
	"time"
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
	deps, err := assembleServe(cfg)
	if err != nil {
		return 1
	}
	defer deps.closeAll()

	// Start everything
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.start(ctx, cancel)

	logger.Info("herald scheduler started", "addr", cfg.Server.Addr, "workers", cfg.Queue.Workers)

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	deps.shutdown()
	return 0
}

// newMetricsSampler builds and starts the process metrics sampler with
// the config knobs. Zero values fall back to the built-in defaults (10s
// interval, 24h retention); a negative value is a configuration error and
// refuses to start.
func newMetricsSampler(cfg *config.Config, registry *worker.Registry) (*metrics.Sampler, error) {
	interval := cfg.Metrics.SampleInterval
	if interval == 0 {
		interval = 10 * time.Second
	}
	retention := cfg.Metrics.Retention
	if retention == 0 {
		retention = 24 * time.Hour
	}
	if err := metrics.Validate(interval, retention); err != nil {
		return nil, err
	}
	// 注册表只持有在册（在线）worker：注册于连接建立、注销于断开，
	// 与 HandleWorkers 的口径一致。
	onlineWorkers := func() int { return len(registry.List()) }
	sampler := metrics.NewSampler(interval, retention, metrics.NewHostCollector(), onlineWorkers, nil)
	sampler.Start()
	return sampler, nil
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
		if err := registerRemoteWorker(conn, workerID, capabilities, cfg.Worker.AuthToken); err != nil {
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

func registerRemoteWorker(conn *gws.Conn, workerID string, capabilities []string, authToken string) error {
	msg := &protocol.RegisterMessage{
		WorkerID:     workerID,
		Mode:         string(worker.Remote),
		Platform:     stdruntime.GOOS,
		Version:      "dev",
		Capabilities: capabilities,
		AuthToken:    authToken,
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
// seedApps converts the config block into registry seeds in a stable
// order (map iteration would do, but stable input keeps the refusal
// messages reproducible).
func seedApps(appSeeds map[string]config.AppConfig) []apps.SeedApp {
	names := make([]string, 0, len(appSeeds))
	for name := range appSeeds {
		names = append(names, name)
	}
	sort.Strings(names)
	seeds := make([]apps.SeedApp, 0, len(names))
	for _, name := range names {
		cfgApp := appSeeds[name]
		tokens := make([]apps.SeedToken, 0, len(cfgApp.Tokens))
		for _, t := range cfgApp.Tokens {
			tokens = append(tokens, apps.SeedToken{Secret: t.Secret, Scopes: t.Scopes})
		}
		seeds = append(seeds, apps.SeedApp{Name: name, Tokens: tokens})
	}
	return seeds
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
