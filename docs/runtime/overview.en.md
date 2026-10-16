> English · [中文](./overview.md)

# Runtime Overview

Which process a Provider runs in is decided by the Runtime. Herald has two categories: Builtin (in-process direct calls) and Worker (a separate process connected over WebSocket).

## Runtime Types

### Builtin Runtime

Runs directly inside the Herald core process; delivery is a single function call, with no IPC.

#### Suitable Scenarios

Telegram, Discord, Feishu, WeCom bot, Email, Webhook, SMS (Aliyun, Tencent Cloud, NetEase Yunxin) — anything that is a pure HTTP API channel counts.

#### Key Features

Tasks never leave the process: latency is just the Provider's own HTTP round trip, registering takes a single `providers:` block, and there is no separate process to manage.

#### Interface

```go
type Provider interface {
    Deliver(ctx context.Context, task *DeliveryTask) error
    Name() string
    Type() string
    Status() *ProviderStatus
}
```

#### Registration Flow

```go
manager.RegisterFactory(&telegram.Factory{})
provider, _ := manager.CreateProvider("telegram", config)
manager.RegisterProvider("telegram", provider, true)
```

### Worker Runtime

A standalone Runtime node that connects to the Herald core over WebSocket.

#### Suitable Scenarios

- Browser automation (WeChat Official Account, web-based tools)
- Mobile bridging (Android notification bridge)
- Scenarios that need a GUI session
- Scenarios that need special system permissions

#### Architecture Diagram

```
┌──────────────────────────────────────────────────────────┐
│                       Herald Core                        │
│                                                          │
│  ┌────────────────────────────────────────────────────┐  │
│  │                  Runtime Manager                   │  │
│  │                                                    │  │
│  │  ┌──────────────────┐    ┌──────────────────┐      │  │
│  │  │ Builtin          │    │ Worker Proxy     │      │  │
│  │  │ Providers        │    │ Providers        │      │  │
│  │  │                  │    │                  │      │  │
│  │  │ • Telegram       │    │ • type: worker   │      │  │
│  │  │ • Email          │    │   forwarding     │      │  │
│  │  │ • SMS            │    │   channel        │      │  │
│  │  └──────────────────┘    └─────────┬────────┘      │  │
│  └────────────────────────────────────┼───────────────┘  │
│                                       │                  │
└───────────────────────────────────────┼──────────────────┘
                                        │
                                    WebSocket
                                        │
┌───────────────────────────────────────┼──────────────────┐
│                             Worker Hub│                  │
│                                       ▼                  │
│  ┌────────────────────────────────────────────────────┐  │
│  │               Remote Worker Process                │  │
│  │                                                    │  │
│  │  • Runs in a separate process                      │  │
│  │  • Connects via WebSocket                          │  │
│  │  • Registers its own capabilities                  │  │
│  │  • Receives and processes tasks                    │  │
│  └────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

#### Key Features

Channel code runs in a separate process: a crash does not take down the scheduler, and it can use environments the main process lacks, such as a GUI session or special OS permissions. The cost is one more WebSocket link that must maintain heartbeat and reconnection.

#### Worker Protocol

The Worker communicates with the core over WebSocket and supports the following message types:

| Message Type | Direction | Description |
|---------|------|------|
| `register` | Worker → Core | Worker registration |
| `register_ack` | Core → Worker | Registration acknowledgment |
| `heartbeat` | Worker → Core | Heartbeat |
| `dispatch` | Core → Worker | Defined but not enabled (no production call sites in the repo; task dispatch goes through the queue and the Hub does not participate) |
| `ack` | Worker → Core | Task acknowledgment |
| `error` | Bidirectional | Error reporting |
| `event` | Worker → Core | Event notification (online/offline/error) |

#### Worker Configuration

```yaml
providers:
  ops-mp:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"
```

#### Worker Registration

On startup, the Worker sends a registration message:

```json
{
  "type": "register",
  "worker_id": "wechat-worker-01",
  "mode": "remote",
  "platform": "linux",
  "version": "1.0.0",
  "capabilities": ["wechatmp", "wechat"]
}
```

The core replies with a registration acknowledgment:

```json
{
  "type": "register_ack",
  "worker_id": "wechat-worker-01",
  "success": true,
  "server_id": "herald-core-01",
  "timestamp": 1716780000
}
```

## Runtime Manager

The Runtime Manager is the management center for all Providers.

### Core Functions

Both Provider factories and instances are registered on the Manager; at delivery time it looks up the instance by name and calls `Deliver`, hands failures over to the retry mechanism, and writes a delivery log entry for every attempt.

### Usage Example

```go
// Create the Manager
manager := runtime.NewManager(1000, &retry.Config{
    Max:         3,
    Backoff:     "exponential",
    InitialDelay: time.Second,
    MaxDelay:    time.Minute,
})

// Register factories
manager.RegisterFactory(&telegram.Factory{})
manager.RegisterFactory(&email.Factory{})

// Create and register a Provider
provider, _ := manager.CreateProvider("telegram", config)
manager.RegisterProvider("telegram", provider, true)

// Deliver a task
err := manager.Deliver(ctx, task)
```

## WebSocket Hub

The Hub only handles Worker registration and lifecycle, not task dispatch — tasks go through the queue (the comment at the top of `core/websocket/hub.go` states this boundary explicitly).

### Core Functions

1. **Registration**: `OnRegister` records the remote Worker (capabilities, Remote mode) into worker.Registry
2. **Heartbeat**: `OnHeartbeat` refreshes the liveness timestamp; expired entries are cleaned up periodically by the Registry
3. **Connection management**: `OnDisconnect` unregisters; `OnTaskAck` / `OnWorkerEvent` are currently empty implementations (task results go through queue Ack/Nack)

## Dispatcher and Worker Pool

The Dispatcher is a thin wrapper over the Worker Pool (in the words of the comment in `core/dispatch/dispatcher.go`; new code should use `worker.Pool` directly).

### Dispatch Logic

All tasks are popped from the queue and then handed uniformly to `runtime.Manager.Deliver`:

```
Queue → worker.Pool (N local goroutines)
                      │
                      └─ runtime.Manager.Deliver(task)
                             │
                             ├─ Builtin Provider → in-process Deliver()
                             └─ type: worker proxy → sent over WebSocket to the target Worker
```

The local-vs-remote fork is decided by the Provider instance's `type`: a `type: worker` proxy channel forwards the task over WebSocket to the remote Worker named by `target`; all other types are delivered directly in-process. The Pool Nacks the queue on delivery failure and Acks on success.
