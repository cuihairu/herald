# REST API

## POST /api/v1/notify

发送通知。

### 请求

```http
POST /api/v1/notify
Content-Type: application/json
```

```json
{
  "title": "Node Offline",
  "body": "node-17 is offline",
  "level": "error",
  "channels": ["telegram"]
}
```

### 参数

| 字段       | 类型     | 必填   | 描述    |
| -------- | ------ | ---- | ----- |
| title    | string | 是    | 标题    |
| body     | string | 是    | 内容    |
| level    | string | 否    | 级别 (debug/info/warning/error/critical) |
| channels | array  | 否    | 指定渠道，不指定则根据 level 路由 |

### 响应

```json
{
  "code": 0,
  "message": "ok"
}
```

## POST /api/v1/events

发送事件。

### 请求

```http
POST /api/v1/events
Content-Type: application/json
```

```json
{
  "type": "node.offline",
  "labels": {
    "node": "node-17",
    "level": "error"
  },
  "data": {
    "reason": "connection timeout"
  }
}
```

### 参数

| 字段       | 类型     | 必填   | 描述    |
| -------- | ------ | ---- | ----- |
| type    | string | 是    | 事件类型 |
| labels  | map    | 否    | 标签    |
| data    | map    | 否    | 附加数据 |

### 响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "event_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

## GET /api/v1/status

查询服务状态。

### 响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "running",
    "providers": [
      {
        "name": "telegram",
        "type": "builtin",
        "status": "available",
        "enabled": true
      }
    ]
  }
}
```

## GET /api/v1/providers

查询 Provider 列表。

### 响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "providers": [
      {
        "name": "telegram",
        "type": "builtin",
        "status": "available",
        "enabled": true,
        "since": "2026-05-18T08:00:00Z"
      },
      {
        "name": "aliyunsms",
        "type": "builtin",
        "status": "available",
        "enabled": false,
        "since": "2026-05-18T08:00:00Z"
      }
    ]
  }
}
```

## POST /api/v1/providers/{name}/enable

启用指定的 Provider。

### 请求

```http
POST /api/v1/providers/telegram/enable
```

### 响应

```json
{
  "code": 0,
  "message": "provider enabled"
}
```

## POST /api/v1/providers/{name}/disable

禁用指定的 Provider。

### 请求

```http
POST /api/v1/providers/telegram/disable
```

### 响应

```json
{
  "code": 0,
  "message": "provider disabled"
}
```

## GET /api/v1/workers

查询已连接的 Workers。

### 响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "count": 1,
    "workers": [
      {
        "worker_id": "wechat-worker-01",
        "platform": "linux",
        "version": "1.0.0",
        "capabilities": ["wechat", "sms"],
        "connected_at": "2026-05-18T08:00:00Z",
        "last_heartbeat": "2026-05-18T08:05:00Z",
        "status": {
          "tasks_sent": 100,
          "tasks_done": 98
        }
      }
    ]
  }
}
```

## GET /api/v1/queue

查询队列状态。

### 响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "size": 5
  }
}
```
