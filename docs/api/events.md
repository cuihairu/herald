# 事件 API

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
    "region": "shanghai",
    "node": "node-17"
  },
  "data": {
    "message": "node is offline",
    "since": "2026-05-18T10:00:00Z"
  }
}
```

### 参数

| 字段     | 类型     | 必填   | 描述    |
| ------ | ------ | ---- | ----- |
| type   | string | 是    | 事件类型  |
| labels | object | 是    | 标签    |
| data   | object | 否    | 附加数据 |

### 事件路由

事件会根据配置的路由规则自动投递到对应的 Provider。

```yaml
routes:
  node.offline:
    - telegram
    - feishu
```

### 响应

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "event_id": "evt_550e8400-e29b-41d4-a716-446655440000",
    "routed_to": ["telegram", "feishu"]
  }
}
```

## 事件类型建议

| 类型             | 描述    |
| -------------- | ----- |
| node.offline   | 节点下线  |
| node.online    | 节点上线  |
| deploy.failed  | 部署失败  |
| deploy.success | 部署成功  |
| alert.triggered| 告警触发  |
