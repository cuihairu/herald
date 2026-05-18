# 模块设计

## 1. Herald API

主要职责：

- HTTP REST
- Auth
- OpenAPI
- Webhook Inbound

### Notify API

```http
POST /api/v1/notify
```

```json
{
  "title": "Node Offline",
  "body": "node-17 offline",
  "level": "error",
  "channels": ["telegram"]
}
```

### Event API

```http
POST /api/v1/events
```

```json
{
  "type": "node.offline",
  "labels": {
    "region": "shanghai"
  }
}
```

## 2. Event Queue

**必须异步**。

禁止：
```text
HTTP 请求同步发送 Provider  // ❌ 错误
```

正确模型：
```
API → Queue → Dispatcher → Provider Runtime
```

## 3. Route Engine

负责：`Event → Provider`

### 示例

```yaml
routes:
  error:
    - telegram
    - wecom

  warning:
    - feishu
```

### 后期支持

```yaml
match:
  labels.region == "shanghai"
```

## 4. Retry

支持：

| 类型           | Retry |
| ------------ | ----- |
| timeout      | yes   |
| 429          | yes   |
| 502          | yes   |
| auth failed  | no    |

**策略：** Exponential Backoff

## 5. Dedup

避免：`offline x1000`

**策略：** `title + labels hash`

**窗口：** 5 min

## 6. Rate Limit

建议：每个 Runtime 自行实现

因为：各平台规则不同。
