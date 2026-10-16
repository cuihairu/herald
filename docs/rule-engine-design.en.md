> English · [中文](./rule-engine-design.md)

# Notification Rule Engine Upgrade Design

> Status: **trunk capabilities implemented** (the trunk capabilities of P1–P3 are all landed, cross-checked item by item against main on 2026-09-27, receipt at the end "Implementation Receipt"). The "status quo" descriptions at the time of writing are kept as a pre-implementation snapshot; the gaps that have since closed (the orphan limiter module, retry not retryable) are explained in the receipt section. The receipt review turned up two more deviations between design and landing (shadow statistics were only an in-process cumulative count, the silence window had no timezone or on-call schedule), which have been rewritten per the facts and marked cell by cell in the table; both deviations have since been filled in (shadow window counts 2026-10-03, silence timezone 2026-10-03, on-call schedule placeholder 2026-10-04). **The "Difference" column in the receipt table is verifiable cell by cell, not a single "implemented"**.

## 1. Positioning: From "Event-Driven Delivery" to a "Programmable Alert Platform"

**Status quo**: herald today is roughly **ntfy + channel breadth**. The caller (CI, monitoring scripts, business code) comes with an explicit intent `POST /api/notify` (`HandleNotify` in `api/handler_notify.go`), and herald's job is to take this `Notification` (`core/types.go`) through dedup, routing, template rendering, task splitting (`Process` in `core/service/notification.go`), push it into the queue, and finally have the worker pool deliver it to 14 classes of built-in channels (`providers/builtin/`: aliyunsms, dingtalk, discord, email, feishu, log, neteasesms, slack, telegram, tencentsms, webhook, wechat, wechatmp, wecom, supporting multiple instances of the same name). In this chain **every single thing is "what to do when an event arrives"**: herald itself makes no judgment, all judgment is in the caller.

**Why this is a problem**: the judgment logic of alert scenarios ("failure rate over 5% and in production", "lasting 3 minutes before it counts as a real fault") is forced upstream into every caller. Ten teams onboarding means writing the same judgment ten times, and changing a threshold means shipping ten times. The callers are usually CI scripts that do not understand alert discipline, and the result is alert storms. The cost of the storm is borne by the channels (Feishu flooding, SMS burning money).

**Upgrade goal**: make it "**an Alertmanager whose rules can be configured from the backend, but with its own channel breadth**". Alertmanager's route-tree/inhibit/silence model has been production-validated over ten years, but its rules are hardcoded in a config file, and changing one route requires a release; herald's existing channel breadth (including domestic IM/SMS) is something Alertmanager does not have. The join point of the two is exactly: **bring Alertmanager's judgment model in, and move the configuration authority of judgment from the config file to the database backend**.

**The evolution path has four layers**, each producing value independently, without a big-bang rewrite:

1. **Static config routing** (status quo): `routes`/`level_routes` in yaml (`core/route/router.go`), changing config requires a restart;
2. **Dynamic rules**: rules stored in DB, backend CRUD + hot reload, match conditions upgraded from "type equality" to "expression evaluation";
3. **Stateful judgment**: `for` duration judgment, `group_by` aggregation, inhibit. Judgment depends on historical events, no longer a pure function;
4. **ACK closed loop**: sending an alert is not the end point; acknowledge, timeout escalation, and incident records form a closed loop.

## 2. Where the Existing Modules Sit in the New Architecture

The upgrade is not a teardown and rebuild. The five existing core modules happen to form the lower body of the new architecture; let me explain one by one where they stay and why they stay there:

| Module | Status quo (real code) | Role in the new architecture | Why it |
|---|---|---|---|
| `core/route` | static map: `Route(type, level)` looks up the channel list first by type then by level; `SetRoute` is runtime-changeable but in-memory only | kept as the **fallback route**. The channel list produced after a rule-engine hit takes priority, and traffic with no rule hit falls back to the existing static route | the fallback route guarantees that "when the rule engine is fully down / matches nothing" system behavior is fully identical to today, the safety net of incremental migration |
| `core/dedup` | in-process map + time window (default 5 minutes), key is the sha256 of the notification content (`dedupKey` in `notification.go`) | kept, position **moved from before ingress to after rule evaluation**; once stateful rules go live the underlying storage shares the Redis scheme with rule state (see §4.1) | both dedup and duration judgment are "making a decision on the event sequence of the same logical entity", and splitting them into two layers makes each go its own way; but dedup's existing semantics are simple and reliable, and the rule engine need not rebuild it |
| `core/limiter` | TokenBucket (`limiter.go`) fully implemented (Allow/Wait/Reservation), `Manager` manages by provider dimension; **currently no caller is wired, it is an orphan module** | formally wired to the delivery side: the rule engine amplifies the delivery volume of a single event (one event hitting N rules), and channel rate limiting goes from "advisory" to "mandatory" | the biggest operational risk of the rule engine is a miswritten rule causing a channel storm, and limiter is the last safety valve; the module is written and only lacks wiring, the highest value-for-effort step in the upgrade |
| `core/retry` | `Retryer.Execute` + exponential/fixed backoff; note the status quo: `ShouldRetry` only recognizes `*RetryableError`, and no provider in the codebase constructs that type, **so currently retries almost never actually happen** | the rule engine does not change it, but P1 incidentally fixes the "which errors are retryable" judgment (wrap a `NewRetryableError` when a provider returns 429/5xx) | the upgrade raises per-channel event density, and an unfixed retry semantics amplifies a failure-retry storm; this is known debt, repaid along with the wiring |
| `core/logstore` | in-memory ring log (default 1000 entries), records task success/failure in `runtime.Manager.Deliver`, queryable via `GET /api/logs` | extended into the **data sink for shadow mode**: rule-evaluation results (hit/miss, would-have-fired) written as a new log type for backend preview | the core requirement of shadow mode is exactly "record it but do not send it", and logstore already has a record-query-statistics skeleton (`Stats`/`Filter`), so adding fields saves more effort than building new storage |

Not directly involved but unchanged: `core/queue` (memory/redis dual implementation, Queue interface Push/Pop/Ack/Nack), `core/template` (template + channel Binding, the rule engine only decides "whether/where to send", it does not touch rendering), `core/runtime.Manager` (delivery orchestration: rate limiting not wired, retry here, logging here, see `Deliver` in `manager.go`), `core/worker` (Pool consume loop). **The rule engine is entirely placed before enqueueing and on the backend face, with zero change to the queue and delivery chain**. Why: the delivery chain is the most stable part of the current system, coverage and tests are built around it, changing it has the highest risk and zero benefit.

## 3. Rule Model

The complete shape of a rule (YAML representation, actually stored as a DB row):

```yaml
id: prod-payment-failure
match: payload.fail_rate > 0.05 && env == "prod"   # expression evaluation
for: 3m                                            # duration judgment anti-flap
group_by: ["env", "service"]                       # aggregation dimensions
inhibit:                                           # inhibit
  - match: cluster_down == true
    equal: ["env", "cluster"]
route:                                             # channel selection (reuses the existing 14 channel classes)
  - channels: [feishu-oncall]
  - match: level == "critical"
    channels: [wecom-boss, aliyunsms-duty]
escalation:
  ack_timeout: 5m                                  # escalate if no one acknowledges
  to: [phone-bridge]
silence:                                           # silence window
  - window: "00:00-06:00"
    tz: "Asia/Shanghai"
    match: level != "critical"
```

The following writes out field by field **why it must exist**, along with design trade-offs:

### match —— expression matching

**Why**: static routing can only ask "what type is the notification", whereas a rule must be able to ask "what does the notification's content mean". `payload.fail_rate > 0.05 && env == "prod"` hands the decision authority to whoever writes the rule, and herald is only responsible for safely executing the expression (execution safety see §4.2). The match input is the notification's structured view (`Notification.Params` + `Level` + `Type` + channel-agnostic metadata), not the rendered text. Regex-matching text is fragile — one template change and all rules go mute.

### for —— duration judgment anti-flap

**Why**: single-point events are mostly noise. A disk alarm flashes once and disappears, a Pod restarts and recovers on its own. If you send every time, people start ignoring notifications after the 10th time, which in alert engineering is called the "boy who cried wolf" effect, worse than not sending alerts at all (users will shield real faults along with it). `for: 3m` means the condition of the same logical alert (grouped by group_by) **must hold continuously for 3 minutes** before it truly fires. The cost is a 3-minute alert delay, the benefit is an order-of-magnitude reduction in noise. The value density of an alert matters more than its timeliness, and false positives are the biggest enemy of timeliness.

### group_by —— aggregation

**Why**: one cluster going down produces alerts for 200 Pods, and without aggregation that is 200 Feishu messages. `group_by: ["env", "service"]` folds same-group events into one group notification (the in-group count increments, the first entry's detail + subsequent count updates). The choice of grouping key is operations semantics: too coarse (only by env) mixes unrelated faults together, too fine (by Pod name) loses the meaning of aggregation, so group_by must be configurable — Alertmanager's ten years of experience prove there is no universal default.

### inhibit —— inhibit

**Why**: when a cluster goes down, dozens of "service unreachable" sub-alerts follow. They are all true, but they all have no action value, and a person only needs to handle the root cause. Inhibition is "suppress lower-level alerts while a higher-level alert is present": when the rule with `cluster_down == true` fires, all sub-rules whose `equal` fields match (same env, same cluster) enter the inhibited state. **Why not filter them out with match**: sub-alerts still need to be recorded, and still need to be re-sent as a summary after the root cause recovers; inhibition is "delayed display" rather than "discard". This is also why it must be a separate field rather than letting the user change match.

### route —— channel selection

**Why**: different levels of alerts going to different channels is the core of alert discipline: critical calls a phone, warning goes to the on-call group, info only goes to the log. The `route` array is evaluated in order, and the first `match` hit takes effect; channels directly reuse the existing provider instance names (`runtime.Manager` already supports multiple instances, e.g. the two Feishu groups `feishu-oncall`/`feishu-boss`). **Why keep static routing**: existing callers already explicitly specify `channels` or rely on `level_routes`, and rules are an incremental capability rather than a mandatory gate. A notification with no rule hit behaves byte-for-byte identically to today.

### escalation —— timeout escalation

**Why**: an alert that nobody looks at is the same as not sent. `ack_timeout: 5m` means that if no one acknowledges within 5 minutes of firing, escalate to the next channel (`to: [phone-bridge]`). It separates "the notification was delivered" from "the incident has an owner": the former is the delivery system's job, the latter is the alert system's job, and herald is to do the latter. The implementation of the escalation chain depends on ACK state (P3), so this field is only modeled and not effective in P1/P2, avoiding a false promise.

### silence —— silence window

**Why**: a routine 3 a.m. inspection report should not wake anyone, but a core-service alert on Friday afternoon must broadcast immediately. The silence window (a combination of time period + match condition) makes "at what time which alerts may stay silent" an explicit configuration rather than a person's self-discipline. Difference from inhibit: inhibit is event-driven (suppress only while a higher-level alert is present), silence is schedule-driven (suppress when the time arrives); the two cannot substitute for each other, and an on-call scenario needs both.

## 4. Three Architecture Points

### 4.1 Evaluation Moved Up to ingress, State Externalized to Redis

**Evaluation location: before enqueueing, not after dequeueing.** Rule evaluation is placed in `NotificationService.Process` after the dedup check and before route resolution. Why: an event held back by a rule **consumes no queue or worker**. If a false-positive storm were filtered on the worker side, the queue would be flooded and legitimate events delayed in line, transferring the cost of filtering to the whole system; whereas holding it back before enqueueing costs only a few milliseconds of expression evaluation. Another reason is that state judgment (for/inhibit) needs the ability to "reject": when the evaluation result is "inhibited / duration not yet satisfied", the event should not become a task at all.

**Stateful rule storage must be externalized to Redis, not put in process memory.** `for` must remember "when this group first hit", inhibit must remember "which higher-level alerts are present", escalation must remember "when to escalate". If this state were kept in a process map like `core/dedup`, scaling out workers would lose the state: two workers each maintaining "first-hit time", the same group of alerts might never accumulate a full 3-minute duration, or each would fire causing duplicate alerts. Key design:

```
rule:{rule_id}:state:{group_hash}   →  {first_seen, last_seen, count, ...}
rule:{rule_id}:ack:{group_hash}     →  {acked_by, acked_at, escalate_at}
```

`group_hash` is the canonicalized hash of the group_by field values. All keys carry a TTL (aligned to the `for` duration + silence-window upper bound), ensuring state is automatically reclaimed after a rule is deleted / no longer hits. Why emphasize TTL: alert state is typical "write and forget" data, and a Redis without TTL will sooner or later become the scene of a second incident. In single-instance deployment, Redis state coexists with in-memory dedup (dedup stays as is), and deployments without Redis queue enabled choose the `memory` rule-state implementation (one interface, two backends, consistent with the approach of `core/queue`).

### 4.2 Expression Engine: expr-lang/expr or cel-go, never a hand-written parser

**Why it cannot be hand-written**: a parser can be written, but the safety boundary cannot be drawn. A user-written `match` will be executed millions of times, and a hand-written parser will sooner or later miss a class of injection: property traversal (`env == "prod" || deleteAll()`), resource exhaustion (`repeat("x", 1e9) == "x"`), deep recursion blowing the stack. Off-the-shelf expression engines turn these problems into a type system and an execution budget.

Two candidates:

- **[expr-lang/expr](https://github.com/expr-lang/expr)**: pure Go, zero dependencies, `expr.Compile(code, expr.Env(Env{}))` does **type checking** at compile time (a non-existent `payload.fail_rate` reports a compile error directly, rather than being a silent false at runtime), and evaluation is passed a strongly-typed environment struct. Heavily used by Kubernetes-related toolchains in the Go ecosystem.
- **[cel-go](https://github.com/google/cel-go)** (Google CEL, Common Expression Language): standardized syntax definition + multi-language implementations, with a built-in **cost model** that can estimate the AST's worst-case execution cost at compile time and reject compilation when over the limit; friendly to the protobuf type system.

Selection recommendation: **P1 uses expr-lang/expr**: small API surface, the type environment is just a Go struct (zero glue cost with existing code), and adding an evaluation timeout (`context.WithTimeout` wrapping `vm.Run`) + length caps (expression character count, AST depth) suffices; cel-go's cost model is stronger but introduces a protobuf dependency, so evaluate migrating when rule complexity genuinely needs compile-time cost estimation. **Common red line**: an expression is compiled once when the rule is saved (a compile failure means the save is rejected), and only the compiled artifact is executed at runtime. Why: catch "syntax errors" at config time rather than at the 3 a.m. alert, which is also the premise for shadow mode to give a deterministic preview.

### 4.3 Shadow Mode: All New Rules Dry-Run First

**Why**: rules are programs written by operators, and going fully live on the first launch of a program is treating production as a test environment. The worst outcome of one miswritten rule (wrong threshold, misspelled field name) is waking the whole company in the middle of the night. The SMS channel bills per message, and a false positive to 10000 people is a real budget incident. Shadow mode turns "a rule taking effect" from a one-shot switch into an observable, gradual process:

1. A rule defaults to `mode: shadow` after creation. Each notification evaluation computes the hit result as usual, but **only records, does not deliver**: writes the extended logstore fields (`rule_id`, `would_fire: true/false`, `matched_at`), and the log state is marked `shadow`;
2. The backend rule detail page shows shadow-period statistics: hit counts over the last 24h/7d, hit samples (desensitized notification summaries), and the channel path it would take if activated;
3. The operator, looking at the real hit volume, judges "is this what I want", and after confirming switches `mode: active`. The API layer supports one-click rollback to shadow.

**Why dry-run is not made into anything beyond a switch**: the cost of shadow evaluation is exactly the same as real evaluation (the same compiled artifact, the same evaluator), so there is no "shadow mode slows the system down". If even evaluation is deemed too expensive, the rule itself should be redesigned. The only thing to be careful about is the volume of shadow logs: under high-QPS notifications, full recording would blow out logstore's ring buffer, so shadow logs are sampled by `rule_id` (first entry + a +1 count every N), and the backend preview reads counts rather than details.

## 5. Benchmarking

| Capability | ntfy | Alertmanager | PagerDuty / Opsgenie | herald status quo | herald target |
|---|---|---|---|---|---|
| multi-channel delivery | self-built push (App/Webhook) | webhook forwarding (channels via ecosystem) | native full (phone/SMS/IM), broad SaaS coverage | **14 classes of built-in channels, domestic IM/SMS native** | unchanged, reused |
| routing model | topic subscription | route tree (receiver subtree matching) | static routing rules | two-level type/level static map | expression rule tree + static fallback |
| condition matching | none (dispatch by topic) | built-in matchers (label equality/regex) | simple field conditions | type equality | sandboxed expressions (§4.2) |
| for duration judgment | — | ✅ | ✅ | — | P2 |
| group_by aggregation | — | ✅ | ✅ (event dedup rules) | — | P2 |
| inhibit | — | ✅ (inhibit_rules) | ✅ (related alerts) | — | P2 |
| silence | — | ✅ (create silence via API) | ✅ (maintenance window) | — | P2 |
| ACK + timeout escalation | — | — (only sends, ack is in on-call tools) | ✅ (core capability) | — | P3 |
| configuration method | CLI/config file | **hardcoded config file, changing routes requires a release** | backend UI but the rule model is not programmable | yaml static | **DB CRUD + hot reload + shadow mode** |
| deployment form | self-hosted, free | self-hosted, free | SaaS, billed per person/per event, expensive | self-hosted, free | unchanged |

The three reference points are exactly three trade-offs: ntfy leaves judgment entirely to the client (hence light); Alertmanager has a complete judgment model but its config is frozen in a file (hence operators hate releases); PagerDuty turns the judgment model and response loop into a SaaS (hence complete but expensive). herald's position: **Alertmanager's judgment depth + backend-ized configuration + its own channel breadth + ACK closed loop**, all self-hosted.

## 6. Phased Implementation

### P1 —— Dynamic Rule Foundation (evaluation is a pure function, stateless)

- Rule storage: DB (SQLite to start, swappable to PostgreSQL after interface abstraction), backend CRUD API (mounted under the existing `api/` Handler pattern) + hot reload (watch a version number/update time, do a diff reload referring to the config loading site);
- Expression evaluation: expr-lang/expr, compile + type check on save, execution with timeout and length caps (§4.2);
- Evaluation location: `NotificationService.Process` before enqueueing (§4.1), no rule hit falls back to the existing static route;
- **shadow mode** (§4.3) and logstore extension;
- `core/limiter` formally wired to the delivery side, incidentally fixing retry's retryability judgment (§2);
- **Delivery criteria**: zero change to existing notification behavior (equivalent to the main branch when the rule table is empty); the first real rule goes live via shadow mode.

### P2 —— Stateful Judgment

- Rule state externalized to Redis (the key design of §4.1), memory backend as fallback for single instance;
- `for` duration judgment (first-seen time + duration check, event-driven update rather than timed polling. Why: polling introduces scan-period error and idles, while event-driven judgment checks "whether the duration is reached" at each hit, semantically precise and idle-free);
- `group_by` aggregation notification (group state machine: open → notify → count update → resolved summary);
- `inhibit` (higher-level alert present set + equal field matching);
- `silence` window (time period + timezone + on-call-schedule-integration shift placeholder);
- **Delivery criteria**: the state of the same group of events is continuous before and after worker scaling (Redis state is not affected by the process lifecycle).

### P3 —— ACK Closed Loop

- ACK callback: Feishu card button callback (reusing the app config of `providers/builtin/feishu`, adding a callback HTTP endpoint) + generic `POST /api/alerts/{id}/ack`;
- `escalation` takes effect: ack_timeout expires unacknowledged → escalation-chain delivery (the phone channel is implemented by bridging an external phone gateway via webhook; herald does not build in phone-carrier integration. Why: phone-channel compliance and carrier access is a separate business, and webhook bridging keeps the channel face pluggable);
- Incident records: the full timeline of a group alert from firing to ack to resolve is persisted, and logstore upgrades from "delivery log" to the query view of an "incident ledger".

### Explicitly Not Doing

- No custom expression-function registration (keeping the evaluator closed; why: open function registration equals giving up the sandbox boundary);
- No touching the delivery chain in P1/P2 (queue/worker/provider protocols unchanged);
- No multi-tenancy (the existing single-config-file deployment form is kept, and the rule table carries a `created_by` field to leave a trace for the future).

## 7. Implementation Receipt (2026-09-27, cross-checked item by item against main)

The trunk capabilities of the four-layer evolution are all landed. The item-by-item cross-check results and actual landing points (2026-09-28 review: the first version of the receipt at `ef0a808` wrote the P1 shadow-statistics and P2 silence-window rows as "consistent", which did not match the code, and has been corrected per the table below; the Difference column writes only what can be found in the code):

| Phase | Design item | Actual landing point | Difference from design |
|---|---|---|---|
| P1 | rule storage DB (SQLite to start) | the `Store` interface in `core/rules/store.go` + `MemoryStore`/`FileStore` (JSON persistence, seeded from `cfg.Rules` at startup in `cmd/heraldd/main.go`) | **SQLite not done**, JSON file first; the interface is abstracted, so swapping the backend does not touch the caller |
| P1 | backend CRUD + hot reload | `POST/GET /api/v1/rules`, `/api/v1/rules/{id}` (`api/handler_rules.go`), CRUD writes directly to `rules.Engine` taking effect immediately in-process, file persistence restored on restart | the "watch version number diff reload" is not done: immediate CRUD effect makes watch unnecessary, effect equivalent |
| P1 | expr expression + compile-time type check + timeout/length caps | `expr-lang/expr` (go.mod), `engine.go` compiles on save (`compileRule`), evaluation has a timeout guardrail | expr chosen per design, cel-go not used |
| P1 | evaluation location: before enqueueing + no hit falls back to static route | the `RuleEvaluator` interface in `core/service/notification.go`, evaluated inside `Process` **before routing, before dedup** (actual order: evaluate `:254` → static-route fallback `s.router.Route` `:339` → dedup gate `:359` as the last gate before enqueueing); when `Rules == nil` it stays pure static routing (the `if s.rules != nil` guard at `notification.go:253`) | **order is opposite to §4.1**: the design says "after the dedup check, before route resolution", the implementation moves evaluation before dedup and moves dedup after routing. Stateful rules (for window, group round, recovery judgment) need to **consume every event** to advance state, while dedup only decides "whether this delivery is a duplicate" (source comments `notification.go:242-247` and `:269-277` give the argument). The two goals "before enqueueing" and "no hit falls back to static route" are consistent with the design |
| P1 | shadow mode + logstore extension | `core/rules/shadow.go` (`ShadowSampler` buckets and samples by `(EventKind, rule)` with exact counts) + the seven observer methods of `RuleObserver` (shadow hit/evaluation failure/for pending/folded/inhibited/silenced/dropped each record one event kind); counts are exposed via `runtime.Manager.ShadowRuleCount` as `shadow_hits` in `GET /api/v1/rules` and `/api/v1/rules/{id}`, and the console rule list has a "shadow hits" column | beyond counts, **the 24h/7d window and hit samples were filled in on 2026-10-03**: `ShadowSampler` accumulates window counts by whole-hour buckets (`WindowCount`/`Stats`), each shadow hit additionally enters a 20-capacity sample ring (sampling and ring buffer independent of the delivery log), read out via `shadow_stats` (total/last_24h/last_7d/samples) in `GET /api/v1/rules/{id}`, and the console rule row's "Detail" drawer renders the window counts and recent samples; still in-process state, reset to zero on restart (window and samples both reset). Step 3 of §4.3, "one-click rollback to shadow", has no dedicated endpoint, fulfilled by the console mode switch going through `PUT /api/v1/rules/{id}` to change `mode` |
| P1 | limiter wired to the delivery side | `Deliver` in `core/runtime/manager.go` calls `lm.Wait(ctx)` before any delivery attempt (`:513`, after `m.mu.RUnlock()` fetches `limiters` and before the delivery call), and the `rate_limit` config is wired via `SetProviderLimiter` (`cmd/heraldd/main.go:168`) | **the §2 "orphan module" status quo is closed** |
| P1 | retry retryability judgment fix | `core/httpclient/client.go`: `retryableStatus` (429/5xx) wraps a transport error into a `RetryableError`, and `ShouldRetry` retries accordingly | **the §2 "no provider constructs that type" status quo is closed** (webhook-class providers get it automatically via httpclient) |
| P2 | state externalized to Redis + memory fallback | `core/rules/state.go` (memory)/`state_redis.go` (Redis), injected via `SetStateStore` (`cmd/heraldd/main.go:241`) | consistent |
| P2 | for / group_by / inhibit / silence | `core/rules/for.go`, `group.go`, `inhibit.go`, `silence.go` + the corresponding `engine_*_test.go`; group backend in `core/groups`; on-call schedule in `core/roster` | all four judgment types are present. silence's **"on-call-schedule-integration shift placeholder" was fulfilled on 2026-10-04**: `SilenceSpec` adds a `roster` field (mutually exclusive with `start`/`end`/`tz`, using both is rejected on save), `core/roster` (`Roster`/`Period`/`Manager`, mirroring the Store+FileStore+Manager shape of groups) stores the absolute time-slot table pushed by an external scheduling system via `POST/PUT /api/v1/rosters` (`rosters_store` persisted), and `Engine.SetRosterSource` wires `Manager.Covers` into the silence gate — referencing an unknown/unpushed/deleted on-call schedule, or not wiring the Manager, fails open (missing schedule data never means it should be silenced). Shift rotation and on-call person management stay outside herald (the placeholder semantics of §5 "on-call-schedule-integration shift placeholder"). `tz` (the IANA timezone name in the §3 YAML) was filled in on 2026-10-03: `ParseSilenceWindow` resolves it via `time.LoadLocation`, an unknown name is rejected on save, and `Contains` converts the instant into the window timezone before taking the clock hour; the default remains the event's own timezone (process local). The window fields are two `HH:MM` values `start`/`end`, not the merged form `window: "00:00-06:00"` from the design; a cross-midnight window (22:00-06:00) and an end-open interval are already supported |
| P3 | ACK callback (Feishu card + generic endpoint) | `POST /api/v1/callbacks/feishu` (`api/handler_callbacks.go`) + `POST /api/v1/alerts/{id}/ack` (`api/handler_alerts.go`), ACK state in `core/ack` | consistent |
| P3 | escalation takes effect | `core/escalation` + `engine_escalation_test.go` (ack_timeout expiry escalation chain) | the promise of P1/P2 modeling, P3 fulfillment, is fulfilled |
| P3 | incident ledger | `core/incident` + `GET` incident view (`api/handler_incidents.go`) | consistent |

Cross-check method: endpoints and call sites are evidenced by `grep` (line numbers annotated above), and behavior is corroborated by all `*_test.go` in each package being green; the "status quo" column of `core/limiter` and `core/retry` in the §2 table is kept as a pre-implementation snapshot, not rewritten cell by cell.

Review addendum (same-day second round of line-by-line grep review): two spots in the first version of the receipt that did not match the code are corrected — ① the evaluation order is actually **before** dedup rather than "after dedup" (row 4 of the table above has been changed, an intentional deviation from §4.1, reasoning in the source comments); ② limiter's `lm.Wait` was originally marked `:380`, actually `:381` on HEAD. The remaining 10 rows' landing points are all re-verified as true (including the full P3 chain of the Feishu card confirm button `feishu.go:168` → callback endpoint → `core/ack`, and `escalation.Manager` injected into the notification service via `api/server.go:96`).

> Line-number drift note (calibrated 2026-10-13): the line numbers in the table above and in this addendum are all the landing points at the time of the 2026-09-28 cross-check, and have drifted with code evolution. Re-anchored item by item against the current HEAD — evaluation `notification.go:254`, static-route fallback `:339`, dedup gate `:359`, `Rules == nil` guard `:253`, `lm.Wait` `manager.go:513`, `SetProviderLimiter` `main.go:168`, `SetStateStore` `main.go:241`, Feishu confirm button `feishu.go:169`, escalation injection `api/server.go:185`. The function name/comment text kept next to the line numbers serves as a stable location anchor.

Review addendum (2026-09-28 third round, with code changes): a targeted check for items "the receipt says are done, but nobody calls in production code" turned up `runtime.Manager.ShadowRuleCount`: it previously had **zero production callers** (`grep -rn ShadowRuleCount --include=*.go . | grep -v _test.go` hit only the definition itself), i.e. "shadow mode landed" landed only to log sampling, with no one able to read the statistics. It is now wired to the rule read interface's `shadow_hits` (`ruleView` in `api/handler_rules.go`, both list + detail read faces) and the console list's "shadow hits" column, with behavior covered by `TestHandleRulesShadowHits`. Incidentally a defect that would make that number **lie** was fixed: the sampler originally bucketed by `rule` alone, so for-pending/folded/inhibited/silenced/dropped and shadow hits shared a counter, and "how many times it would have fired during the shadow period" would be inflated by "what this rule has done"; after changing to bucketing by `(EventKind, rule)` (`core/rules/shadow.go`), the event kinds no longer interfere with each other, and the shadow sample stream is no longer eaten by high-traffic dropped events' sampling budget (`TestShadowSamplerKindsAreIndependent`). This round also rewrote the two "consistent" spots (the P1 shadow row, the P2 silence row) per the code facts.

**Conclusion: the four-layer trunk capabilities (static fallback → dynamic rules → stateful judgment → ACK closed loop) are confirmed all landed; the last design detail (silence's on-call-schedule shift placeholder) was filled in on 2026-10-04** (`core/roster` + `silence.roster` + the `/api/v1/rosters` push face, fail-open semantics; the timezone field `tz` was filled in on 2026-10-03). Shadow statistics' 24h/7d window counts and hit samples were also filled in on 2026-10-03 (the `shadow_stats` read face + the console detail drawer, still in-process state). All design promises are settled as of now, and the Difference column has only the intentional deviations on record (evaluation order, JSON storage, window field notation).
