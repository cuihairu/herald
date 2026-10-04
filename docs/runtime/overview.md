# Runtime 概述

## Provider Runtime Architecture

这是 Herald 的核心。

## Runtime 类型

### Builtin Runtime

直接运行在 Herald 核心进程内。

#### 适合场景

- Telegram
- Discord
- 飞书
- 企业微信 Bot
- Email
- Webhook
- SMS（阿里云、腾讯云、网易云）

#### 特点

- 轻量
- 无 IPC
- 高性能
- 配置简单

#### 接口

```go
type Provider interface {
    Deliver(ctx context.Context, task *DeliveryTask) error
    Name() string
    Type() string
    Status() *ProviderStatus
}
```

#### 注册流程

```go
manager.RegisterFactory(&telegram.Factory{})
provider, _ := manager.CreateProvider("telegram", config)
manager.RegisterProvider("telegram", provider, true)
```

### Worker Runtime

独立的 Runtime 节点，通过 WebSocket 连接到 Herald 核心。

#### 适合场景

- 浏览器自动化（微信公众号、网页版工具）
- 移动端桥接（Android 通知桥接）
- 需要 GUI Session 的场景
- 需要特殊系统权限的场景

#### 架构图

```
┌─────────────────────────────────────────────────────────┐
│                     Herald Core                          │
│                                                           │
│  ┌────────────────────────────────────────────────────┐  │
│  │            Runtime Manager                         │  │
│  │                                                     │  │
│  │  ┌──────────────────┐      ┌──────────────────┐    │  │
│  │  │ Builtin         │      │ Worker 代理       │    │  │
│  │  │ Providers       │      │ Providers        │    │  │
│  │  │                 │      │                  │    │  │
│  │  │ • Telegram      │      │ • type: worker   │    │  │
│  │  │ • Email         │      │   的转发渠道      │    │  │
│  │  │ • SMS           │      │                  │    │  │
│  │  └─────────────────┘      └────────┬─────────┘    │  │
│  └─────────────────────────────────────────┼────────────┘  │
│                                            │              │
└────────────────────────────────────────────┼──────────────┘
                                             │
                                    WebSocket
                                             │
┌────────────────────────────────────────────┼──────────────┐
│                                    Worker Hub │              │
│                                              ▼              │
│  ┌───────────────────────────────────────────────────┐    │
│  │              Remote Worker Process                 │    │
│  │                                                   │    │
│  │  • 运行在独立进程中                               │    │
│  │  • 通过 WebSocket 连接                           │    │
│  │  • 注册自己的 capabilities                        │    │
│  │  • 接收并处理任务                                 │    │
│  └───────────────────────────────────────────────────┘    │
└────────────────────────────────────────────────────────────┘
```

#### 特点

- 崩溃隔离
- 独立权限
- 特殊 OS 环境
- GUI Session 支持
- 独立进程隔离

#### Worker 协议

Worker 通过 WebSocket 与核心通信，支持以下消息类型：

| 消息类型 | 方向 | 说明 |
|---------|------|------|
| `register` | Worker → Core | Worker 注册 |
| `register_ack` | Core → Worker | 注册确认 |
| `heartbeat` | Worker → Core | 心跳 |
| `dispatch` | Core → Worker | 任务分发 |
| `ack` | Worker → Core | 任务确认 |
| `error` | 双向 | 错误通报 |
| `event` | Worker → Core | 事件通知（online/offline/error） |

#### Worker 配置

```yaml
providers:
  ops-mp:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"
```

#### Worker 注册

Worker 启动时发送注册消息：

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

核心返回注册确认：

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

Runtime Manager 是所有 Provider 的管理中心。

### 核心功能

1. **Provider 注册**：注册 Provider Factory 和实例
2. **Provider 创建**：从配置创建 Provider 实例
3. **任务投递**：将任务分发给指定的 Provider
4. **重试机制**：处理失败任务的重试
5. **日志记录**：记录投递日志

### 使用示例

```go
// 创建 Manager
manager := runtime.NewManager(1000, &retry.Config{
    Max:         3,
    Backoff:     "exponential",
    InitialDelay: time.Second,
    MaxDelay:    time.Minute,
})

// 注册 Factory
manager.RegisterFactory(&telegram.Factory{})
manager.RegisterFactory(&email.Factory{})

// 创建并注册 Provider
provider, _ := manager.CreateProvider("telegram", config)
manager.RegisterProvider("telegram", provider, true)

// 投递任务
err := manager.Deliver(ctx, task)
```

## WebSocket Hub

Hub 只管 Worker 的注册与生命周期，不管任务分发——任务走队列（`core/websocket/hub.go` 顶部注释明确写了这条边界）。

### 核心功能

1. **注册**：`OnRegister` 把远程 Worker（capabilities、Remote 模式）记入 worker.Registry
2. **心跳**：`OnHeartbeat` 刷新存活时间，过期条目由 Registry 定期清理
3. **连接管理**：`OnDisconnect` 注销；`OnTaskAck` / `OnWorkerEvent` 目前为空实现（任务结果走队列 Ack/Nack）

## Dispatcher 与 Worker Pool

Dispatcher 是 Worker Pool 的薄封装（`core/dispatch/dispatcher.go` 注释原话，新代码直接用 `worker.Pool`）。

### 分发逻辑

所有任务都从队列 Pop，然后统一交给 `runtime.Manager.Deliver`：

```
Queue → worker.Pool (N 个本地 goroutine)
                      │
                      └─ runtime.Manager.Deliver(task)
                             │
                             ├─ Builtin Provider → 进程内 Deliver()
                             └─ type: worker 代理  → WebSocket 发给目标 Worker
```

本地与远程的分流点在 Provider 实例的 `type` 上：`type: worker` 的代理渠道把任务经 WebSocket 转发给 `target` 指定的远程 Worker；其余类型全部进程内直发。Pool 在投递失败时对队列 Nack，成功时 Ack。
