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

## WeChat Runtime Worker 职责

- 登录状态
- Session
- DLL 注入
- Hook
- IPC
- reconnect
- contact lookup
- send message

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
