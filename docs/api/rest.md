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
| level    | string | 否    | 级别    |
| channels | array  | 否    | 指定渠道 |

### 响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "task_id": "550e8400-e29b-41d4-a716-446655440000"
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
    "workers": {
      "wechat-node-01": "online"
    }
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
        "status": "available"
      },
      {
        "name": "wechat",
        "type": "worker",
        "status": "online",
        "worker_id": "wechat-node-01"
      }
    ]
  }
}
```
