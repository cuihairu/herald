# REST API

## 认证 {#auth}

本页端点在开启 `auth.enabled` 后都需要 `Authorization: Bearer <token>`（或
`X-API-Key` 头携带 API Key；GET 请求也可用 `?api_key=` 查询参数）；未开启时匿名可用。
API Key 与 JWT 混用时先按 API Key 比对。JWT 由 `/auth/login` 签发，**有效期固定 24 小时**，
过期用 `/auth/refresh` 换新。四个例外域另见
[API 概述](./overview.md#认证)：`/auth/*` 自身、集成者 app 面（按 app token 三级
scope 鉴权，独立于 `auth.enabled`）、平台自证回调（bot/公众号/飞书）与公开读口
（`/api/v1/status`、`/feeds/**`）。

## POST /api/v1/auth/login {#auth-login}

登录换取 JWT token。用户名或密码错误返回 401。token 也可通过 `X-API-Key` 头携带
API Key 代替（见[认证](#auth)）。

### login-请求 {#login-request}

```json
{
  "username": "admin",
  "password": "admin"
}
```

### login-响应 {#login-response}

```json
{
  "token": "eyJ...",
  "user": {
    "id": "u-1",
    "username": "admin",
    "role": "admin"
  }
}
```

## POST /api/v1/auth/refresh {#auth-refresh}

刷新 JWT：凭 Bearer token（或 `herald_token` cookie / `?token=` 查询参数）换发新
token。token 缺失、无效或过期返回 401。

### refresh-响应 {#refresh-response}

```json
{ "token": "eyJ..." }
```

## GET /api/v1/auth/me {#auth-me}

查询当前登录用户。token 缺失、无效或过期返回 401；用户不存在返回 404。

### me-响应 {#me-response}

```json
{
  "id": "u-1",
  "username": "admin",
  "role": "admin"
}
```

## POST /api/v1/notify

发送通知。

模型语义：这里受理的是 **Notification**（通知），展开出的每一条渠道投递是 **Delivery**。`accepted` 只代表 Herald 已受理，不代表所有 Provider 均已送达（投递结果看 [/api/v1/logs](#get-logs)）。请求按 `type` + `level` 路由，也可显式命名接收方（`channels` / `channel` / `audience`）；`template` + `params`/`data` 走模板渲染，`title`/`body` 走直接内容。幂等语义见 [接收方与幂等](#notify-receivers)，字段演进依据 [受众领域模型总纲](/design-audience-model)。

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
  "channels": ["telegram", "aliyunsms"]
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
| level    | string | 否    | 级别。无枚举校验（任意字符串透传），惯用 `info` / `warning` / `error`，`critical` 可用；与紧急度、事件 severity 的映射见[事件与告警模型](/guide/events) |
| channels | array  | 否    | 指定渠道，不指定则根据 type/level 路由 |
| channel  | string | 否    | 单渠道写法，与 `channels` 并列并集（见[接收方与幂等](#notify-receivers)） |
| audience | array  | 否    | 受众引用（`group:` / `user:` / 裸渠道名），逐项按渠道展开 |
| recipients | map | 否    | 按渠道指定接收人 `{ "telegram": ["chat_id_1"] }` |
| template | string | 否    | 模板 ID |
| params   | map    | 否    | 模板参数 |
| data     | map    | 否    | 模板数据，与 `params` 合并、`params` 优先 |
| idempotency_key | string | 否    | 幂等键：进程生命周期内同键重复请求返回首次结果、不再投递 |
| title    | string | 否    | 直接标题（无模板时使用） |
| body     | string | 否    | 直接内容（无模板时使用） |

### notify-响应 {#notify-response}

三种结果（成功 / 部分失败 / 全部失败）的 **HTTP 状态码都是 200**：部分失败的
body `code` 仍是 0，只有全部失败才把 body `code` 写成 422。HTTP 状态码只用于
传输层错误（如请求体不合法 400）。

**成功：**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "notification_id": "550e8400-e29b-41d4-a716-446655440000",
    "task_ids": ["task-001", "task-002"],
    "accepted": ["telegram", "email"]
  }
}
```

`failed` 键只在非空时出现（全成功时整体缺席）。

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
      { "Channel": "sms-x", "Error": "provider not found: sms-x" }
    ]
  }
}
```

`failed` 条目的键是首字母大写的 `Channel` / `Error`（历史形状，无小写别名）。

**全部失败：**

```json
{
  "code": 422,
  "message": "all channels failed: [sms-x: provider not found: sms-x]",
  "data": {
    "notification_id": "550e8400-e29b-41d4-a716-446655440000",
    "accepted": [],
    "failed": [
      { "Channel": "sms-x", "Error": "provider not found: sms-x" }
    ]
  }
}
```

### 接收方与幂等 {#notify-receivers}

**接收方拼集**：`channels`、`channel`、`audience` 三个字段并列、可任意混用，最终接收方为三者合并。`audience` 的每一项按受众引用展开：`group:` 解析为群组成员、`user:` 解析为[配置化接收人](/guide/configuration#领域模型与配置块)、裸名字按渠道名处理。`data` 与 `params` 同为模板数据、合并生效，重名时 `params` 优先。

```json
{
  "type": "server.alert",
  "level": "error",
  "channel": "telegram",
  "audience": ["user:alice", "group:ops"],
  "template": "server_alert",
  "data": { "host": "node-17", "status": "offline" },
  "idempotency_key": "alert:node-17:4"
}
```

**幂等**：携带 `idempotency_key` 的请求，在 **进程生命周期内**重复发送同键请求时，返回首次请求的记录结果（同样的 `notification_id` / `task_ids`），且**不产生新的投递**。只有成功受理（`code` 0）的请求会被记录；全部失败（422）的请求不记录、可修正后重试同键。幂等表为内存实现（约 1000 条 FIFO 逐出，服务重启即清空）。需要跨重启幂等的场景请由调用方自行去重。

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
        "mode": "remote",
        "capabilities": ["wechat", "sms"],
        "connected_at": "2026-05-18T08:00:00Z",
        "last_heartbeat": "2026-05-18T08:05:00Z",
        "status": "online"
      }
    ]
  }
}
```

`mode` 为 `local` / `remote`；`status` 为字符串 `online` / `offline`。未接入远程
Worker 的部署返回空列表（`count` 0）。

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
| status   | string | 按状态过滤（`success` / `failed` / `pending` / `shadow`，与 wire 词汇一致） |
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
    "logs": [
      {
        "id": "21a2a819-9392-4b4d-b9a7-e868a658f6a9",
        "provider": "log",
        "payload_kind": "content",
        "level": "error",
        "status": "success",
        "error": "",
        "created_at": "2026-10-10T12:53:09+08:00",
        "completed_at": "2026-10-10T12:53:09+08:00",
        "matched_at": "0001-01-01T00:00:00Z",
        "category": "server.alert"
      }
    ]
  }
}
```

每条投递任务一行，按 `created_at` 降序。`status` ∈ `success` / `failed` / `pending` / `shadow`（重试中的任务保持 `pending`，终态才翻转）。`payload_kind` ∈ `content` / `provider_template` / `raw`。`category` 是通知的 `type`（app 面投递为注册品类）。`matched_at` 为规则匹配时间，未经规则引擎的任务显示零值 `0001-01-01T00:00:00Z`。`duration`（毫秒）与 `error` 只在非零/非空时出现；shadow 行额外带 `would_fire` 与 `channels`。

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
        "shadow_hits": 42,
        "route": [{ "channels": ["feishu-oncall", "group:ops"] }]
      }
    ],
    "count": 1
  }
}
```

`mode` 是启停载体：`active` 生效、`shadow` 只观察不动作、`off` 停用（省略时按 `shadow` 处理）。`action` 是决策层动词：`route` 改道（默认）、`allow` 放行、`suppress` 抑制；`route` 步骤只属于 `action=route`，带上会被拒。`priority` 从高到低排序，同级保持写入顺序，第一条命中的生效规则决定结果并停止求值。

`shadow_hits` 是该规则在影子期累计的命中次数（`GET /api/v1/rules/{id}` 也带这个字段）：只统计"本会触发"的影子命中，因 `for` 挂起、组折叠、抑制、静默、丢弃而扣下的事件各记各的、不计入其中。它是**进程内观测状态**：重启归零、不随规则持久化；把规则切到 `active` 后计数停止增长，切回 `shadow` 继续。切之前先看这个数：它就是激活后真实会发出去的量。列表只带累计计数；近 24 小时/近 7 天窗口与最近命中样本在**规则详情**的 `shadow_stats` 里（见 `GET /api/v1/rules/{id}`）。

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

查询指定规则，不存在返回 404。响应是规则文档加上与列表同一份 `shadow_hits` 统计，以及完整的影子统计读面 `shadow_stats`（控制台「详情」抽屉渲染的就是它）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": "prod-payment-failure",
    "match": "level == \"error\"",
    "shadow_hits": 42,
    "shadow_stats": {
      "total": 42,
      "last_24h": 9,
      "last_7d": 30,
      "samples": [
        { "at": "2026-10-03T08:00:00Z", "type": "deploy", "level": "error", "title": "上线完成", "channels": ["feishu-oncall"] }
      ]
    }
  }
}
```

`samples` 是最近命中（最新在前，最多 20 条，从未命中时省略），来自独立于投递日志的采样环形缓冲，每条影子命中都进环。窗口按整点小时桶累计（`last_24h`/`last_7d`）。与 `shadow_hits` 一样是进程内观测状态：重启归零。未接 Runtime 的部署省略 `shadow_stats` 字段。

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

删除群组，不存在返回 404。删除**不会**清理规则里对它的引用：仍在引用它的规则会在派发时因未知群组显式失败，这胜过悄悄改写用户的规则。

## GET /api/v1/rosters {#rosters-list}

查询值班表列表。值班表管理器未配置时返回 503。

### rosters-响应 {#rosters-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "rosters": [
      {
        "id": "ops-oncall",
        "description": "周末值班",
        "periods": [
          { "start": "2026-10-10T09:00:00+08:00", "end": "2026-10-10T18:00:00+08:00" }
        ]
      }
    ],
    "count": 1
  }
}
```

值班表是外部排班系统推进 herald 的绝对时段表：规则的 `silence: {roster: "ops-oncall"}` 在任一时段覆盖的每一刻静默该规则（见 [rules-响应](#rules-response) 的 silence 字段）。herald 只存与判定推送来的时段，不做排班。

## POST /api/v1/rosters {#roster-create}

推送值班表（排班系统整表全量推送）。ID 已存在返回 409；时段校验失败（缺时间戳、零长度/倒置时段、时段重叠、超过 512 段、空时段表）返回 400。乱序时段会被自动按 start 排序。

### roster-create-请求 {#roster-create-request}

```json
{
  "id": "ops-oncall",
  "description": "周末值班",
  "periods": [{ "start": "2026-10-10T09:00:00+08:00", "end": "2026-10-10T18:00:00+08:00" }]
}
```

## GET /api/v1/rosters/{id} {#roster-get}

查询指定值班表，不存在返回 404。

## PUT /api/v1/rosters/{id} {#roster-update}

整体替换该值班表的时段（URL 里的 id 优先于请求体），替换立即对静默判定生效。

## DELETE /api/v1/rosters/{id} {#roster-delete}

删除值班表，不存在返回 404。删除不会改写引用它的规则：引用它的静默立即失效（fail-open，缺日程数据不等于该静默生效），恢复投递比悄悄静默安全。

## GET /api/v1/alerts/{id} {#alert-get}

查询告警的确认状态。ack 存储未配置返回 503。

### alert-get-响应 {#alert-get-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "alert_id": "alert-42",
    "acknowledged": true,
    "ack": {
      "alert_id": "alert-42",
      "acked_by": "alice",
      "source": "api",
      "acked_at": "2026-10-04T08:00:00Z"
    }
  }
}
```

未确认时 `acknowledged` 为 `false` 且省略 `ack` 字段。

## POST /api/v1/alerts/{id}/ack {#alert-ack}

确认告警。**幂等**：同一告警首条确认生效，重复确认返回同一条记录。及时确认会
取消该告警待触发的升级（`escalation`），并把事件账本上的对应事件标为已确认。
ack 存储未配置返回 503。

### alert-ack-请求 {#alert-ack-request}

```json
{ "acked_by": "alice" }
```

`acked_by` 可省略（认领人为空）。响应为确认记录本体（同上例的 `ack` 字段）。

## GET /api/v1/incidents {#incidents-list}

查询事件账本（规则路由的投递打开的事件，确认与恢复关闭），最新在前。事件存储
未配置返回 503。

### incidents-参数 {#incidents-params}

| 参数       | 类型     | 描述    |
| -------- | ------ | ----- |
| status   | string | 按状态过滤：`open` / `acked` / `resolved`（省略或 `all` 为全部；其他值 400） |
| rule_id  | string | 按规则过滤 |
| alert_id | string | 按告警过滤 |
| limit    | int    | 返回条数（默认 100，最大 1000，越界 400） |

状态不是响应里的字段：`open` / `acked` / `resolved` 由 `resolved_at` / `acked_at`
是否存在**派生**，过滤按派生状态执行。

### incidents-响应 {#incidents-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "incidents": [
      {
        "id": "incident-1",
        "rule_id": "prod-payment-failure",
        "alert_id": "alert-42",
        "title": "支付失败率过高",
        "level": "error",
        "opened_at": "2026-10-04T08:00:00Z",
        "events": 7,
        "channels": ["feishu-oncall"]
      }
    ],
    "count": 1
  }
}
```

## GET /api/v1/incidents/{id} {#incident-get}

查询单个事件及其完整时间线（`timeline`），不存在返回 404。

## POST /api/v1/callbacks/feishu {#feishu-callback}

飞书互动卡片回调入口（卡片上的确认按钮）。**不走 Bearer 认证**，调用方是飞书
的服务器，凭 `card_callback.encrypt_key` 认证：应答飞书的 `url_verification`
挑战；加密回调（`encrypt` 字段，AES-256-CBC）在配置了加密密钥时解密处理，未
配置返回 501；明文回调始终可处理。确认动作与 `POST /alerts/{id}/ack` 走同一套
存储：首条确认生效、取消待触发升级、事件账本落账。ack 存储未配置返回 503。

### feishu-callback-响应 {#feishu-callback-response}

挑战应答：

```json
{ "code": 0, "message": "ok", "data": { "challenge": "..." } }
```

确认动作：

```json
{
  "code": 0,
  "message": "ok",
  "data": { "alert_id": "alert-42", "acknowledged": true, "acked_by": "ou_xxx" }
}
```

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

## POST /api/v1/callbacks/bot {#bot-callback}

Telegram 平台回调入口（来源适配器：`/start` 兑换绑定、`/stop` 全停订阅）。**不走 Bearer**，凭请求头 `X-Telegram-Bot-Api-Secret-Token` 与 `sources.bot.secret` 常量时间比对；不匹配 403，secret 未配置时端点整体 404。payload 是 Telegram update JSON 子集（`message.chat.id` + `message.text`）。一切良构 update 恒回 200（Telegram 对非 2xx 会重试）。

## POST /api/v1/callbacks/wechat-mp {#wechat-mp-callback}

微信公众号回调入口（关注/取关来源适配器）。**不走 Bearer**，query 参数 `signature` / `timestamp` / `nonce` 三件套自证：`signature == hex(sha1(sort([timestamp, nonce, token]).join("")))`，`token` 来自 `sources.wechat_mp.token`，常量时间比对；缺一 403，token 未配置时端点整体 404。`GET` 用于控制台 URL 验证（原样回显 `echostr`）。POST body 是 XML（`FromUserName` + `Event` ∈ subscribe/unsubscribe）。openid 无绑定时静默 200。

## GET /feeds/{name} {#feeds-public}

公共品类 feed（RSS 拉式，匿名可读，不在 `/api/v1` 下）。`name` 形如 `<品类>.xml`，品类名须匹配 `^[a-zA-Z0-9._-]{1,64}$`——不匹配 404；合法但无人写过的品类回**空频道 200**（拉式源没人写过，这是诚实答案）。只含无受众引用的公开内容。响应 `Content-Type: application/rss+xml; charset=utf-8`。非 GET 方法 405、渲染失败 500（两者均为纯文本，不走 JSON 信封）。

## GET /feeds/private/{name} {#feeds-private}

受众私密 feed。`name` 形如 `<rss_token>.xml`，**token 即凭据**（随受众稳定签发，重置即失效）；未知 token 404。可见性在读取时判定：条目只在受众对该品类的实时订阅关系允许 rss 渠道时才出现——**取关即从下一次拉取起消失**，投影侧零记账。响应格式与错误语义同公共 feed。

> **缺口（审计 #22，待拍板）**：token 目前**没有任何 API 或界面可读路径**——`RSSToken()` 仅在测试中被调用，绑定/关系/审计响应都不带 token。投影、端点、可见性判定均已工作，但读者拿不到自己的 feed 地址，私密 feed 实际不可达。补法（绑定响应带 token / 新增 surfaces 读口）属设计决策，见[一致性审计](/审计-文档一致性)。

## GET /api/v1/audiences/{id}/relations {#audience-relations}

查询受众现有关系（订阅/指派）。未知受众返回 200 空列表（path id 不做格式校验）。

```json
{"code":0,"message":"ok","data":{"relations":[{
  "audience_id":"alice","category":"bills","channel":"email",
  "type":"subscription","source":"test",
  "policy":{"allow_unsubscribe":true,"must_deliver":false}
}]}}
```

`type` ∈ `subscription`（主动订阅，永远可退订）/ `enrollment`（被动指派，must-deliver 或可退订二选一）；`source` ∈ `bot` / `wechat_mp` / `preference_center` / `admin` / `app:<name>`。

## POST / DELETE /api/v1/audiences/{id}/subscriptions {#audience-subscriptions}

偏好中心勾选/退订入口。来源适配器未配置时 404。

**POST 请求体**：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| category | string | 是 | 1–64 字符 |
| channel | string | 是 | 1–64 字符 |

落下 `type=subscription`、`source=preference_center` 的关系。**响应是关系对象本体，wire 键是 Go 字段名（首字母大写）**：

```json
{"AudienceID":"alice","Category":"bills","Channel":"email","Type":"subscription",
 "Source":"preference_center","Policy":{"AllowUnsubscribe":true,"MustDeliver":false}}
```

**DELETE** 用查询参数 `?category=&<>&channel=`：槽位无关系 404 `subscription not found`；must-deliver 关系不可退订 409。

## POST / DELETE /api/v1/audiences/{id}/surfaces {#audience-surfaces}

管理侧代绑定联系面（用户联系信息在整合方库里的场景），操作入口记为 `admin` 并进审计流。来源适配器未配置时 404。

**POST 请求体**：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| channel | string | 是 | 1–64 字符 |
| target | string | 是 | 联系目标（≤256 字符） |
| categories | array | 否 | 随绑定一并订阅的默认品类 |

```json
{"code":0,"message":"ok","data":{
  "surface":{"audience_id":"user.1","channel":"email","target":"u1@example.com","status":"active"},
  "bind_changed":true,
  "subscribed":1
}}
```

`status` ∈ `pending` / `active` / `invalid`；`bind_changed` 表示 surface 状态是否迁移；`subscribed` 是成功落地的默认订阅**数量**。活跃槽位已持有不同 target 时拒绝（422 重绑守卫）。

**DELETE** 用查询参数 `?channel=`：surface 标记 `invalid`（停投），该 channel 上所有 subscription 型关系终止，**enrollment 关系保留**（must-deliver 底线不是受众可撤销的）。未知/已 invalid 的 surface 返回 200 `{"surface_invalidated":false,"terminated":0}`。
