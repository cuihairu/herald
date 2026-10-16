> English · [中文](./sdk.md)

# Worker SDK

The Worker SDK is used to develop remote Workers. A remote Worker consumes tasks from the shared Queue and registers with the scheduler over WebSocket.

## Core Concepts

```
Remote Worker startup flow:
1. WebSocket registration (reports ID, capabilities) → scheduler
2. Queue consumption (Pop → Deliver → Ack/Nack) ← shared Queue
```

## Go SDK Usage Examples

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/cuihairu/herald/core"
    "github.com/cuihairu/herald/protocol"
    workersdk "github.com/cuihairu/herald/worker-sdk/go"
)

func main() {
    config := &protocol.WorkerConfig{
        WorkerID:          "my-worker-001",
        CoreURL:           "ws://localhost:8081/worker",
        HeartbeatInterval: 30 * time.Second,
        Capabilities:      []string{"wechat"},
    }

    // Queue is set to nil; pass a redis queue in production
    client := workersdk.NewClient(config, nil)

    // Set the task handler
    client.OnTask(func(task *core.DeliveryTask) error {
        fmt.Printf("Task: %s, Provider: %s\n", task.ID, task.Provider)
        if task.Payload.Content != nil {
            fmt.Printf("Title: %s\n", task.Payload.Content.Title)
        }
        return nil
    })

    client.OnConnect(func() {
        fmt.Println("Connected to Herald core")
    })

    // Start (registration + consumption loop)
    ctx := context.Background()
    if err := client.Run(ctx); err != nil {
        panic(err)
    }

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    _ = client.Disconnect()
}
```

Note: the SDK does not reconnect automatically — once the connection drops, the client stays in `Disconnected`, and recovery means calling `Run` again to re-register. `ReconnectDelay` is only honored by the builtin binary (`heraldd worker`).

### Using the Builtin Binary

You can also use the `heraldd worker` subcommand directly, without writing any code:

```bash
heraldd worker --config worker.yaml
```

## SDK Selection Guide

| Language | Purpose | Status |
|------|------|------|
| Go | Worker development | Done |
| C++ | Native Addon | Planned |
| Python | Automation | Planned |
| Rust | Native Worker | Planned |

## Connection States

```
Disconnected → Connecting → Ready
     ↑                         ↓
     └────── (error/close) ────┘
```

`Connected` / `Registered` are intermediate states in the protocol vocabulary that the SDK never passes through — once the register-ack lands, it goes straight from `Connecting` to `Ready`; a dropped connection returns to `Disconnected` and does not reconnect automatically (see above).

## Protocol Messages

### Registration Message

```json
{
  "type": "register",
  "worker_id": "wechat-node-01",
  "mode": "remote",
  "platform": "windows",
  "version": "1.0.0",
  "capabilities": ["wechat"]
}
```

### Heartbeat Message

```json
{
  "type": "heartbeat",
  "worker_id": "wechat-node-01",
  "timestamp": 1716000000
}
```

`status` is an optional free-form map in the protocol; neither the current SDK nor the builtin worker populates it, so the examples send only `worker_id` + `timestamp`.

### Event Message

```json
{
  "type": "event",
  "worker_id": "wechat-node-01",
  "event_type": "offline",
  "timestamp": 1716000002
}
```
