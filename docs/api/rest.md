# REST API

## POST /api/v1/notify

发送通知。

### notify-请求 {#notify-request}

```http
POST /api/v1/notify
Content-Type: application/json
```

**使用模板：**

```json
{
  "type": "server.alert",
  "level": "error",
  "template": "server_alert",
  "params": {
    "host": "node-17",
    "status": "offline"
  },
  "channels": ["telegram", "aliunsms"]
}
```

**直接内容：**

```json
{
  "type": "server.alert",
  "level": "error",
  "title": "Node Offline",
  "body": "node-17 is offline",
  "channels": ["telegram"]
}
```

### notify-参数 {#notify-params}

| 字段       | 类型     | 必填   | 描述    |
| -------- | ------ | ---- | ----- |
| type     | string | 是    | 通知类型（用于路由） |
| level    | string | 否    | 级别 (debug/info/warning/error/critical) |
| channels | array  | 否    | 指定渠道，不指定则根据 type/level 路由 |
| recipients | map | 否    | 按渠道指定接收人 `{ "telegram": ["chat_id_1"] }` |
| template | string | 否    | 模板 ID |
| params   | map    | 否    | 模板参数 |
| title    | string | 否    | 直接标题（无模板时使用） |
| body     | string | 否    | 直接内容（无模板时使用） |

### notify-响应 {#notify-response}

**成功：**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "notification_id": "550e8400-e29b-41d4-a716-446655440000",
    "task_ids": ["task-001", "task-002"],
    "accepted": ["telegram", "email"],
    "failed": []
  }
}
```

**部分失败：**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "notification_id": "550e8400-e29b-41d4-a716-446655440000",
    "task_ids": ["task-001"],
    "accepted": ["telegram"],
    "failed": [
      { "channel": "aliunsms", "error": "factory not found" }
    ]
  }
}
```

**全部失败：**

```json
{
  "code": 422,
  "message": "all channels failed: [aliunsms: factory not found]",
  "data": {
    "notification_id": "550e8400-e29b-41d4-a716-446655440000",
    "accepted": [],
    "failed": [
      { "channel": "aliunsms", "error": "factory not found" }
    ]
  }
}
```

## GET /api/v1/status {#get-status}

查询服务状态。

### status-响应 {#status-response}

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

## GET /api/v1/providers {#get-providers}

查询 Provider 列表。

### providers-响应 {#providers-response}

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
        "name": "aliunsms",
        "type": "builtin",
        "status": "available",
        "enabled": false,
        "since": "2026-05-18T08:00:00Z"
      }
    ]
  }
}
```

## POST /api/v1/providers/{name}/enable {#provider-enable}

启用指定的 Provider。

### enable-请求 {#enable-request}

```http
POST /api/v1/providers/telegram/enable
```

### enable-响应 {#enable-response}

```json
{
  "code": 0,
  "message": "provider enabled"
}
```

## POST /api/v1/providers/{name}/disable {#provider-disable}

禁用指定的 Provider。

### disable-请求 {#disable-request}

```http
POST /api/v1/providers/telegram/disable
```

### disable-响应 {#disable-response}

```json
{
  "code": 0,
  "message": "provider disabled"
}
```

## GET /api/v1/workers {#get-workers}

查询已连接的 Workers。

### workers-响应 {#workers-response}

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

## GET /api/v1/queue {#get-queue}

查询队列状态。

### queue-响应 {#queue-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "size": 5
  }
}
```

## GET /api/v1/logs {#get-logs}

查询投递日志。

### logs-参数 {#logs-params}

| 参数       | 类型     | 描述    |
| -------- | ------ | ----- |
| offset   | int    | 偏移量 |
| limit    | int    | 每页数量（默认 50，最大 500） |
| status   | string | 按状态过滤（success/failed） |
| provider | string | 按 Provider 过滤 |
| level    | string | 按级别过滤 |
| since    | string | 起始时间（RFC3339） |
| until    | string | 结束时间（RFC3339） |

### logs-响应 {#logs-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "total": 100,
    "offset": 0,
    "limit": 50,
    "logs": [...]
  }
}
```

## GET /api/v1/logs/stats {#logs-stats}

查询日志统计。

## GET /api/v1/logs/{id} {#log-by-id}

查询单条日志。

## GET /api/v1/config/{name} {#config-get}

查询 Provider 配置。

## PUT /api/v1/config/{name} {#config-update}

更新 Provider 配置。

## GET /api/v1/rules {#rules-list}

查询规则列表。规则引擎未配置时返回 503。

### rules-响应 {#rules-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "rules": [
      {
        "id": "prod-payment-failure",
        "match": "level == \"error\" && params.fail_rate > 0.05",
        "mode": "active",
        "action": "route",
        "priority": 10,
        "route": [{ "channels": ["feishu-oncall", "group:ops"] }]
      }
    ],
    "count": 1
  }
}
```

`mode` 是启停载体：`active` 生效、`shadow` 只观察不动作、`off` 停用（省略时按 `shadow` 处理）。`action` 是决策层动词：`route` 改道（默认）、`allow` 放行、`suppress` 抑制；`route` 步骤只属于 `action=route`，带上会被拒。`priority` 从高到低排序，同级保持写入顺序，第一条命中的生效规则决定结果并停止求值。

## POST /api/v1/rules {#rule-create}

创建规则。**保存即编译**：表达式在这里编译，非法表达式直接 400，不会进入求值路径。ID 已存在返回 409。

### rule-create-请求 {#rule-create-request}

```json
{
  "id": "prod-payment-failure",
  "match": "level == \"error\" && params.fail_rate > 0.05",
  "mode": "active",
  "action": "route",
  "priority": 10,
  "route": [{ "channels": ["feishu-oncall", "group:ops"] }]
}
```

## GET /api/v1/rules/{id} {#rule-get}

查询指定规则，不存在返回 404。

## PUT /api/v1/rules/{id} {#rule-update}

整体替换该规则（PUT 语义，**URL 里的 id 优先于请求体**）。启停开关走的就是这条：带上原字段、只改 `mode` 即可。同样在写入前编译表达式。

## DELETE /api/v1/rules/{id} {#rule-delete}

删除规则，不存在返回 404。

## GET /api/v1/groups {#groups-list}

查询通知群组列表。群组管理器未配置时返回 503。

### groups-响应 {#groups-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "groups": [
      {
        "id": "ops",
        "description": "值班花名册",
        "members": [
          { "channel": "feishu-oncall", "recipients": ["@zhang"] },
          { "channel": "sms-duty" }
        ]
      }
    ],
    "count": 1
  }
}
```

`recipients` 是可选的渠道内收件人钉选，省略表示该渠道的全部收件人。群组可以被规则的路由渠道以 `group:<id>` 形式引用（见 [rules-响应](#rules-response)），引用一个不存在的群组会在派发时显式失败，而不是静默丢弃。

## POST /api/v1/groups {#group-create}

创建群组。ID 已存在返回 409。

### group-create-请求 {#group-create-request}

```json
{
  "id": "ops",
  "description": "值班花名册",
  "members": [{ "channel": "feishu-oncall", "recipients": ["@zhang"] }]
}
```

## GET /api/v1/groups/{id} {#group-get}

查询指定群组，不存在返回 404。

## PUT /api/v1/groups/{id} {#group-update}

整体替换该群组（URL 里的 id 优先于请求体），替换立即对派发热路径生效。

## DELETE /api/v1/groups/{id} {#group-delete}

删除群组，不存在返回 404。删除**不会**清理规则里对它的引用——仍在引用它的规则会在派发时因未知群组显式失败，这胜过悄悄改写用户的规则。

## GET /api/v1/templates {#templates-list}

查询模板列表。

## POST /api/v1/templates/create {#template-create}

创建模板。

### create-请求 {#create-request}

```json
{
  "id": "server_alert",
  "name": "服务器告警",
  "title": "服务器 {{.host}} 告警",
  "level": "error",
  "fields": [
    { "label": "主机", "value": "{{.host}}" },
    { "label": "状态", "value": "{{.status}}" }
  ]
}
```

## GET /api/v1/templates/{id} {#template-get}

查询指定模板。

## PUT /api/v1/templates/{id} {#template-update}

更新模板。

## DELETE /api/v1/templates/{id} {#template-delete}

删除模板。
