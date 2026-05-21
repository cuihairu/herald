# Worker Runtime

## Worker 本质

Worker 本质是 **Persistent Runtime Node**，而不是 Plugin。

## Worker 生命周期

```
register → heartbeat → dispatch → ack → reconnect
```

## Worker Registration

```json
{
  "worker_id": "wechat-node-01",
  "platform": "windows",
  "capabilities": [
    "wechat"
  ]
}
```

## Dispatch Task

```json
{
  "provider": "wechat",
  "title": "offline",
  "body": "node-17"
}
```

## Worker Runtime 必须支持

### Runtime State

- online
- offline
- relogin
- session_expired
- qr_required

### Event Push

例如：`wechat disconnected` 主动上报 Core。

### Heartbeat

必须。

### Reconnect

必须。

## Worker Runtime 职责

- 连接状态管理
- 心跳与重连
- 任务接收与确认
- 事件上报
- 状态管理

## Herald Core 职责

只负责：
> dispatch task

例如：

```json
{
  "provider": "wechat",
  "target": "group_xxx",
  "message": "offline"
}
```
