> English · [中文](./library-usage.md)

# Using Herald as a Go Library

Herald has two usage forms, both sharing the same core pipeline (queue, routing, dedup, templates, Providers):

- **CLI gateway** (`cmd/heraldd`): a standalone process with a REST API and Dashboard, suitable for deployment as an organization-wide notification gateway; see [Getting Started](/guide/getting-started).
- **Go library** (this page): `import "github.com/cuihairu/herald"` in your own process and use a single `App` instance for enqueuing and delivery — suitable for embedding notification capabilities directly into your own services.

By default, the library form depends on no external services: an in-memory queue plus a local worker pool, drained gracefully when the process exits.

## Installation

Herald is a single Go module; one dependency gives you the facade and all built-in channels:

```bash
go get github.com/cuihairu/herald
```

## 60-Second Quickstart

```go
package main

import (
    "context"
    "fmt"

    "github.com/cuihairu/herald"
    "github.com/cuihairu/herald/config"
    "github.com/cuihairu/herald/core"
)

func main() {
    cfg := config.Default()
    cfg.Providers["log"] = config.ProviderConfig{Type: "log"}

    app, err := herald.New(cfg)
    if err != nil {
        panic(err)
    }
    defer func() { _ = app.Close() }()

    res, err := app.DispatchSync(context.Background(), &core.Notification{
        Type:     "quickstart",
        Level:    "info",
        Channels: []string{"log"},
        Content:  &core.DirectContent{Title: "Hello from herald"},
    })
    if err != nil {
        panic(err)
    }
    fmt.Println("accepted:", res.Accepted) // [log]
}
```

A runnable minimal project lives in the repository at `examples/quickstart`; an example in the standard library's Example form is provided as `ExampleNew` in the root package.

## Facade API

`github.com/cuihairu/herald` (the root package) is the recommended entry point for library users, exposing one constructor and 11 methods on `App`:

| API | Semantics |
| --- | --- |
| `New(cfg *config.Config) (*App, error)` | Builds the App and starts the delivery pool in the background. When `cfg` is `nil`, everything falls back to defaults; channels declared in `cfg.Providers` are created and registered before return, and any failure aborts construction (resources already created are reclaimed). Invalid `audiences`/`recipients` tables likewise abort construction. |
| `(*App).Dispatch(ctx, n *core.Notification) (*service.ProcessResult, error)` | Asynchronous delivery: channel resolution, template rendering, and dedup complete synchronously within the call and errors are reported here; the actual sending is performed by the background pool. |
| `(*App).DispatchSync(ctx, n) (*service.ProcessResult, error)` | Synchronous delivery: on top of `Dispatch` semantics, blocks until every task produced by the notification has been Acked (success) or Nack'ed (failure), or until `ctx` ends. |
| `(*App).Runtime() *core/runtime.Manager` | Provider manager, used to register custom channels, enable/disable channels, and query delivery logs. |
| `(*App).Queue() core.Queue` | The underlying queue; generally only used for side-channel observation (e.g. backlog depth). |
| `(*App).Groups() *core/groups.Manager` | Notification group manager: the runtime CRUD entry for `group:` references. |
| `(*App).Rules() *core/rules.Engine` | Rule engine: compiles expressions on save; `Put`/`Delete` take effect on the next delivery. |
| `(*App).Acks() *core/ack.MemoryStore` | Alert acknowledgment store (the escalation timers read from the same store). |
| `(*App).AckAlert(ctx, alertID, ackedBy, source)` | Acknowledge an alert: writes to the ack store and records an entry in the incident ledger, aligned with the HTTP ack API. |
| `(*App).Escalation() *core/escalation.Manager` | Manager for unacknowledged escalations. |
| `(*App).Incidents() *core/incident.Store` | Incident ledger: rule-routed deliveries open incidents; acknowledgments and recovery close them. |
| `(*App).Close() error` | Graceful shutdown: stops the delivery pool and waits for it to drain, then closes the queue and the channels. After closing, `Dispatch` returns an error. |

`App` is safe for concurrent `Dispatch` calls; `Close` should be called only once.

## Defaults

`New` fills in defaults for zero-value fields (only zero values are filled; explicit configuration is never overwritten):

| Option | Default | Description |
| --- | --- | --- |
| `Queue.Type` | `memory` | In-memory queue, no external dependencies. |
| `Queue.Size` | `10000` | Queue capacity. |
| `Queue.Workers` | `NumCPU*2+1` | Local delivery concurrency. |
| `Dedup.Window` | `5m` | Dedup window (the dedup switch `Dedup.Enabled` comes from the provided config; it is `true` in `config.Default()`). |
| `Retry.Max` | `3` | Retry limit. |
| `Retry.Backoff` | `exponential` | Backoff strategy. |
| `Retry.InitialDelay` / `MaxDelay` | `1s` / `1m` | Backoff range. |

## Configuration

Two equivalent entry points:

```go
cfg := config.Default()   // 代码内改字段
cfg, err := config.Load("herald.yaml") // 从 YAML 文件读取
```

The fields of `config.Config` are exactly the same as the CLI form's configuration file (`Server`, `Providers`, `Routes`, `Queue`, `Retry`, `Dedup`, `Templates`, etc.), so the same YAML works both for `cmd/heraldd` and for the library form. Delivery-related keys (`Providers`/`Routes`/`level_routes`/`channels`/`audiences`/`recipients`/`groups`/`rules`/`templates`/`dedup`/`retry`, plus the queue, escalation, and incident ledger stores) are wired identically in both forms; in the library form only the HTTP-service-facing fields have no effect: `Server`/`WebSocket`/`Auth`/`Worker`/`card_callback`/`rosters_store` (the on-call roster is pushed in by an external scheduling system, for which the library form has no intake, so the rule engine's `silence.roster` gate always passes). Additionally, `cfg.ExpandEnv()` (expands `$VAR`-style environment variables) and `cfg.Validate()` can be called as needed.

Example channel declaration:

```go
cfg.Providers["feishu-ops"] = config.ProviderConfig{
    Type:   "feishu",
    Config: map[string]interface{}{"webhook_url": "..."},
    // Enabled: nil 等价于启用
}
```

## Routing and Channel Selection

The resolution order by which a `core.Notification` reaches its target channels:

1. If `n.Channels` is non-empty: each listed reference is resolved in turn — a provider instance name (a `Providers` key or a name registered via `Runtime()`) is used directly; `group:<id>` expands to the group members; `user:<id>` expands to that audience/recipient's endpoints; a plain name is first looked up in the `channels` block (expanded into a provider list); if none of these match, it is treated as a literal channel name and reported as `provider not found` before enqueueing.
2. If `n.Channels` is empty: the static route `cfg.Routes[n.Type]` is consulted first; on a miss, `cfg.LevelRoutes[n.Level]`.
3. If neither exists: `Dispatch` returns a `no route found` error. **Registering a channel does not mean traffic will be routed to it.**

## Dedup

When `Dedup.Enabled` is on (it is on in `config.Default()`), repeated notifications with identical content are suppressed within the window: the returned `ProcessResult.TaskIDs` is empty, there is no error, and no delivery is produced. Callers can use this to distinguish "accepted" from "deduped".

## Delivery Semantics and Error Layering

The fields of `ProcessResult` divide the outcome into three layers; understanding this avoids misreported errors:

| Layer | Fields | When |
| --- | --- | --- |
| Accepted | `Accepted`, `TaskIDs` | Channel resolution passed and tasks enqueued (by the time `Dispatch` returns, enqueueing is complete). |
| Channel-level failure | `Failed []ChannelError` | Failures **before enqueueing**: channel does not exist, channel not enabled, etc. |
| Delivery failure | the `error` returned by `DispatchSync` | Send failures **after enqueueing** (the Provider's `Deliver` returned an error); this layer does not go into `Failed`. |

For `Dispatch` (asynchronous), delivery failures happen in the background; errors are only recorded in the delivery logs and can be observed via `app.Runtime().GetLogs(...)`. If you need the delivery outcome, use `DispatchSync`.

## Custom Providers

Implement the four methods of `core.Provider` and it works as soon as it is registered:

```go
type echoProvider struct{}

func (echoProvider) Deliver(_ context.Context, task *core.DeliveryTask) error {
    fmt.Println("delivered:", task.Payload.Content.Title)
    return nil
}
func (echoProvider) Name() string                 { return "echo" }
func (echoProvider) Type() string                 { return "echo" }
func (echoProvider) Status() *core.ProviderStatus { return &core.ProviderStatus{Name: "echo", Type: "echo", Status: "ok"} }

// 构建后注册
_ = app.Runtime().RegisterProvider("echo", echoProvider{}, true)
```

If you also want a custom channel to go through config-driven creation via `cfg.Providers`, implement `core.ProviderFactory` and register it with `Runtime().RegisterFactory(...)`, then declare a `Type` pointing at it in the configuration.

## Lifecycle and Concurrency

- Once `New` returns, the delivery pool is already running in the background and you can `Dispatch` immediately.
- `Close` first stops the pool and waits for it to drain, then closes the queue and the channels, guaranteeing that accepted tasks are never lost and that no zero-value task is ever popped from a queue that is being closed.
- Typical embedding pattern: call `New` once, let it live as long as the service process, and `defer app.Close()` after the `http.Server` shuts down.

## Exported Surface Inventory

The stable surface of the library form consists of the following public packages (the `internal/` directory carries no compatibility promise):

| Package | Role | Key exports |
| --- | --- | --- |
| `github.com/cuihairu/herald` | facade (recommended entry point) | `App`, `New`, `Dispatch`, `DispatchSync`, `Runtime`, `Queue`, `Groups`, `Rules`, `Acks`, `AckAlert`, `Escalation`, `Incidents`, `Close` |
| `config` | Configuration model | `Config`, `Default`, `Load`, `Validate`, `ExpandEnv`; `ProviderConfig`, `QueueConfig`, `RetryConfig`, `DedupConfig`, `ChannelConfig`, etc. |
| `core` | Domain models and interfaces | `Notification`, `DirectContent`, `DeliveryTask`, `DeliveryPayload`, `RenderedContent`, `Provider`, `ProviderFactory`, `ProviderStatus`, `ProviderCapability`, `Queue`, `BuiltinProvider`, `CapableProvider`, `WorkerProvider`, `MaskConfig` |
| `core/service` | Processing pipeline | `NotificationService`, `ProcessResult`, `ChannelError`, `DeliveryPlanner` |
| `core/runtime` | Provider management | `Manager` (`RegisterProvider`, `RegisterFactory`, `Enable`/`Disable`, `GetLogs`, and other delivery log queries) |
| `core/queue` | Queue implementations | `NewQueue`, `NewMemoryQueue`, `NewRedisQueue`, `QueueConfig` |
| `core/route` | Static routing | `Router` (the type table, the `level_routes` table, the `channels` block) |
| `core/dedup` | Content dedup | `Dedup` |
| `core/retry` | Retry policy | `Config` |
| `core/errclass` | Six-way error classification | `Of`, `Class` (temporary/permanent/rate_limited/authentication/invalid_request/timeout) |
| `core/logstore` | Delivery log storage | `TaskLog`, `Filter`, `Stats` |
| `core/template` | Template system | `Manager`, `TemplateConfig` |
| `core/audience` | `user:` audiences | `Manager`, `Audience`, `Recipient`, `Endpoint` |
| `core/groups` | Notification groups | `Group`, `Manager` |
| `core/rules` | Rule engine | `Engine`, `Rule`, `PolicyAllow`/`PolicyDeny` |
| `core/roster` | On-call roster | `Manager` (the read surface for silence schedules) |
| `core/ack` / `core/escalation` / `core/incident` | Alert acknowledgment and incident ledger | `Store`, `Manager`, `Store` |
| `core/worker` | Local delivery pool | `Pool`, `Registry` |
| `providers/builtin/registry` | Built-in channel registration | `RegisterBuiltinProviders` |
| `providers/builtin/*` | Built-in channel implementations | `log`, `webhook`, `email`, `slack`, `telegram`, `discord`, `feishu`, `dingtalk`, `wecom`, `wechat`, `wechatmp`, `aliyunsms`, `tencentsms`, `neteasesms`, `fcm`, `apns`, `jpush`, `getui`, and the `worker` type that forwards to remote Workers |
| `worker-sdk/go` (package `sdk`) | Remote Worker client | SDK for a custom Runtime side to integrate with Herald |

Version compatibility promise: the exported signatures of the facade (root package) and of the public packages in the table above follow Go module semantic versioning; `internal/` and unexported members of any package may change at any time.
