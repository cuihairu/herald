# Worker SDK

Worker SDK 用于开发远程 Worker。远程 Worker 从共享 Queue 消费任务，通过 WebSocket 注册到调度器。

## 核心概念

```
远程 Worker 启动流程：
1. WebSocket 注册（上报 ID、能力） → 调度器
2. Queue 消费（Pop → Deliver → Ack/Nack） ← 共享 Queue
```

## Go SDK 使用示例

### 基本使用

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
        CoreURL:           "ws://localhost:8081",
        ReconnectDelay:    5 * time.Second,
        HeartbeatInterval: 30 * time.Second,
        Capabilities:      []string{"wechat"},
    }

    // Queue 设置为 nil；生产环境传入 redis queue
    client := workersdk.NewClient(config, nil)

    // 设置任务处理器
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

    // 启动（注册 + 消费循环）
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

### 使用内置二进制

也可以直接使用 `heraldd worker` 子命令，无需编写代码：

```bash
heraldd worker --config worker.yaml
```

## SDK 选择指南

| 语言 | 用途 | 状态 |
|------|------|------|
| Go | Worker 开发 | 完成 |
| C++ | Native Addon | 计划中 |
| Python | Automation | 计划中 |
| Rust | Native Worker | 计划中 |

## 连接状态

```
Disconnected → Connecting → Connected → Registered → Ready
     ↓              ↓              ↓            ↓
   (error)       (error)        (error)     (error)
```

## 协议消息

### 注册消息

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

### 心跳消息

```json
{
  "type": "heartbeat",
  "worker_id": "wechat-node-01",
  "timestamp": 1716000000,
  "status": {
    "tasks_done": 98
  }
}
```

### 事件消息

```json
{
  "type": "event",
  "worker_id": "wechat-node-01",
  "event_type": "offline",
  "timestamp": 1716000002
}
```
