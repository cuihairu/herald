# Audience Domain Model: Phase 0-9 Refactor Audit (Archive)

> Status: **archive**. Continues the original "Audience Domain Model: Architecture Improvement Plan" (Phase 0-9, wrapped up; that page was rewritten on 2026-10-07 into the [audience domain model overview](./design-audience-model.md), and the original plan text is preserved in git history). Following the output requirements of Phase 0 of that plan (current implementation → target model → differences → files to change → compatibility risks), this document records the factual baseline and the per-batch execution log; every conclusion carries a file:line. Audience-layer expansion (unified subscription and delivery hub) is not tracked here — see the [relations detail design](./design-audience-relations.md) and the todo.

## 1. Current Implementation: The Complete Journey of a Notification

```
POST /api/v1/notify
  └─ api/handler_notify.go: handleNotify → core.Notification
      └─ core/service/notification.go: NotificationService.Process (147)
          ├─ rule engine evaluation (rules.Evaluate) → may rewrite channel / inhibit / hold / fold / silence
          ├─ static routing when no explicit channels core/route/router.go: Route(type, level)
          ├─ template rendering (template.Manager.Render)
          ├─ dedup (dedup.Check, keyed on content)
          ├─ enqueue (340): expands each channel reference one by one
          │    ├─ plain channel → provider instance
          │    └─ group:<id> → core/groups members (expandRef, 367)
          │    └─ enqueueOne (386) → service/planner.go: DeliveryPlanner.Plan
          │         one core.DeliveryTask per channel → queue.Push
          └─ escalation / incident ledger bypass
      └─ core/worker/pool.go: workerLoop (63)
          Pop → core/runtime/manager.go: Manager.Deliver (399)
              ├─ rate-limit wait → on failure NewTaskLog+UpdateStatus("failed")
              ├─ logstore.Add(pending)  ← the only status carrier
              ├─ retry.Retryer.Execute (synchronous retry, max 3 / exponential backoff)
              ├─ failure → UpdateStatus("failed", err); success → UpdateStatus("success")
          → queue.Ack / queue.Nack
```

### Fact list

| # | Stage | Current state | Evidence |
|---|------|------|------|
| 1 | Notification | `core.Notification`: ID/Type/Level/**Channels[]**/**Recipients map[key][]target**/TemplateRef/Params/Content{Title,Body}/CreatedAt. The API request `NotifyRequest` is identical name-for-name and shape-for-shape (+title/body) | core/types.go:18-28; api/handler_notify.go:11-22 |
| 2 | Delivery Task | `core.DeliveryTask`: ID/**Provider**/Targets[]/Payload{Kind,Content,ProviderTemplate,Raw}/Level/AlertID/**RetryCount**/CreatedAt. One task per channel per notify (group members expand per channel), `ProcessResult.TaskIDs/Accepted/Failed` aggregate them | core/types.go:37-49; service/notification.go:340-418 |
| 3 | Status model | **No explicit delivery state machine**. Status = the `logstore.TaskLog.Status` string: `pending / success / failed / shadow` (shadow is a rule-observation entry). Written only by runtime.Manager.Deliver and the rule-observation path; no enum, no attempts/next_retry_at | core/logstore/logstore.go:19-30,192; runtime/manager.go:423,444,448 |
| 4 | Retry | `retry.Retryer.Execute`: default max=3, exponential, initial 1s, max_delay 1m; **synchronous loop** (retries inside a single Deliver call), task.RetryCount accumulates; after the final Nack on failure it is **not re-enqueued** | core/retry/retryer.go:51-97; worker/pool.go:90-101 |
| 5 | Queue | Interface Push/Pop/Ack/Nack/Size/Close; implementations memory/redis | core/types.go:84-91; core/queue/memory.go, redis.go |
| 6 | Worker | local workerPool: Pop→Deliver→Ack/Nack; remote worker same interface; Registry manages heartbeat/expiry | core/worker/pool.go; core/worker/registry.go |
| 7 | Logs | **TaskLog is both the delivery status and the log (one set of data)**; logs keep no historical event stream; stats aggregate by level/provider/status; API: /logs, /logs/stats, /logs/{id} | logstore.go:169-211 (Stats); api/server.go:144-146 |
| 8 | Groups (audience `group:` form) | `groups.Group{ID,Description,Members[{Channel,Recipients}]}`, **single-level expansion, no nesting**; Manager implements `GroupResolver.ExpandGroup`; "group:id" expands in expandRef into per-member channel references; **a reference to a nonexistent group → that channel fails explicitly, the rest still go out**; API: /groups CRUD + groups_store persistence | core/groups/groups.go:32-46; service/notification.go:367-383,142-144; api/handler_groups.go |
| 9 | user:/Recipient/Endpoint | **Does not exist**. The `Recipients` field semantically holds a per-channel pinned target list (the address a provider expects), not an audience model | grep across the whole repo finds no `user:` level concept; types.go:23 |
| 10 | Channel | No standalone config block. Static routing = `routes` (by type) + `level_routes` (by level), `Route(type,level)` prefers type over level; an explicit `channels` in notify overrides both tables | core/route/router.go; config/config.go:24 |
| 11 | Config | Top-level keys: server/websocket/worker/auth/providers/routes/queue/retry/dedup/templates/rules(+store/policy/state)/groups(+store)/rosters_store/escalation_store/incident_limit/card_callback. **Lenient yaml parsing** (unknown keys silently ignored); `ExpandEnv()` supports `$VAR` | config/config.go:18-60,181,299-317 |
| 12 | Residual channel-semantics confusion | The points where vocabulary is shared with the target model: `channels` means "provider instance reference" in notify, rules and routing alike; Template Binding picks a format per channel (a partially decoupled-from-channel part does exist); ProviderCapability already exists (used for format selection) | service/planner.go:57-76,247 |
| 13 | Test baseline | handler_*_test.go complete (notify/groups/rules/rosters/logs/worker); service/planner/retryer/pool/logstore all have tests; coverage gate 100% (zero_check) | api/, core/ *_test.go |

## 2. Target Model vs Current State: Full Difference Table

| Target (from the original plan) | Current state | Gap |
|----------------------------------|------|------|
| Notification ≠ Delivery Task | Already separated (core.Notification / core.DeliveryTask) | ✅ semantically sound; TaskLog hangs the status off the task ✓ |
| Standalone Delivery state machine (accepted/queued/delivering/delivered/failed/retrying/dead) + attempts/max_attempts/last_error/next_retry_at | 4-value Status string (pending/success/failed/shadow); RetryCount already exists; retry is synchronous, nothing is queued | ❌ enum-ize the state machine + hang the status off Delivery (or an equivalent); next_retry_at additionally required to support "Nack then re-enqueue"-style asynchronous retry |
| Retry belongs to Delivery, errors uniformly classified (Temporary/Permanent/RateLimited/Auth/InvalidRequest/Timeout) | Errors are bare error; the retryer treats every error alike | ❌ provider error-classification interface missing |
| Audience = two levels, group + user | only the `group:` form | ❌ `user:`/Recipient/Endpoint must be added (MVP: local config model) |
| Standalone Channel config block (channels: {ci: {providers: [...]}}) | routes/level_routes maps carry the static mapping | ◑ capability-equivalent but a different model; can evolve selectively |
| Logs separated from Delivery status (log = historical event stream) | The log is the status (one set of data) | ❌ the highest-cost point of evolving "log/status separation" (requires introducing an event stream or splitting storage) |
| Idempotency (idempotency_key) | dedup is content-key window dedup (not an explicit idempotency key) | ◑ different semantics: dedup prevents duplicate delivery, an idempotency key prevents duplicate creation |
| Provider Capability | format-selection capability already exists (selectFormat/getCapability) | ✅ partially satisfies the Provider Capability recommendation in the plan |
| Config blocks: audiences/recipients/endpoints | do not exist (silently ignored by lenient parsing) | ❌ to be added |

## 3. Changed-File List (Phase 1+ reference, not a commitment)

| File | Direction of change |
|------|---------|
| core/types.go | Delivery status enum constants, TaskLog field extensions (attempts/next_retry_at), Audience reference types (unified group:/user: prefixes) |
| core/logstore/logstore.go | Enum-ize the status values (preserving JSON serialization compatibility); if "log/status separation" is done, split out an event-stream store |
| core/runtime/manager.go | Align the status write points with the new enum; error classification conversion (Temporary/Permanent/...) |
| core/retry/retryer.go | Decide retry/give-up per error class (backoff on RateLimited, straight to dead on Permanent); optional: asynchronous re-enqueue |
| core/service/notification.go, planner.go | expandRef gains `user:` resolution; Channel resolution layer (explicit channels → Channel block → routes compatibility) |
| core/groups/ → core/audience/ (new) | Carries the recipients/endpoints first level (group is retained as an audience aggregate) |
| config/config.go | channels/audiences/recipients config blocks (the lenient-parsing semantics that exist today: silently ignored until they land) |
| api/handler_notify.go | **Not forced to change this round** (notify's channel/audience/idempotency_key fields are all optional additions, backward compatible) |
| api/server.go, handler_groups.go | If audience/recipient endpoints are added (phase one can stay config-only, no API) |

## 4. Compatibility Risks and Guardrails

1. **The notify request/response stays compatible**: all new fields are optional; `channels` explicit semantics, the `code/message/data` response envelope, and `notification_id/task_ids/accepted/failed` are unchanged. A breaking change must follow the original plan's breaking-change procedure (explanation + migration + docs + tests).
2. **TaskLog Status string**: when extending from 4 values to an enum, **add only, never delete and never rename** (keep all of `pending/success/failed/shadow`; new statuses are additions), so existing dashboard and API consumers do not break.
3. The **groups API and `group:` references** stay as they are (`group:` semantics unchanged; `user:` is a brand-new prefix, each with its own resolution path).
4. **routes/level_routes** are retained as the equivalent of static routing; when the channels block is introduced, the "explicit channel > channels block > routes table" priority is layered on top — no existing key is deleted.
5. **Retry semantics**: the current synchronous retry is observable by the caller (one Deliver call blocks through the whole policy); switching to asynchronous re-enqueue changes end-to-end latency and log ordering — that needs its own design in Phase 3 and must not be changed in passing.
6. **Lenient yaml parsing** is a double-edged sword: the silent ignoring of new config keys before they land is already noted in the docs (configuration.md "domain model and config blocks"), so users are not left thinking they already take effect.
7. Every phase must pass the full gate before completion (Go race + covermerge gate 100 + golangci-lint + dashboard tests/build + docs build), consistent with the repo's existing discipline.

## 5. Execution Record (2026-10-04: all of Phase 0-9 landed, plan wrapped up)

The first two batches were delivered and passed the full gate (race all green / covermerge gate 100 GREEN / golangci-lint 0 issues / dashboard 100% / docs build passing); the third batch (Phase 6) cleared the same bar. Filling in the differences above item by item:

### Phase 1 MVP: `user:`-level audience (config-driven)

- `core/audience/` (new package): `Endpoint{Type,Target}` / `Recipient{Endpoints}` / `Audience{Recipients}`; `Manager.ExpandUser` resolution — **the audiences table takes priority**, falling back to the recipients table on a miss; `NewManager` startup validation (illegal id, an audience referencing an unknown recipient, a recipient with zero endpoints, an endpoint type/target that is empty or too long → error and refuse to start): a `user:` reference never silently falls through to nothing.
- `config` gains top-level `audiences` / `recipients` blocks (lenient parsing makes them take effect from here on; see configuration.md "domain model and config blocks").
- `core/service/notification.go`: `expandRef` gains a `user:` branch (a `UserResolver` interface plus `SetUserResolver`, structurally identical to GroupResolver); `mergeUserEndpoints` **merges endpoints per provider** — all targets on the same provider are bundled into one delivery, and provider order sorting keeps tasks/logs stable across runs. Unknown user / no resolver → that channel fails explicitly, the rest still go out (identical behavior to `group:`).
- `cmd/heraldd/main.go` wires up `audience.NewManager` (illegal table → exit code 1); `api.Config.Users` is passed through.
- Deliberately **not done** under the lightness principle: a standalone channels config block, the audiences/recipients runtime API (the idempotency request field was completed by the third batch, Phase 6 — see below).

### Phase 3 MVP: Delivery state machine (enum-ization + synchronous retry)

- The `core.DeliveryStatus` enum (accepted/queued/delivering/retrying/delivered/failed/dead) plus a `DeliveryTask.Status` field; the planner sets `queued` as soon as it creates a task.
- `core/retry/retryer.go`: a new `ErrMaxRetries` sentinel for an **errors.Is** determination of **dead** — a retryable failure whose retry rounds are exhausted returns `max retries exceeded: <last>` (message text unchanged from history, upgraded only to an unpackable marker); the retry rounds drive `RetryCount` (the number of completed retries) and `Status=retrying`.
- `core/runtime/manager.go`: sets `delivering` before each attempt; the terminal state is written per **delivered / failed (permanent failure and rate-limit abort) / dead (retryable error exhausted)**: `errors.Is(deliverErr, retry.ErrMaxRetries)` distinguishes dead from failed.
- **The TaskLog wire stays unchanged**: `pending/success/failed/shadow` are preserved verbatim — dead/retrying show up only in the DeliveryTask status, invisible to the dashboard and the logs API (guardrail 2 discharged in place).
- Error classification stays as it is per guardrail 5: the binary `RetryableError` / `httpclient.RetryableError` markers (HTTP 408/429/5xx, network errors) → retryable; everything else fails immediately (the earlier draft of this audit wrongly said "a 401 also retries 3 times" — the understanding has been corrected per the facts above). Asynchronous re-enqueue and `next_retry_at` are left to a later phase (they change end-to-end latency and log ordering and need their own design).

### Phase 6 MVP: notify API domain fields (channel / audience / data / idempotency_key)

Landed per the original plan's notify field design and idempotency design, with guardrail 1 (all new fields optional) satisfied — fully backward compatible:

- `api/handler_notify.go`: `NotifyRequest` gains `channel` (singular, unioned with `channels`), `audience` (an array of audience references, each item going through the existing `expandRef` chain — `group:` / `user:` / bare channel name), `data` (template data, merged with `params`, with `params` winning on a name collision), and `idempotency_key`; the merge logic is extracted into two pure functions `notifyChannels` / `notifyParams`, and the success response reuses an extracted `notifyData` (first delivery and idempotent replay share one construction).
- `api/idempotency.go` (new): a `notifyIdempotency` in-memory table — mutex + map + FIFO queue, capacity 1000 (`defaultNotifyIdempotencyCap`, evicting the oldest on demand); `get` / `put`, with a repeated put keeping the first record. Mounted by `NewHandler`, zero configuration.
- Idempotency semantics: only **successfully accepted** (`code 0`) results are recorded; on a hit → return the first `notification_id` / `task_ids` directly, **without touching the delivery pipeline** (the queue grows by zero); failed requests (422) are not recorded, so the same key can be corrected and retried. Valid for the process lifetime, cleared on restart (no persistence in the MVP, explicitly stated in the docs).
- Coverage: `handler_notify_test.go` gains 7 subtests (channels+channel union, channel alone, audience expansion of `user:` references, data merged into params, params winning on a name collision, data alone, idempotent replay/key independence/failures not recorded) + a table-driven `idempotency_test.go` (miss/roundtrip/repeated put keeps the first/FIFO eviction/capacity fallback/custom capacity).
- Design trade-off: idempotency lives in the handler layer rather than the service layer — the key is a request field and the hit path short-circuits before Process (dedup is content-fingerprint dedup, idempotency is request-key replay; the two have different semantics and neither replaces the other); the table stores no raw request text, only `service.ProcessResult`.

### Phase 2 MVP: Making the Routing boundary explicit (separating decision from execution)

The Phase 2 chain of the plan (Notification → Audience resolution → Channel routing → Delivery Task) was already covered by the previous batch (`group:`/`user:` expansion in `expandRef` + channel routing by Router/rules); what this batch completes is **the boundary itself**:

- `core/service/notification.go`: a new `expandRefs(refs)` — a **pure decision** step turning a reference set into delivery targets (it never touches the queue); a single reference that fails to resolve is recorded as that reference's `ChannelError` and the rest expand normally. `enqueue` now takes the decision first and performs the enqueue afterwards (`enqueueOne`), giving Routing (who receives what) and Delivery (Plan/Push/retry) an explicit seam in code. Zero behavior change.
- `core/service/routing_test.go` (new): `expandRefs` decision layer unit-tested directly — empty reference set, bare channel pass-through, mixed `group:`+`user:` expansion (`user:alice` merges endpoints per provider), a failing reference not blocking the batch, explicit failure when there is no resolver; zero contact with `ctx`/queue (pure-function testability is itself the proof that the boundary holds).

### Phase 4 MVP: Five-point Queue / Worker audit + delivery field backfill

Audited point by point against the original plan (enqueue/dequeue/ack/worker/retry):

- **enqueue / dequeue / ack**: the `core.Queue` interface (Push/Pop/Ack/Nack/Size/Close) and the memory queue implementation are complete (buffered chan, closed guard, ctx cancellation; in the memory queue the ack semantics = Pop dequeues, and Ack/Nack are no-ops by design); worker success→Ack and failure→Nack are already wired. **No gap**.
- **worker**: `Pool.workerLoop` only does "take a task → call the Provider → Ack/Nack", carrying no routing/template/recipient knowledge (the worker boundary holds; routing decisions were separated in Phase 2). **No gap**.
- **retry**: synchronous retry lives inside runtime.Deliver (Phase 3), one Pop covers the whole retry cycle, exhaustion → dead. **No gap**.
- **Delivery field audit backfill**: the plan recommended Delivery store `attempts` / `max_attempts` / `last_error` / `next_retry_at`. The check found `last_error` and `max_attempts` exist only in the retry policy / task logs, not on the task object. This batch completes it:
  - `core.DeliveryTask` gains `MaxAttempts int` (a delivery-budget snapshot: retry-policy rounds plus the first attempt; 1 when there is no retryer, pinned by runtime before the first attempt) and `LastError string` (the terminal error: the final error is written on failed/dead, cleared on delivered, likewise recorded on a rate-limit abort; coexisting with the task log's full history — this is exactly the plan's "Delivery carries its own last_error");
  - `core/retry.Retryer.MaxAttempts()` (= MaxRetries()+1) exposes the budget;
  - `next_retry_at` still not done: synchronous retry completes within a single Deliver and has no queueing semantics (declared in the Phase 3 execution record; held for the asynchronous batch).
- **Audit tests**: `core/worker/pool_phase4_test.go` (new) — a five-point end-to-end delivery audit through a real runtime + a real worker loop (success→delivered+ack, failure→failed+nack+last_error, retryable exhaustion→dead with assertions on budget/retry count/residual error; synchronized through a mutex observation point on the queue, race-clean); the four terminal-state tests in `manager_status_test.go` gain MaxAttempts/LastError assertions; `retryer_test.go` gains a MaxAttempts budget test.
- Zero new external dependencies (original plan: keep MemoryQueue as the first queue, leave the Redis queue alone, introduce no new backend).

### Phase 5 MVP: Provider error classification (six-class vocabulary feeding the retrying/dead decision)

Landing point: the shared error vocabulary **core/errclass** plus two **single-point** integrations in httpclient and retry — every provider that goes through httpclient (aliyunsms/neteasesms/tencentsms) gets classification with zero changes.

- `core/errclass` (new package): six `Class` constants (temporary / permanent / rate_limited / authentication / invalid_request / timeout) plus an `Error{Class, Err}` wrapper. **Classification rides the error chain, not the error's clothing**: `Error()` returns the wrapped text verbatim (task logs and API responses keep their historical shape), `Of()` sees through multiple layers of `%w` wrapping via errors.As, `Retryable()` decides — the first three classes are retryable, the last three fail immediately.
- `core/httpclient` single-point integration: non-2xx → `classifyStatus` (408→timeout, 429→rate_limited, ≥500→temporary; other 4xx stay **unclassified** — retrying a bad request or bad credentials can never succeed; the semantics are unchanged, merely made explicit); transport-layer errors → `sendError` (net.Error timeout→timeout, the rest→temporary); error text unchanged.
- `core/retry` integration: `isRetryable` becomes **errclass-first** (with a classification use it, without one fall back); the legacy `RetryableError` / `httpclient.RetryableError` markers are kept for compatibility (existing tests pass unchanged).
- Tests: errclass unit tests (nil passthrough / text fidelity / seeing through wrapping / an 8-case classification table); `TestClassifiedStatusErrors` (7 status codes→class + IsRetryable consistent with the class); `TestRetryerClassifiedErrors` (production-shaped policy: temporary/rate_limited/timeout exhausting→ErrMaxRetries over 3 calls total; permanent/authentication/invalid_request→one call and done, not dead); `TestDeliverClassifiedErrorTerminals` (end-to-end: authentication→failed with no retry, rate_limited→retries exhausted→dead).
- Not done (held per guardrail 5): 4xx such as 401 still do not retry (semantics identical to history); RateLimited still has no per-class backoff (Retry-After unused, the uniform backoff policy stands).
  - **Later-batch completion (2026-10-05)**: Retry-After is now integrated — `errclass.Error` gains a `RetryAfter` hint, `httpclient.statusError` parses `Retry-After` on a 429 (delay-seconds / HTTP-date; non-positive values and past instants are not counted as a suggestion), and on the retry side the hint takes priority over the backoff curve, capped by `retry.max_delay` (no cap when MaxDelay is 0); `NewWithRetryAfter` / `RetryAfterOf` / `parseRetryAfter` have a unit test per branch + a seam test for the retryer's hint path.

### Later batch: Asynchronous re-enqueue (2026-10-05)

Deferred item 1 landed after its own design under guardrail 5: a retryable failure no longer pins a worker through the whole backoff curve — the task returns to the queue carrying `next_retry_at`, the queue releases it when due, and the worker immediately picks up the next task.

- `core/types.go`: `DeliveryTask` gains `NextRetryAt *time.Time` (stamped when deferring, cleared as soon as the next attempt starts; wire field `next_retry_at` omitempty, so the worker protocol stays backward compatible); a new optional-capability `Scheduler` interface (`Schedule(task, delay)`) that does not enter the main `Queue` interface — a custom queue with no delay-holding capability keeps the synchronous semantics verbatim.
- `core/retry`: `Defer(task, err)` makes a single decision — retryable with budget remaining → `RetryCount++`, `Status=retrying`, `NextRetryAt=now+delay`, return `*Deferred{Delay, Err}`; budget exhausted → `ErrMaxRetries` wrapping (dead); non-retryable → returned as-is (failed). `waitFor` is extracted as shared code: the Retry-After hint and the `max_delay` cap rules are identical on the synchronous and asynchronous paths. The budget (`RetryCount`) travels with the task, accumulates across re-enqueues, and survives redis serialization.
- `core/queue`: the memory implementation holds delays (a min-heap; Pop releases due items before looking at the ready channel, so backoff tasks are not starved by new traffic; the delay heap has its own mutex — Push holding RLock while blocked on a full channel would self-deadlock if Pop's hot path took the write lock); the redis implementation uses the sorted set `<stream>:delayed` (score = due UnixNano; ZREM arbitrates across processes first, the loser does not touch the entry; undecodable members are discarded to protect the read loop; a ZRANGEBYSCORE failure is raised as a Pop error). Both implementations drop non-positive delays straight into the ready queue; Close on either does not drain undelivered tasks.
- `core/logstore.AddIfAbsent`: one log line per task — it stays at `pending` during retries and only flips to success/failed at the terminal state. The log wire vocabulary (success/failed/pending/shadow) changes by zero, honoring the principle "do not treat the log directly as the status".
- `core/runtime`: provider lookup / budget snapshot / rate-limit wait are extracted from `Deliver` into `deliverPrologue`, terminal-state mapping into `settle`, and a new `DeliverOnce` is added on the shared skeleton (exactly one attempt; a Deferred is not settled, and the log line goes through AddIfAbsent).
- `core/worker`: `NewPool` probes the queue's capabilities — with a `Scheduler` it uses `DeliverOnce` + `Schedule` (no Nack: a delayed retry is not a failure); if Schedule is rejected → fall back to Nack to settle, and the task does not fall into limbo. Without the capability it falls back to `Deliver` (synchronous backoff, historical behavior verbatim). The `awaitingQueue` decorator in `herald.go` forwards `Schedule`, so library mode does not misjudge the capability through the wrapper and degrade.
- Semantic changes (the timing changes guardrail 5 names, recorded faithfully): the retry wait no longer occupies a worker; `DispatchSync`'s wait suspends across a defer and only resolves at the terminal Ack/Nack (total blocking duration unchanged); the redis queue's wait survives a process restart (the memory queue dies with the process, same as the original semantics).
- Tests: the Defer table (hint/cap/no-cap/both terminal paths/budget exhaustion across calls), memory Schedule (clock seam: ordering by due time / due items before ready / cancellation / close rejection), redis Schedule (hold/due transfer/loss-race arbitration/poison member/read failure), manager `DeliverOnce` (defer→resumed settling on the same log line / terminal table / immediate failure without a retryer), the pool asynchronous end-to-end path (re-enqueue / rejected→Nack / synchronous fallback / capability probing), logstore `AddIfAbsent`, and awaitingQueue forwarding.

### Phase 7 MVP: Configuration (standalone `channels` block + the three-level priority stack)

Landing point: the standalone `channels` config block (`channels: {ci: {providers: [...]}}`) lands, guardrail 4's "explicit channel > channels block > routes table" priority is made explicit, and the routes/level_routes keys are preserved verbatim.

- `config.ChannelConfig` (new): a top-level `channels` block — named channel → providers list; `Validate()` startup validation: a channel with zero providers, or a reference to an unconfigured provider → **refuse to start** (same discipline as the Phase 1 audience tables); `ChannelRoutes()` flattens into the router's shape.
- `core/route.Router.ExpandChannel` (new): channel block lookup (returned in configured order; an empty entry counts as a miss — falling back to the bare provider target with its familiar "provider not found" failure, never silently sending nothing).
- `core/service` resolution chain (`expandRef`): a bare channel name resolves by priority — **explicit provider instance > channels block**; when neither matches it stays a literal provider target (failing at delivery time, semantics unchanged). The `routes` table sits below that: it only routes notifications that name no channel (existing behavior). `group:`/`user:` references and synthesized deliveries (summarize/escalate/recover) all go through the same resolution chain, with zero special cases.
- Wiring: `cmd/heraldd` injects the flattened block when building the router and logs `channels loaded`; `api/server.go` mounts `SetChannelResolver(config.Router)`.
- Tests: router expansion (hit order/unknown/empty entry returns false); config validation (no providers/unknown provider/valid) + yaml parsing + flattening; the service decision layer (ordered block expansion, provider beating the block, falling back to literal when neither matches) + end-to-end (`channels: [ci]` → one task per provider); two heraldd refuse-to-start cases + FullFeaturedLifecycle with a channels block.
- Not done (held per guardrail 5 / out of scope): the channels runtime API (config-only is the MVP, consistent with the phase-one position on audiences); group member channel names do not go through block expansion (the member semantics is a provider instance name, kept as-is).

### Phase 8 MVP: Test completion (seven test blocks audited one by one + Multi Provider isolation)

Audited the existing test baseline block by block against the seven test blocks in the original plan — **six already existed**, one was a genuine gap:

| Test block | Requirement | Baseline (audit evidence) |
|--------|------|-----------------|
| Notification | creation + parameter validation | `TestNotificationService_Process`; handler_notify_test's invalid body / 422 / unknown channel and template |
| Audience | group→recipients, user→recipient, recipient→endpoints | `TestProcessGroupReferences` (members + recipient pinning); `TestExpandUser`/`TestExpandUserAudiencePrecedence`; `TestProcessUserReferences`/`TestMergeUserEndpoints` (endpoints merged per provider) |
| Routing | Notification+Audience+Channel → Delivery Tasks | `TestExpandRefs` 5 scenarios + `TestExpandRefsChannelsBlock` + `TestProcessChannelBlockReference` + the full group:/user: chain |
| **Multi Provider** | telegram success does not duplicate / feishu failure can retry | **gap → filled in this batch**: `TestPoolMultiProviderFanOutIsolation` (core/worker/pool_phase8_test.go) |
| Retry | Temporary→retry / Permanent→immediate failure | Phase 5: `TestRetryerClassifiedErrors` + `TestDeliverClassifiedErrorTerminals` |
| State | queued→delivering→delivered; failed→retrying→delivered | Phase 3/4: `TestDeliverMarksStatusDelivered` + `TestDeliverMarksEveryAttemptDelivering` + pool_phase4 |
| Idempotency | the same idempotency_key → no duplicate Delivery created | Phase 6: handler replay (the queue grows by zero) + the table-driven `idempotency_test` |

The added tests run the **full real chain** (service routing → planner planning → queue → worker → provider): one fan-out sends to two providers with independent terminal states — telegram succeeds on the first attempt and is delivered exactly once (a sibling task's failure does not duplicate it), feishu fails retryably on the first attempt and lands on the second (delivered rather than dead, RetryCount 1, LastError cleared on delivery), and each task logs success on its own line. The count assertions pin down the original plan's "no duplicate / retryable" semantics.

### Difference table backfill (after this batch)

| Target (design-audience-model.md) | Status after landing |
|----------------------------------|-----------|
| Standalone Delivery state machine + attempts | ✅ enum-ized + driven by measured `RetryCount`; `next_retry_at` + asynchronous re-enqueue have landed (2026-10-05 later batch: `Scheduler`-capable queues hold the retry wait, and the worker no longer sleeps through the backoff) |
| user:/Recipient/Endpoint | ✅ config-driven MVP (audiences/recipients YAML + `user:` reference expansion and merge) |
| Config blocks audiences/recipients | ✅ in effect + startup validation (a first precedent for "lenient parsing takes effect on landing") |
| notify domain fields channel/audience/data/idempotency_key | ✅ all optional fields shipped backward compatible (Phase 6: union expansion + data/params merge + in-memory idempotency table) |
| Provider error classification interface | ✅ the six-class vocabulary in core/errclass feeds httpclient classification and the retry decision (Phase 5: 408/429/5xx/transport errors explicitly classified, other 4xx unclassified and not retried, wire text unchanged) |
| Logs separated from Delivery status | untouched (the highest-cost item; "log/status separation" needs an independent phase to design an event stream) |
| Standalone Channel config block | ✅ `channels: {name: {providers: [...]}}` landed (Phase 7: startup validation refuses to start, explicit provider > channels block > routes table priority, routes keys not deleted) |

### Phase 9 MVP: Documentation (site-wide alignment + difference table wrap-up)

Audited the seven documentation surfaces of the plan's Phase 9 one by one. Zero code changes, a pure documentation batch:

- **Getting Started** (getting-started.md): the current-implementation section was rewritten into the terminal "all of Phase 0-9 landed" state (the channels block, the six error classes, MaxAttempts/LastError folded in); the deferred items are named and point at the audit; step 3's integration checklist gains three pointers — `user:` audiences, `channels` named channels, and `idempotency_key` idempotent resend.
- **Architecture** (architecture/overview.md): the boundary/current-state section was rewritten from "in progress + three follow-up plans" into the landed terminal state — the standalone Channel config block and the error classification interface **have landed** (previously stale wording); only asynchronous re-enqueue and the Logs event-stream separation remain deferred.
- **Configuration**: synced in the Phase 7 batch (the domain model and config blocks table all ✅ + a channels section + a rewritten lenient-parsing warning); this batch's re-check found nothing new.
- **Provider** (providers/overview.md): a new "error classification and retry" section — the six-class vocabulary table (trigger / whether it retries), the httpclient auto-classification criteria (408/429/5xx/transport errors vs. other 4xx), SMS business-code differences pointed at each channel's page, and an explanation of the separation between dead/last_error and the log wire vocabulary.
- **Audience** (introduction.md): the stale wording "`user:`-level segmentation is planned" was corrected to landed-and-config-driven.
- **Delivery**: the state machine doc was updated alongside the Getting Started / Architecture terminal state; the API does not expose the task object (the queue endpoints only report size), so there is no API documentation change; troubleshooting.md fixes one **documentation error** — the `status` field of `/api/v1/logs` is actually `success/failed/pending/shadow` (the wire vocabulary), and the original text wrongly wrote `delivered/failed/retrying`.
- **API** (api/rest.md): synced in the Phase 6 batch (notify domain fields + the idempotency section); this batch's re-check found nothing new.

**Difference table wrap-up**: every row converges to either "✅ landed" or "deferred + reason", with no undecided gaps. The deferred list (all explicit design decisions, not omissions):
1. Logs separated from Delivery status — the highest-cost item of "log/status separation" (an event stream or a storage split); one set of data read two ways already satisfies the need today.
2. The audiences/recipients/channels runtime API — the phase-one position is config-only (recorded in the Phase 1/7 records).
3. Group member channel names do not go through channels block expansion — the member semantics is a provider instance name (deferred in Phase 7).

> 2026-10-05 update: "RateLimited per-class backoff (Retry-After)" and "`next_retry_at` / asynchronous re-enqueue" from the original list have both landed (see respectively the later-batch completion in the Phase 5 record and the new later-batch section added today); 3 items remain on the list.
>
> 2026-10-05 decision: the remaining 3 items are confirmed to stay as they are — "Logs separated from Delivery status" and "the audiences/recipients/channels runtime API" remain deferred and unimplemented, and "group member channel block expansion" stays decided as not-done; the queue is thereby emptied, and new directions are assigned separately.
>
> 2026-10-05 explicit decision record: by user decision, **① Logs separated from Delivery status** (item 1 of the list) and **② the audiences/recipients/channels runtime API** (item 2 of the list) are explicitly **not implemented**; item 3 **group member channel block expansion** was already decided as not-done (recorded in Phase 7). All three are explicit design decisions, not omissions; this audit document and todo.md have been closed out in sync, and the queue is emptied.

> 2026-10-13 unfreeze: item 3 (group member channel block expansion) was assigned to be built by the patrol, and member channels were wired into the existing resolution chain ([unfreeze plan](./design-audience-deferrals.md) ③, option B); ① and ② remain unimplemented.

## 6. Status Lines Against the New Model (2026-10-07)

The table above covers the Phase 0-9 pipeline refactor, and every row still holds. From 2026-10-05 the positioning was upgraded to "unified subscription and delivery hub", and the audience layer keeps expanding along the 11 atomic batches of [relations detail design](./design-audience-relations.md) §15, with terminology governed by §2 of the [overview](./design-audience-model.md). Status lines for the new dimensions:

| New model capability | Status | Evidence |
| --- | --- | --- |
| Relation model and audience registry (subscription/enrollment, Policy slots, Subscribe/Enroll under distinct names) | ✅ landed (batch 1) | `core/audience.Registry`, commit `3fc1a82` |
| Contact surfaces and binding (three-state ContactSurface, one-time token, old-channel confirmation on rebind, private RSS token) | ✅ landed (batch 2) | `core/audience.SurfaceRegistry`, commit `19cf16c`; the queue's due-wait got an injected clock in the same batch (`342caa1`) |
| Preference center (Frequency/Preference/DefaultPolicy, default policy table, read/write API) | ✅ landed (batch 3) | the preference model in `core/audience`, commit `4df837a` |
| Channel×relation matrix and delivery filtering (four families, mandatory Enroll gate, re-check before sending, Filter.Allow intersection) | ✅ landed (batch 4) | `core/audience.Filter`/`MatrixAllows`, commit `4b18b4a`; the expandRef hookup is triggered by the batch 11 send surface |
| Delivery audit completion (relation/contact-surface change trail, dedup folding detail, relation snapshot into task and log) | ✅ landed (batch 5) | `core/audit`, `SetRecorder`/`SetFoldAudit`, commit `de05e94`; the send-side relation context follows batches 8/11 |
| Digest time-window aggregation (window flip, exemption arbitration, digest template, flip loop and optional redis lease) | ✅ landed (batch 6) | `core/digest`, `SetDigest`/`FlushDigest`, commit `3204e90` |
| RSS pull (batch 7) / source adapters (batch 8) | ✅ landed | `core/feeds`+`/feeds/**` (commit `a96e1a1`), `core/audience.SourceAdapter`/`Reconciler`+ callback endpoints (batch 8, commit hash recorded in the todo) |
| Intensity and delivery modes / dedup frequency control (three tiers) / integrator API and Go SDK | ◑ in flight (batches 9-11) | relations detail design §15 |
| Dedup (the pre-existing part) | ◑ content-fingerprint dedup and the request idempotency key have shipped | audit §1 #13, the Phase 6 record |

This document is sealed here as the pipeline-refactor archive; execution records for the new batches go into the todo and the corresponding sections of the relations detail design, and are not written back into this table.
