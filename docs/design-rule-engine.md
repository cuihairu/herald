# Rule Engine Design: Condition and Action Model (allow / inhibit / reroute)

> Status: implemented. This document is the finalized design of the rule engine's "decision layer": how a rule is expressed, what fields it matches, in what order it is evaluated, what it does to a message once hit (allow/inhibit/reroute), and the default policy when no rule hits.
> For the alert platform's long-term evolution (the ins and outs of for/group_by/inhibit/silence/escalation), see
> [rule-engine-design.md](./rule-engine-design.md); this document focuses only on "a message comes in, how does the rule judge and act",
> and is in a layered relationship with that document: that one manages "the historical dimension of judgment", this one manages "the split-second decision of judgment".

## 1. The Problem: Decision Authority Has Not Returned Home

Without a rule engine, everything in herald is "what to do when an event arrives" (`api/handler_notify.go` →
`Process` in `core/service/notification.go`):

- Whether to send: decided by dedup (content duplicates are not sent), everything else is sent;
- Where to send: the caller's explicit `channels` take priority, otherwise the static route table (`routes`/`level_routes` in `core/route/router.go`, type equal → level equal).

Both decision dimensions are un-operable:

1. **"Whether to send" has no rule.** "No sound for info outside working hours", "alerts in the canary environment are always held back".
   Today such policies can only be written into each caller, or the code is changed;
2. **"Where to send" is a config file.** Changing one static route requires restarting the process, and it can only match by type/level equality,
   so it cannot express "title containing 'drill' goes to the drill group".

The rule engine pulls these two decision authorities into the herald backend: **condition (match expression) + action (allow/inhibit/reroute)**,
rules are stored in a store, CRUD from the backend, hot-effective, and on no hit fall back to static routing, with zero change to existing behavior.

## 2. How a Rule Is Expressed

The storage model of a rule (the `Rule` in `core/rules/rule.go`):

```yaml
id: prod-payment-failure
priority: 100
action: route            # allow | suppress | route (default)
mode: active             # shadow | active | off (default shadow)
match: 'params.fail_rate > 0.05 && env == "prod"'
route:                   # required when action == route
  - match: 'level == "critical"'
    channels: [wecom-boss, aliyunsms-duty]
  - channels: [feishu-oncall]
for: 3m                  # the following are advanced semantics, see rule-engine-design.md
group_by: [env, service]
# inhibit / silence / escalation are analogous
```

### 2.1 Condition Model: Matchable Fields

The match input is the notification's **channel-agnostic structured view** (`rules.Env`):

| Field | Source | Notes |
|---|---|---|
| `type` | `Notification.Type` | business type (deploy, alert…) |
| `level` | `Notification.Level` | info / warning / error / critical |
| `title` | inline `Content.Title` | direct-send content only; text after template rendering does **not** enter matching |
| `body` | inline `Content.Body` | same as above |
| `params` | `Notification.Params` | caller-defined structured fields, the workhorse of rule matching |

**Why the match is against Env and not the rendered text**: regex-matching text is fragile — one template change and all rules go mute; and template rendering happens after rule evaluation (rerouting decides the channel, and only the channel affects rendering), so at that timing it is unavailable anyway.
`params.fail_rate > 0.05` lands the judgment on the structure of the data, and no template revision affects the rule.

**Why the fields are a closed set**: Env is a Go struct, and `expr.Compile` with `expr.Env(Env{})` does type checking **at save time**, so `params.fail_ate` (misspelled) fails to compile and is rejected outright, rather than being a silent false at runtime.
An open field set (say, spreading out the entire notification JSON) buys no more expressiveness, only more chances for a misspelling to go unnoticed.

### 2.2 Condition Model: Expression Semantics and Safety

The expression engine uses [expr-lang/expr](https://github.com/expr-lang/expr), and **never hand-writes a parser**.
The crux of a hand-written parser is that the safety boundary cannot be drawn: injection, resource exhaustion, deep recursion.
Existing constraints (`core/rules/engine.go`):

- compiled once at save time, and only the compiled artifact is executed at runtime. Syntax errors are caught at config time, not at the 3 a.m. alert;
- `expr.Env(Env{})` type checking + `AsBool()` forcing a boolean result;
- `MaxNodes(256)` caps AST complexity, `MaxExpressionLen(2048)` caps source-text length;
- `DisableAllBuiltins()` turns off all built-in functions. Expressions are pure comparison/logic and cannot call anything;
- the AST check rejects the `..` range operator (`1..1e9` can allocate huge memory within the node limit);
- evaluation has a 500ms timeout fallback (`runProgram`).

**Alternative: cel-go**. Its cost model (estimating worst-case execution cost at compile time) is stronger, but it introduces a protobuf dependency and an API surface an order of magnitude larger; herald's expressions are short comparisons, and expr + static limits already seal off the resource surface. Rejected.
**Alternative: a hand-written DSL (structured JSON of field-operator-value)**. It cannot express any combination beyond `a && (b || c)`, so either it grows into a crippled query language or it limits expressiveness; and it still has to handle types and injection itself. Rejected.

### 2.3 Action Model: allow / inhibit / reroute (the newly added `action` field)

The action answers "what to do to this message once hit". Three values, for three intents:

| action | Semantics | Message destination | route field |
|---|---|---|---|
| `route` (default) | **reroute**: the rule decides the channel | the channel resolved by the rule's `route` steps | required (≥1 step) |
| `suppress` | **inhibit**: hold the message back | not delivered, an observation record is kept | must be empty |
| `allow` | **allow**: process as if there were no rule | the caller's explicit channel, otherwise static routing | must be empty |

Three design trade-offs:

**Why action defaults to `route`**: backward compatibility. Every existing rule before the action field landed is "hit means reroute", an empty action is read as route, and not a single line of existing store files needs changing. `route` also requires non-empty route steps, while `allow`/`suppress` require route to be empty. Action and route table are mutually exclusive intents; mixing them is either a typo or a misunderstanding, rejected outright at save time, leaving no opening for "silently ignore".

**Why an explicit `suppress` is needed**: previously inhibition had only four dedicated channels (silence/for/folded/inhibit), all bound to alert semantics; whereas an **unconditional filter** like "any alert in the canary environment must not be sent" required constructing a zero-semantics rule to express. `action: suppress` is that filter: one line of rule, occupying no state machine.
Its difference from silence: suppress is **identity-driven** (this traffic just should not be sent), silence is **schedule-driven** (this time slot should not send); a suppress rule does not change with time, a silence window lifts automatically when its time arrives.

**Why an explicit `allow` is needed**: allow is the other half of the priority model. Rules are evaluated by priority with first-hit-stop (§3.1); without allow, "exemption" cannot be expressed: `priority: 1000, match: env == "canary", action: allow` means "canary traffic stops here, all later reroute/inhibit rules leave it alone, it goes the original way".
It is equivalent to ACCEPT-and-stop in firewall rules, a standard primitive of a policy table.

**The boundary of inhibition: explicit channels are not inhibited**. Consistent with existing semantics (silence/for/inhibit all are): a notification whose `channels` the caller explicitly specifies is an unambiguous delivery intent, and the rule's inhibit action only acts on traffic that "would otherwise rely on rules/static routing to decide the destination". This boundary is written in the same code as the default policy of §3.4, and does not set up a separate exception for each of the two inhibit paths.

## 3. Match Semantics

### 3.1 Evaluation Order: Explicit Priority (the newly added `priority` field)

The rule table is evaluated in **descending priority**, ties by **write order** (stable sort).
`priority` is an int32, default 0, larger evaluated first; the first hit **active** rule is the "governing rule" (governing rule), evaluation stops, first-hit-stop.

**Alternative: pure list order (the status quo)**. List order is priority, and the problem is that reordering equals a full rewrite: to bring one rule to the front, you must PUT the entire table. And the order is an implicit contract — API users cannot see "why this one precedes that one".
**Alternative: iptables-style insertion position (before/after anchors)**. The strongest expressiveness, but CRUD complexity explodes (anchor invalidation, cycle detection), and herald's rule scale (a few dozen) does not need it.
**Chosen: numeric priority + stable tie-break**. Adjusting priority = PUT one number; ties behave deterministically (write order); first-hit-stop makes "high-priority exemption/block" naturally composable. We will talk about anchor insertion when the rule scale demands it.

In implementation, sorting happens at the moment the engine loads the table (`Put`/`Reload`), transparent to `Evaluate`.
The evaluation loop merely traverses an already-sorted slice, zero extra overhead on the hot path. `List` returns the **evaluation order** (active-table order), what you see is what is judged.

### 3.2 Three Modes: active / shadow / off

- `active`: the rule takes effect, actions execute per §2.3;
- `shadow` (default): record only, no action; each evaluation computes the hit as usual, and a hit is written to the observation record
  (`Decision.Shadow` → `RuleObserver.RecordShadow` → delivery log), delivery behavior unchanged;
- `off`: the rule is stored but skipped in evaluation, equivalent to "disabled".

A new rule defaulting to shadow is a safety net: rules are programs written by operators, and going fully live on first launch is treating production as a test; during the shadow period you watch the real hit volume, then switch to active. At the API layer, PUT changing mode is start/stop, one-click rollback.

**Alternative: a separate enabled boolean field**. It duplicates the semantics of `mode: off`, two fields expressing one switch, and it would also produce combinations like `enabled: true, mode: off` that need arbitration. Rejected. The three-valued mode is the complete state machine of "live/observe/disabled".

### 3.3 Evaluation Failure: a Bad Rule Does Not Drag Down Routing

If one rule's expression evaluation fails (say it references a params key this notification does not have): skip that rule and continue evaluating, record the failure in `Decision.EvalErrors` and return it as an error to the caller for observation. A bad rule at worst affects only itself, and is not allowed to paralyze the whole table. A state-backend (for/group/inhibit state) failure likewise fails open and is skipped.

### 3.4 Default Policy: What to Do When No Rule Hits (newly added)

`rules_default_policy` configures the engine-level default policy, two values:

- `allow` (default): no rule hit → fall back to **static routing** (`core/route`), behavior byte-for-byte identical to having no rule engine;
- `deny`: no hit → **the message is held back** (`Decision{Action: suppress, Defaulted: true}`), the observation record's RuleID is an empty string.

allow is the safety net of incremental migration: when the rule table is empty, system behavior is fully identical to trunk.
deny is "whitelist mode": herald flips from "send everything by default, rules block" to "send nothing by default, rules allow". The final step of alert convergence is letting out only traffic that has been explicitly configured. The two policies switch on the same line of code, and under deny a shadow hit is still recorded (observation does not go blind because of tightening).

**Alternative: a per-rule default field**. "Default policy" describes the no-rule-hit case, a table-level property rather than a rule property, and placing it on a rule is semantically misaligned (whose default counts? the one that runs first?). Rejected.
**Alternative: returning an error to the caller under deny**. Exposing the policy as a fault to the caller is inappropriate: holding back is a normal decision, not an error, HTTP is still 200, and the destination goes into the observation record. Rejected.

### 3.5 Unified Expression of the Decision

`Evaluate` returns a `Decision`, which from this round carries an `Action` field, stating "what the governing rule wants to do" in one go:

```go
Decision{
    RuleID, Mode,           // governing rule (RuleID is empty when the default policy takes effect)
    Action,                 // allow | suppress | route —— newly added this round
    Channels,               // target channels when action == route
    Defaulted,              // true = default policy took effect (no rule hit) —— newly added this round
    Silenced/ForPending/Folded/Inhibited,  // existing dedicated inhibit semantics (rule-engine-design.md)
    Shadow, EvalErrors, ... // observation record
}
```

The caller (`service.Process`) only recognizes Action and the dedicated inhibit flags, and does not re-implement the policy.

## 4. Management Plane: CRUD and Start/Stop

REST API (mounted under the existing Handler pattern, `api/handler_rules.go`):

| Method | Path | Semantics |
|---|---|---|
| GET | `/api/v1/rules` | list (evaluation order) |
| POST | `/api/v1/rules` | create (expression compile failure → 400, rejected on save) |
| GET | `/api/v1/rules/{id}` | single rule |
| PUT | `/api/v1/rules/{id}` | full replace (changing mode=start/stop, changing priority=reorder) |
| DELETE | `/api/v1/rules/{id}` | delete |

All write operations go through `Engine.Put/Delete`: validate + compile + write to store + **refresh the active table in place**, hot-effective without restart.
The persistence backend is swappable (the `Store` interface: `MemoryStore` / JSON `FileStore`, atomic write: temp file + rename).
The config file `rules:` seed list is loaded at startup (coexisting with the store, seeds are always Put into the store).

## 5. Implementation Boundaries

- Evaluation location: inside `Process`, before routing, before template rendering (see the comment in `core/service/notification.go`);
  a held-back message does not enter the queue and does not consume a worker.
- Zero change to the delivery chain: the queue/worker/provider protocols are untouched, and the rule engine lives entirely before enqueueing and in the management plane.
- For notification groups (rules/callers referencing a `group:xxx` channel group), see
  [design-notification-groups.md](./design-notification-groups.md), orthogonal to this design.
