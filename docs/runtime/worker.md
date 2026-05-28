# Worker Runtime

## Worker 本质

Worker 是独立的 Runtime 节点，通过 WebSocket 连接到 Herald 核心，用于处理需要特殊运行环境的 Provider。

## 适用场景

- 浏览器自动化（微信公众号、网页版工具）
- 移动端桥接（Android 通知桥接）
- 需要 GUI Session 的场景
- 需要特殊系统权限的场景
- 崩溃隔离需求

## 架构图

```
┌─────────────────────────────────────────────────────────┐
│                     Herald Core                          │
│                                                           │
│  ┌────────────────────────────────────────────────────┐  │
│  │            WebSocket Hub                           │  │
│  │                                                     │  │
│  │  capability → worker_id 映射                       │  │
│  └────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                        ▲
                        │ WebSocket
                        │
┌───────────────────────┼───────────────────────────────────┐
│                       ▼                                   │
│  ┌───────────────────────────────────────────────────┐    │
│  │              Worker Process                        │    │
│  │                                                   │    │
│  │  • 注册 capabilities                              │    │
│  │  • 接收 dispatch 任务                              │    │
│  │  • 发送 ack 确认                                   │    │
│  │  • 定期心跳                                        │    │
│  └───────────────────────────────────────────────────┘    │
└────────────────────────────────────────────────────────────┘
```

## Worker 生命周期

```
连接 → 注册 → 就绪 → 心跳/任务处理 → 断开重连
```

## 协议消息

### 注册消息（Worker → Core）

```json
{
  "type": "register",
  "worker_id": "wechat-worker-01",
  "platform": "linux",
  "version": "1.0.0",
  "capabilities": ["wechatmp", "wechat"]
}
```

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

### 任务分发（Core → Worker）

```json
{
  "type": "dispatch",
  "task_id": "task-123",
  "provider": "wechatmp",
  "title": "服务告警",
  "body": "订单服务异常",
  "level": "error",
  "target": "user_open_id",
  "timestamp": 1716780000
}
```

### 任务确认（Worker → Core）

```json
{
  "type": "ack",
  "task_id": "task-123",
  "success": true,
  "timestamp": 1716780001
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

## Worker 配置

### Provider 配置

```yaml
providers:
  wechatmp:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"
```

### Worker 配置文件

Worker 进程需要配置 WebSocket 连接信息：

```yaml
worker_id: "wechat-worker-01"
core_url: "ws://localhost:8081"
reconnect_delay: 5s
heartbeat_interval: 30s
capabilities:
  - wechatmp
  - wechat
```

## Worker SDK

Herald 提供 Worker SDK 用于快速开发 Worker。

详见 [Worker SDK](/runtime/sdk)。

## Worker 职责

### 必须实现

1. **连接管理**：维持与 Core 的 WebSocket 连接
2. **注册机制**：连接后发送注册消息
3. **心跳机制**：定期发送心跳保持连接
4. **任务处理**：接收 dispatch 消息并处理后发送 ack
5. **重连机制**：断线后自动重连

### 可选实现

1. **事件上报**：主动上报 Worker 状态变化
2. **状态管理**：管理登录状态、会话状态等

## Herald Core 职责

Core 只负责：

1. **Worker 注册**：记录 Worker 的 capabilities
2. **任务分发**：根据 capability 或 worker_id 分发任务
3. **连接管理**：管理 Worker 连接、断开、重连

Core 不关心 Worker 内部实现细节。

## 运行时状态

Worker 需要管理的状态：

| 状态 | 说明 |
|------|------|
| `online` | 在线可用 |
| `offline` | 离线 |
| `relogin` | 需要重新登录 |
| `session_expired` | 会话过期 |
| `qr_required` | 需要扫码登录 |

## 最佳实践

1. **崩溃隔离**：Worker 独立运行，崩溃不影响 Core
2. **自动重连**：断线后自动重连，并重新注册
3. **心跳保活**：定期心跳，及时发现连接问题
4. **任务确认**：每个任务都必须发送 ack
5. **状态上报**：主动上报状态变化，便于监控
