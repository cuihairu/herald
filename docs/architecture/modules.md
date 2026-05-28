# 模块设计

## 核心模块

### Dispatcher（调度器）

调度器负责从队列中消费任务并分发给相应的 Runtime：

```
Queue → Dispatcher → Runtime Manager → Provider
                      │
                      ├─ Builtin Provider（直接投递）
                      └─ Worker Provider（WebSocket 分发）
```

**核心职责：**

1. 从队列中获取待投递任务
2. 根据 Provider 类型选择分发方式
3. Builtin Provider → 直接通过 Runtime Manager 投递
4. Worker Provider → 通过 WebSocket Hub 分发给远程 Worker

**源码位置：** `core/dispatch/dispatcher.go`

## 数据流

```
API Request → Handler → NotificationService → DeliveryPlanner → Queue → Dispatcher → Runtime → Provider
                          │                      │
                          ├─ Template 渲染       ├─ Binding 解析
                          ├─ Dedup 去重          ├─ SMS 参数适配
                          └─ Route 路由          └─ Renderer 内容渲染
```

## 核心类型

### Notification（通知意图）

API 层的输入，描述"用户想发什么"。

```go
type Notification struct {
    ID          string              // 通知唯一标识
    Type        string              // 通知类型，用于路由
    Level       string              // 级别
    Channels    []string            // 目标渠道
    Recipients  map[string][]string // 按渠道的接收人
    TemplateRef string              // 模板 ID
    Params      map[string]any      // 模板参数
    Content     *DirectContent      // 直接内容（无模板时）
}
```

### DeliveryTask（投递任务）

Provider 层的输入，描述"怎么发到具体渠道"。

```go
type DeliveryTask struct {
    ID      string
    Provider string
    Targets []string
    Payload DeliveryPayload
    Level   string
}

type DeliveryPayload struct {
    Kind             PayloadKind              // content / provider_template / raw
    Content          *RenderedContent         // 内容类 Provider 使用
    ProviderTemplate *ProviderTemplatePayload // SMS 类 Provider 使用
    Raw              map[string]any           // 原始透传
}
```

## 1. NotificationService

核心编排层，流程：

1. **去重** — 基于内容的 SHA256 稳定 key（type+level+channels+params+content）
2. **路由** — 根据 type/level 解析目标渠道
3. **模板渲染** — 替换模板变量
4. **投递规划** — 为每个渠道生成 DeliveryTask
5. **入队** — 推送到 Queue

返回 `ProcessResult`，包含 accepted/failed 渠道列表，错误透明上报。

## 2. DeliveryPlanner

根据 Provider 能力和模板 Binding 生成 `DeliveryTask`：

- **SMS Provider**（aliunsms/tencentsms/neteasesms）→ `ProviderTemplatePayload`
  - 从模板 Binding 获取 `template_code`/`template_id`
  - 通过 `params`（命名参数）或 `param_order`（有序参数）适配不同厂商
- **内容 Provider**（email/telegram/feishu 等）→ `RenderedContent`
  - 使用 Renderer 渲染为 HTML/Markdown/Plain/JSON
  - Binding 中可指定 `format` 覆盖默认格式

## 3. Template + Binding

一个业务模板可映射到多个渠道配置：

```yaml
templates:
  server_alert:
    name: "服务器告警"
    title: "服务器 {{.host}} 告警"
    level: error
    fields:
      - { label: "主机", value: "{{.host}}" }
      - { label: "状态", value: "{{.status}}" }
    bindings:
      email:        { format: html }
      telegram:     { format: markdown }
      aliyunsms:
        template_code: "SMS_123456"
        params: { "主机": "host", "状态": "status" }
      tencentsms:
        template_id: "789"
        param_order: ["主机", "状态"]
```

### SMS 参数适配

| Provider | Template 字段 | 参数类型 | 格式 |
|----------|--------------|---------|------|
| Aliyun   | `template_code` | `map[string]string` | JSON 字符串 |
| Tencent  | `template_id` | `[]string` | 有序数组 |
| NetEase  | `template_id` | `[]string` | 逗号分隔 |

## 4. Route Engine

负责：`Notification Type/Level → Provider`

```yaml
routes:
  server.alert:
    - telegram
    - email
  error:
    - telegram
    - wecom
```

## 5. Retry

接入 `runtime.Manager.Deliver()`，支持：

| 类型           | Retry |
| ------------ | ----- |
| timeout      | yes   |
| 429          | yes   |
| 502          | yes   |
| auth failed  | no    |

策略：Exponential Backoff

## 6. Dedup

基于内容的稳定 key（SHA256 of type+level+channels+params+content），窗口 5 分钟。

## 7. Queue

异步解耦：

```
API → Queue → Dispatcher → Provider Runtime
```

## 8. WebSocket Hub

管理 Worker 连接和任务分发：

```go
type Hub struct {
    server        *Server
    workerByCap   map[string]string  // capability → workerID
}
```

**职责：**

- Worker 注册与能力管理
- 任务分发（按 capability 或 worker_id）
- 连接状态管理

## 9. Provider Capability

每个 Provider 声明自己的能力：

```go
type ProviderCapability struct {
    PayloadKinds     []PayloadKind   // 支持的 payload 类型
    ContentFormats   []string        // html, markdown, plain, json
    SupportsBatch    bool
    SupportsTemplate bool            // 是否支持厂商模板（SMS）
}
```

## Provider 类型

### Builtin Provider

直接在 Herald 进程内运行，通过 Runtime Manager 直接投递。

**支持：** Telegram、Feishu、Email、Webhook、SMS 等

### Worker Provider

代理远程 Worker，通过 WebSocket 分发任务。

**支持：** 微信公众号、微信个人推送等需要特殊运行环境的渠道
