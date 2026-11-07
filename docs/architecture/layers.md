# Source Layer Analysis

> Source-first, abstraction-first reading of the Herald codebase (read before
> writing; no directory-name guessing). Scope: non-test Go, the TypeScript
> dashboard, protocol and config schema files; excludes `deps`, `node_modules`,
> `dist`, `build`, `.git`, `.cache`, `Testing`, `vendor`, coverage artifacts and
> generated binaries. Layers are *responsibilities*, not directory names.

## 1. Glossary

| Term | English | Definition | File:line |
|------|---------|------------|-----------|
| Event | Inbound notification intent | A caller's "what happened" description: type/level/channels/params, plus optional audience, event identity and state machine dimension. | core/types.go:26 |
| Notification | Normalized notification | The pipeline's canonical in-flight record that rules, dedup and routing all read. | core/types.go:26 |
| Delivery Task | One channel delivery unit | A single provider-bound attempt: task id, provider, rendered payload, recipients, retry budget. | core/types.go:64 |
| Queue | Task transport | Reliable consumption queue (in-memory channel or Redis Streams) that holds tasks between planning and worker consumption. | core/types.go:169 |
| Scheduler | Delayed re-enqueue capability | Optional queue capability: holds a task aside until a future time (async retry waits live in the queue, not in the worker). | core/types.go:184 |
| Worker | Delivery consumer | A pool goroutine (local) or a separate process (remote) that pops tasks and drives provider delivery. | core/worker/pool.go:17 |
| Provider | Channel adapter | The one interface every delivery channel implements: `Deliver` plus name/type/status. | core/provider.go:8 |
| Provider Factory | Provider constructor | Maps a provider type string + config map to a concrete Provider instance. | core/provider.go:43 |
| Runtime Manager | Provider registry | Owns registered provider instances, factories, per-provider rate limiters and the delivery/retry settle state. | core/runtime/manager.go:27 |
| Router | Static routing table | Resolves a notification type/level to a channel list, then to provider lists through the channels block. | core/route/router.go:14 |
| Rule Engine | Declarative policy table | expr-compiled match/route/allow/suppress rules plus for/group/inhibit/silence semantics evaluated before routing. | core/rules/engine.go:291 |
| Rule Store | Rule persistence | Ordered list of rules (memory or JSON file); order is evaluation priority. | core/rules/store.go:13 |
| Rule State Store | Stateful rule state | Durable for-window / group / inhibit presence state (memory or Redis). | core/rules/state.go:20 |
| Dedup Gate | §11 four-stage gate | Event idempotency → state machine → frequency tier → delivery, plus the fold ledger. | core/dedup/gate.go:46 |
| Template Manager | Template registry | Holds channel-agnostic templates and renders them to a semantic RenderedData. | core/template/manager.go:14 |
| Template Engine | Text rendering | `text/template` substitution over title/fields, cached by content hash. | core/template/engine.go:12 |
| Delivery Planner | Channel→task planning | Expands refs (plain/group/user/channel) and renders per-provider payloads into tasks. | core/service/planner.go |
| Notification Service | Pipeline orchestrator | Rules → routing → template render → dedup → digest/feed projection → enqueue; the one Process() path. | core/service/notification.go:233 |
| Delivery Store | Lifecycle state machine | Accepted/queued/delivering/retrying/delivered/failed/dead per task, with retry progression. | core/deliverystore/deliverystore.go:13 |
| Log Store | Factual event log | One row per task's send/result/duration/error; status is the event outcome, not the lifecycle. | core/logstore/logstore.go:17 |
| Retryer | Retry policy | Exponential/fixed backoff over classified errors; async Defer re-enqueues with a stamped NextRetryAt. | core/retry/retryer.go:112 |
| Limiter | Per-provider rate limit | Token-bucket limiter attached to a provider instance, waited on before every Deliver. | core/limiter/limiter.go:15 |
| Error Class | Provider error vocabulary | Temporary/permanent/rate_limited/authentication/invalid_request/timeout; retry decides from it alone. | core/errclass/errclass.go:14 |
| Ack Store | Acknowledgement records | Alert-id keyed acknowledgements consulted by escalation before it fires. | core/ack/ack.go:17 |
| Escalation Manager | Ack-gated upgrade | Arms a timeout on rule delivery; ack cancels, timeout fires on the plan's "to" channels; pending records persist to JSON. | core/escalation/escalation.go:46 |
| Incident Ledger | Alert episode record | Open/acked/resolved lifecycle per alert episode, keyed by both engine group key and caller alert id. | core/incident/incident.go:49 |
| Digest Aggregator | Window fold | Collects low-frequency events per audience×category×mode into daily/weekly windows; flips emit one summary. | core/digest/digest.go:128 |
| Leader Lock | Multi-instance lease | Redis SET NX PX lease that makes digest flip and reconcile sweeps run once per fleet round. | core/digest/lock.go:14 |
| Feeds Store | RSS pull projection | Stores rss-classified deliveries as feed items; public and token-private feeds render them. | core/feeds/store.go |
| Surface Registry | Contact-surface table | Audience×channel handles with pending/active/invalid lifecycle and one-time binding tokens. | core/audience/surfaces.go:33 |
| Relation Registry | Subscription table | Audience×category×channel entitlements (subscription vs enrollment) with policy rights and audit. | core/audience/relations.go:40 |
| Audience Manager | user: resolution | Named audiences/recipients; "user:" refs expand to provider endpoints. | core/audience/audience.go:44 |
| Group Manager | group: resolution | Named delivery audiences; "group:" refs expand to members (single-level). | core/groups/groups.go:13 |
| Roster Manager | Duty roster | Externally pushed absolute time periods that a rule's silence can name as its schedule. | core/roster/roster.go |
| Source Adapter | Platform entry points | Bot/公众号 callbacks and in-app actions that converge follow/unfollow onto the registries. | core/audience/sources.go |
| Reconciler | External-state sweep | Periodically probes active surfaces; dead handles go invalid and their subscriptions terminate. | core/audience/reconcile.go:30 |
| Delivery Policy | Intensity×urgency×mode | L0–L5 channel intensity, category urgency and fixed/escalation/parallel mode planning. | core/audience/intensity.go |
| Preference Registry | §7 frequency table | Per-audience×category×channel realtime/daily/weekly/none choice with the silence floor. | core/audience/preferences.go |
| Delivery Filter | §14 intersection | Target ∩ relation permission ∩ active contact surface, checked per audience/channel/category. | core/audience/filter.go:13 |
| Audit Trail | §12 event stream | Relation/surface changes and dedup folds in one store, filtered apart at read time. | core/audit/audit.go:16 |
| Apps Registry | §13.1 integration namespaces | App-token auth with config/trigger/query scopes; namespace-scoped categories, policies, templates. | core/apps/apps.go:13 |
| Callback Provider | §13.5 webhook callback | Posts signed delivery-result/unsubscribe events through the same queue→worker pipeline. | core/callback/provider.go:15 |
| WebSocket Server | Management channel | Remote worker register/heartbeat/ack/event over WebSocket, with origin and token checks. | core/websocket/server.go:55 |
| Worker SDK | Remote worker client | Client that registers via WebSocket and consumes queue tasks; embedded mode skips the control plane. | worker-sdk/go/client.go:21 |
| Apps SDK | Integrator client | Typed Go client for the §13 faces (categories/policies/templates/dispatch/events/query). | apps-sdk/go/faces.go:10 |
| Auth | Operator authentication | API key + JWT session + bcrypt admin login, with per-client login rate limiting. | core/auth/auth.go:27 |
| Credential Store | SQLite credential persistence | Dashboard-minted API keys (hash + masked prefix) and rotated password hashes. | internal/store/store.go:10 |
| Metrics Sampler | Process sampling | CPU/RSS/goroutines/online workers into a bounded ring for dashboard trend charts. | core/metrics/metrics.go:27 |
| HTTP Client | Shared transport | Timeout/connection-pool HTTP client plus pinned-IP dialing and retry classification helpers. | core/httpclient/client.go:30 |
| Config Schema | Startup configuration | YAML config struct, env expansion, defaults and validation; provider schema map. | config/config.go:56 |
| Protocol | Wire messages | Worker↔core register/heartbeat/dispatch/ack/event messages with typed envelope. | protocol/message.go:19 |
| CLI Entry | Process composition | `heraldd serve`/`worker` builds queue, router, runtime, stores, API and WebSocket, then runs to signal. | cmd/heralld/main.go:100 |
| Library Facade | Embedded App | Zero-config embeddable dispatcher composing the same components for in-process use. | herald.go:34 |
| Dashboard | Operator UI | React SPA (Vite + TS) talking to the REST API; state in a single store module. | dashboard/src/stores/herald.ts |
| Docs Site | VitePress documentation | English docs at docs/, Chinese mirror at docs/zh/; sidebar per section. | docs/.vitepress/config.mts:1 |

## 2. Layer Map

### 2.1 Composition / Entry Layer

- **Implementation:** `cmd/heraldd/main.go` (serve + worker commands), `herald.go` (embedded `App` facade).
- **Config schema:** `config/config.go` (`Config` at :56; sub-schemas `ServerConfig` :164, `WebSocketConfig` :172, `AuthConfig` :201, `ProviderConfig` :229, `QueueConfig` :245, `DedupConfig` :287, `DigestConfig` :303, `FeedsConfig` :330, `SourcesConfig` :350, `DeliveryConfig` :28, `RulesStateConfig` :154).
- **Protocol files:** `protocol/message.go` (worker↔core wire types), shared by `worker-sdk` and `core/websocket`.
- **Copy:** docs site `docs/` (English) + `docs/zh/` (Chinese mirror); `docs/guide/configuration.md` documents the YAML schema.
- **Lifecycle:** `serveCmd` (cmd/heraldd/main.go:100) loads+validates config, builds queue/router/runtime/stores/rules/groups/audiences/rosters/registry/pool, wires the API and WebSocket servers, starts all goroutines, blocks on SIGINT/SIGTERM, then shuts down in reverse order (:665-720). `workerCmd` (:731) starts a remote worker: shared queue only, then `runRemoteWorkerControlPlane` (:829).

### 2.2 HTTP / API Layer

- **Implementation:** `api/server.go` (`NewServer` :141, route table :268-363), `api/handler.go` and `api/handler_*.go` per resource (notify, logs, deliveries, providers, templates, rules, groups, audiences, rosters, incidents, feeds, apps, sources, callbacks, workers, keys, overview, metrics).
- **Config schema:** `api.Config` (api/server.go:47) — all wiring dependencies are injected, not loaded.
- **Protocol files:** `docs/api/openapi.yaml` (OpenAPI 3 spec); `docs/api/rest.md` + `docs/api/apps.md` document endpoints.
- **Copy:** `docs/api/` (English); the Chinese mirror lives at `docs/zh/api/`.
- **Lifecycle:** HTTP server bound in `NewServer`; `Start` (api/server.go:407) runs `ListenAndServe`; `Shutdown` (:423) drains it. Auth middleware (`withAuth`/`withApp`) guards the protected endpoints.

### 2.3 Delivery Pipeline Layer

- **Implementation:** `core/service/notification.go` (`Process` :233), `core/service/planner.go` (channel→task planning), `core/service/dispatch.go` (app dispatch face).
- **Config schema:** none — the service is wired from `api.Config` (rules, dedup, digest, feeds, escalation, incidents, groups/users/channel resolvers, surfaces).
- **Protocol files:** none; it consumes `core.Notification` and emits `core.DeliveryTask`.
- **Copy:** `docs/architecture/modules.md` + `docs/guide/events.md`.
- **Lifecycle:** stateless orchestrator; per-call `Process` runs rules → routing → render → dedup gate → digest/feed projection → enqueue. Settled tasks flow back through the runtime callback sink (`core/callback/emitter.go`).

### 2.4 Routing / Rules Layer

- **Implementation:** `core/route/router.go` (static type/level/channel resolution), `core/rules/engine.go` (evaluation), `core/rules/rule.go` (Rule model), `core/rules/for.go`, `group.go`, `inhibit.go`, `silence.go`, `shadow.go`, `escalation.go` (semantics).
- **Config schema:** `routes`, `level_routes`, `channels` in `config.Config`; `rules: []rules.Rule`; `rules_store`, `rules_default_policy`, `rules_state`.
- **Protocol files:** none.
- **Copy:** `docs/design-rule-engine.md`, `docs/architecture/core-design.md`.
- **Lifecycle:** engine holds compiled rules in list order; store (memory/file) reloads on `Reload`; state store (memory/redis) persists for-window/group/inhibit across restarts.

### 2.5 Dedup / Fold Layer

- **Implementation:** `core/dedup/dedup.go`, `core/dedup/gate.go` (four-stage gate), `core/dedup/policy.go` (tiers).
- **Config schema:** `dedup` block (`enabled`, `window`, `category_tiers`, `category_windows`).
- **Protocol files:** none.
- **Copy:** `docs/architecture/modules.md` §11.
- **Lifecycle:** in-memory FIFO-capped tables (4096) keyed by event id / dedup key / state; restart clears them ("deliver again", never "silently drop").

### 2.6 Template Layer

- **Implementation:** `core/template/manager.go` (registry), `core/template/engine.go` (text/template execution), `core/template/renderer.go` (HTML/Markdown/plain/JSON renderers), `core/template/types.go` (Template/Field/Binding).
- **Config schema:** `templates: map[string]template.TemplateConfig`.
- **Protocol files:** none.
- **Copy:** `docs/guide/templates.md`, `docs/architecture/templates.md`.
- **Lifecycle:** in-memory registry; templates parsed lazily and cached by content hash (engine.go:24-40).

### 2.7 Queue / Worker Layer

- **Implementation:** `core/queue/queue.go` (interface), `factory.go` (memory|redis), `memory.go`, `redis.go`; `core/worker/pool.go` (pool), `registry.go` (worker registry).
- **Config schema:** `queue` block (`type`, `size`, `workers`, `timeout`, `redis`).
- **Protocol files:** none for queue; `protocol/message.go` for the remote-worker control plane.
- **Copy:** `docs/runtime/worker.md`, `docs/runtime/sdk.md`.
- **Lifecycle:** pool starts N local workers, registers them, runs a stale-worker sweeper; on cancellation workers deregister and return. Redis queue creates a consumer group on startup; scheduled tasks move from a ZSET to the stream ahead of `XREADGROUP`.

### 2.8 Runtime / Provider Layer

- **Implementation:** `core/runtime/manager.go` (registry+delivery), `core/provider.go` (Provider/Factory interfaces), `providers/builtin/registry/registry.go` (20 builtin factories), `providers/builtin/*` (concrete channels).
- **Config schema:** `providers: map[string]ProviderConfig` (type, config, enabled, rate_limit); the type→field schema map lives at `core/runtime/manager.go:64-84`.
- **Protocol files:** none (providers are HTTP/SDK adapters); worker runtime uses `protocol/message.go`.
- **Copy:** `docs/runtime/builtin.md`, `docs/providers/overview.md`, `docs/providers/*.md`.
- **Lifecycle:** `RegisterFactory`/`RegisterProvider` at startup; `Deliver`/`DeliverOnce` (sync vs async re-enqueue); `settle` (:558) maps outcome to delivery store + log store; `Close` (:632) closes all providers.

### 2.9 State / Persistence Layer

- **Implementation:** `internal/store/store.go` (SQLite credentials), `core/rules/file.go` (JSON rule file), `core/groups/file.go`, `core/audience/file.go`, `core/roster/file.go`, `core/escalation/escalation.go` (pending JSON), `core/rules/state_redis.go` (Redis rule state), `core/queue/redis.go` (Redis Streams), `core/digest/lock.go` (Redis lease); the rest are in-memory.
- **Config schema:** `auth.store_path`, `rules_store`, `groups_store`, `audiences_store`, `rosters_store`, `escalation_store`, `rules_state`, `queue.redis`, `digest.redis_*`.
- **Protocol files:** none.
- **Copy:** `docs/guide/configuration.md`.
- **Lifecycle:** stores open at startup (fail-fast on malformed files / unreachable Redis), close on shutdown; JSON stores write atomically via temp+rename; SQLite uses WAL and a single connection.

### 2.10 Audience / Relations Layer

- **Implementation:** `core/audience/surfaces.go`, `relations.go`, `audience.go`, `store.go`, `sources.go`, `filter.go`, `intensity.go`, `mode.go`, `preferences.go`, `reconcile.go`, `matrix.go`, `overlay.go`; `core/groups/*`; `core/roster/*`.
- **Config schema:** `audiences`, `recipients`, `audiences_store`, `groups`, `groups_store`, `rosters_store`, `sources`, `delivery`.
- **Protocol files:** none.
- **Copy:** `docs/design-audience-model.md`, `docs/design-audience-relations.md`, `docs/design-audience-boundaries.md`, `docs/design-audience-audit.md`, `docs/design-notification-groups.md`.
- **Lifecycle:** registries are in-memory with optional JSON persistence; source adapters and reconciler run on config-enabled entries only; audit recorder is always attached.

### 2.11 Apps / Callback Layer

- **Implementation:** `core/apps/apps.go` (registry+scopes), `policies.go`, `templates.go`, `callback.go` (callback URL policy); `core/callback/provider.go` (queue-side provider), `emitter.go` (settle/unsubscribe hooks), `event.go` (event kinds).
- **Config schema:** `apps: map[string]AppConfig` (tokens with scopes); `card_callback` (Feishu encrypt key).
- **Protocol files:** `docs/api/openapi.yaml` for the /api/v1/apps/** face; `apps-sdk/go/faces.go` is the typed client.
- **Copy:** `docs/api/apps.md`, `docs/guide/integration.md`.
- **Lifecycle:** registry seeded from config (validation point); callback emitter enqueues signed events into the same queue; provider posts them through the delivery pipeline with retry.

### 2.12 Auth / Operator Layer

- **Implementation:** `core/auth/auth.go` (API key + login + password), `core/jwt/jwt.go` (sessions), `core/user/user.go` (bcrypt users), `internal/store/store.go` (persisted credentials).
- **Config schema:** `auth` block (`enabled`, `api_keys`, `secret_key`, `admin_user`, `login_rate`, `login_burst`, `store_path`).
- **Protocol files:** `docs/api/openapi.yaml` (auth endpoints).
- **Copy:** `docs/guide/configuration.md` (authentication section).
- **Lifecycle:** in-memory users + optional SQLite store; JWT secret random per-process unless configured; dynamic key hashes cached and invalidated on writes.

### 2.13 Observability Layer

- **Implementation:** `core/metrics/metrics.go` (sampler), `core/logstore/logstore.go` (delivery log), `core/deliverystore/deliverystore.go` (lifecycle), `core/audit/audit.go` (audit trail), `internal/logger/logger.go`.
- **Config schema:** `metrics` block (sample interval, retention).
- **Protocol files:** none; API exposes `/api/v1/logs`, `/deliveries`, `/metrics/history`, `/audit`.
- **Copy:** `docs/architecture/coverage.md`, `docs/runtime/overview.md`.
- **Lifecycle:** sampler goroutine started by `newMetricsSampler` (cmd/heraldd/main.go:706), stopped on shutdown; log/delivery/audit stores are bounded in-memory rings.

### 2.14 Worker Control-Plane / SDK Layer

- **Implementation:** `core/websocket/server.go` + `hub.go`; `worker-sdk/go/client.go`; `protocol/message.go`.
- **Config schema:** `websocket` block (`addr`, timeouts, `allowed_origins`, `auth_token`, `max_message_size`); `worker` block (client side).
- **Protocol files:** `protocol/message.go` is the sole wire contract (register/register_ack/heartbeat/dispatch/ack/error/event).
- **Copy:** `docs/runtime/worker.md`, `docs/runtime/sdk.md`, `docs/architecture/protocols.md`.
- **Lifecycle:** server upgrades WebSocket, verifies origin + token, registers workers; hub forwards heartbeat/disconnect to the registry; worker SDK connects, registers, heartbeats and consumes the queue.

### 2.15 Frontend Layer

- **Implementation:** `dashboard/src/` (Vite + React + TS): `main.tsx`, `App.tsx`, `api/index.ts`, `stores/herald.ts`, `pages/*.tsx`, `layouts/DashboardLayout.tsx`, `theme/*`.
- **Config schema:** none (frontend reads the REST API); build config in `vite.config.js`, `vitest.config.ts`.
- **Protocol files:** none; it talks to `api/server.go` routes.
- **Copy:** none in-repo (the dashboard is a separate artifact); docs cover the API it consumes.
- **Lifecycle:** SPA bootstrap; single Zustand-style store holds auth + data; per-page fetch/refresh.

### 2.16 Docs Layer

- **Implementation:** `docs/.vitepress/config.mts` (site config + sidebar), `docs/**/*.md` (English), `docs/zh/**/*.md` (Chinese mirror), `docs/public/images/architecture-{en,zh}.svg`.
- **Config schema:** VitePress config in `docs/.vitepress/config.mts`.
- **Protocol files:** none.
- **Copy:** the docs themselves are the copy surface (English primary, Chinese mirror in `docs/zh/`).
- **Lifecycle:** static site built by `pnpm --dir docs build` (GitHub workflow `docs.yml`).

## 3. Why These Layers Exist (Evidence)

1. **One canonical pipeline path.** `NotificationService.Process` (core/service/notification.go:233) is the only place rules→routing→render→dedup→digest/feed→enqueue runs, so every entry (HTTP notify, app dispatch, digest flush, escalation, recovery) inherits identical semantics; splitting routing from delivery is explicit (core/service/planner.go, `expandRefs` at notification.go:644).
2. **Delivery state is separate from factual logging.** `deliverystore` owns the accepted/queued/delivering/retrying/delivered/failed/dead lifecycle (core/deliverystore/deliverystore.go:13), while `logstore` records only the event outcome (core/logstore/logstore.go:17); `runtime.settle` writes them independently (core/runtime/manager.go:558) so "dead vs failed" never leaks into the log vocabulary.
3. **Retry is policy, not provider logic.** `core/errclass` defines six classes (core/errclass/errclass.go:14) and `core/retry` decides purely from that vocabulary (core/retry/retryer.go:29 `isRetryable`), so no provider ever owns backoff; `Retry-After` advice is honoured with a cap (retryer.go:203).
4. **Queue is an interface with two backends.** `core.Queue` (core/types.go:169) plus optional `Scheduler` (core/types.go:184) let memory (core/queue/memory.go) and Redis Streams (core/queue/redis.go) swap without touching the worker; the pool selects async re-enqueue by capability (core/worker/pool.go:31).
5. **Runtime registry decouples provider identity from config.** `Manager` holds factories and instances separately (core/runtime/manager.go:27), so a config provider name never collides with a builtin factory and `ReplaceProvider` can hot-swap an instance (manager.go:710).
6. **Rules are compiled once, evaluated cheaply.** `compileExpr` is the only expression→program path (core/rules/engine.go:79), with AST guard, node cap and a 2s timeout (engine.go:46-66); the engine holds compiled rules in list order (engine.go:291) so priority is structural, not a sort at eval time.
7. **Stateful rule semantics have a swappable store.** `StateStore` (core/rules/state.go:20) is memory by default and Redis for multi-instance (`NewRedisStateStore`, core/rules/state_redis.go:22), so for-window/group/inhibit judgements survive restarts; rule definitions themselves persist via `Store` (core/rules/store.go:13) as memory or atomic JSON file (core/rules/file.go:19).
8. **Dedup is a four-stage ladder under one lock.** `Gate.Decide` (core/dedup/gate.go:146) runs idempotency → state machine → frequency tier → delivery and keeps a fold ledger, all FIFO-capped (gate.go:46), so the audit row can expand "this ×N" without unbounded memory.
9. **Audience identity is two registries, not one.** Contact surfaces (core/audience/surfaces.go:33) and relations (core/audience/relations.go:40) are separate because a handle can exist without an entitlement and vice-versa; `Filter` (core/audience/filter.go:13) intersects them at send time in §14 order.
10. **Pull channels are not providers.** `rss` classifies as L0 intensity (core/audience/intensity.go:13) and the service projects rss-classified deliveries into `feeds.Store` instead of enqueueing a provider task (core/service/notification.go:612), so readers pull on their own cadence.
11. **Digest and reconcile share one leader lease.** `digest.LeaderLock` (core/digest/lock.go:14) is a Redis SET NX PX lease that the flip loop (cmd/heraldd/main.go:641) and reconcile loop (cmd/heraldd/main.go:683) both use, so a fleet flips/sweeps exactly once per round without per-feature lock code.
12. **Escalation is time-driven, not event-driven.** Pending upgrades persist to JSON and are restored on restart (core/escalation/escalation.go:46), because the whole point is to act when nobody acks; the ack store is a separate interface (core/ack/ack.go:17) so escalation consults it before firing.
13. **Provider errors are classified once at the edge.** `httpclient` wraps HTTP 408/429/5xx as retryable (core/httpclient/client.go) and provider code uses `errclass.New`, so the retry layer never inspects provider internals.
14. **Webhook callbacks ride the delivery pipeline.** `callback.Emitter` (core/callback/emitter.go:22) turns settled deliveries and unsubscribes into signed events enqueued as normal tasks (core/callback/provider.go:15), giving callbacks the same retry budget and at-least-once semantics as any delivery.
15. **The worker control plane is one protocol.** `protocol/message.go` (protocol/message.go:19) is the only wire contract between core and remote workers; the server (core/websocket/server.go:55) and SDK (worker-sdk/go/client.go:21) both import it, so a message change is a compile-time break on both sides.
16. **Credential persistence is behind an interface.** `core/auth` depends on `PasswordStore`/`APIKeyHashes` (core/auth/auth.go:48-59) and `internal/store` implements them over SQLite (internal/store/store.go:10), keeping the auth package free of the sqlite dependency.
17. **Config is validated at startup, never clamped.** `config.Validate` (config/config.go:630) refuses unparseable tiers/windows and bad bindings; `expandRefs` and `Gate.tierFor` degrade only for callers that skipped validation (core/dedup/gate.go:104), so a bad config fails loudly instead of silently re-grading.
18. **The embedded facade reuses the same composition.** `herald.App` (herald.go:34) builds queue/router/runtime/service/pool exactly like `serveCmd` (cmd/heraldd/main.go:100), so library and CLI behavior cannot drift.
19. **The dashboard is a pure API client.** `dashboard/src/stores/herald.ts` holds all state and `dashboard/src/api/index.ts` the HTTP calls; pages never touch transport details, so the frontend can be replaced without changing the REST contract.
20. **Docs are split by language and section.** English lives at `docs/` and the Chinese mirror at `docs/zh/` (docs/.vitepress/config.mts:1), so each audience gets a stable path; architecture/runtime/api/guide/providers each have their own sidebar section.
