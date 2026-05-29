# Worker 架构

## 统一 Worker 模型

Herald 中所有 Worker 都是同一种概念，通过 `local`/`remote` 区分部署方式：

```
                         ┌──────────────────────────┐
                         │     Herald Scheduler      │
                         │   (API + 路由 + 模板渲染)  │
                         └─────────┬────────────────┘
                                   │ Push
                            ┌──────▼──────┐
                            │    Queue     │  ← 唯一任务通道 (memory/redis)
                            └──────┬──────┘
                                   │ Pop + Ack/Nack
                    ┌──────────────┼──────────────┐
                    ↓              ↓              ↓
              ┌──────────┐  ┌──────────┐  ┌──────────┐
              │ Worker   │  │ Worker   │  │ Worker   │
              │ mode:local│  │ mode:local│  │ mode:remote│
              │ goroutine │  │ goroutine │  │ 独立进程  │
              └──────────┘  └──────────┘  └──────────┘
                                            ↕ WebSocket (注册/心跳/状态)
```

| 属性 | Local Worker | Remote Worker |
|------|-------------|---------------|
| 部署 | 进程内 goroutine | 独立进程（`heraldd worker`） |
| 任务来源 | 从 Queue Pop | 从共享 Queue Pop（Redis） |
| 通信 | 直接调用 Provider | WebSocket 注册 + Queue 消费 |
| 适用 | 内置 Provider（Telegram、Email 等） | 需要特殊环境的 Provider |

## 启动方式

```bash
# 调度器模式（自动启动 local workers）
heraldd serve --config config.yaml

# 远程 Worker 模式
heraldd worker --config worker.yaml
```

## Worker 注册表

所有 Worker（local 和 remote）都注册到统一的 Registry：

```go
type Info struct {
    ID            string    // worker ID
    Mode          Mode      // "local" 或 "remote"
    Capabilities  []string  // 支持的 Provider 类型
    Status        string    // online / offline
    ConnectedAt   time.Time
    LastHeartbeat time.Time
}
```

- **Local Worker**：启动时自动注册，进程退出时自动注销
- **Remote Worker**：启动时通过 WebSocket 注册，定期心跳保活，超时自动注销

## Queue 接口

Queue 是唯一的任务分发通道，支持可靠消费语义：

```go
type Queue interface {
    Push(ctx context.Context, task *DeliveryTask) error
    Pop(ctx context.Context) (*DeliveryTask, error)
    Ack(ctx context.Context, taskID string) error
    Nack(ctx context.Context, taskID string, reason error) error
    Size() int
    Close() error
}
```

| 实现 | 适用场景 | Remote Worker 支持 |
|------|---------|-------------------|
| `memory` | 单机部署 | 不支持 |
| `redis` | 分布式部署 | 支持 |

## WebSocket 管理通道

WebSocket 不用于任务分发，仅作为远程 Worker 的管理通道：

- **注册**：Remote Worker 启动时上报 ID、能力等信息
- **心跳**：定期保活，超时自动注销
- **状态上报**：Worker 上报自身状态变化（如 session 过期、需要扫码等）

### 注册消息（Worker → Core）

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

### 心跳消息（Worker → Core）

```json
{
  "type": "heartbeat",
  "worker_id": "wechat-worker-01",
  "timestamp": 1716780000,
  "status": {
    "tasks_sent": 100,
    "tasks_done": 98
  }
}
```

## 协议消息

### 注册确认（Core → Worker）

```json
{
  "type": "register_ack",
  "worker_id": "wechat-worker-01",
  "success": true,
  "server_id": "herald-core-01",
  "timestamp": 1716780000
}
```

### 事件消息（Worker → Core）

```json
{
  "type": "event",
  "worker_id": "wechat-worker-01",
  "event_type": "offline",
  "timestamp": 1716780000,
  "data": {
    "reason": "session_expired"
  }
}
```

## 适用场景

Remote Worker 适用于：
- 浏览器自动化（微信公众号、网页版工具）
- 移动端桥接（Android 通知桥接）
- 需要 GUI Session 的场景
- 需要特殊系统权限的场景
- 崩溃隔离需求

## 最佳实践

1. **单机优先**：开发和小规模部署使用 `memory` 队列即可
2. **按需扩展**：需要分布式时切换到 `redis` 队列，代码零改动
3. **崩溃隔离**：Remote Worker 独立运行，崩溃不影响调度器
4. **自动重连**：Remote Worker 断线后自动重连并重新注册
5. **心跳保活**：定期心跳，及时发现连接问题
