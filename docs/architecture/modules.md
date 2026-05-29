# 模块设计

## 核心模块

### Worker Pool（统一 Worker 池）

Worker Pool 管理一组 Worker，统一从 Queue 消费任务并投递：

```
Queue → Worker Pool → Runtime Manager → Provider
```

**核心职责：**

1. 启动 N 个 local worker goroutine
2. 每个 worker 循环：Pop → Deliver → Ack/Nack
3. 管理远程 Worker 注册信息

**源码位置：** `core/worker/pool.go`

### Registry（Worker 注册表）

统一管理所有 Worker 的元信息：

```
┌─────────────────────────────────┐
│          Registry               │
│                                 │
│  local-0   → { mode: local }   │
│  local-1   → { mode: local }   │
│  worker-01 → { mode: remote }  │
│  worker-02 → { mode: remote }  │
└─────────────────────────────────┘
```

**源码位置：** `core/worker/registry.go`

## 数据流

```
API Request → Handler → NotificationService → DeliveryPlanner → Queue → Worker Pool → Runtime → Provider
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
    ID          string
    Type        string
    Level       string
    Channels    []string
    Recipients  map[string][]string
    TemplateRef string
    Params      map[string]any
    Content     *DirectContent
}
```

### DeliveryTask（投递任务）

Provider 层的输入，描述"怎么发到具体渠道"。

```go
type DeliveryTask struct {
    ID       string
    Provider string
    Targets  []string
    Payload  DeliveryPayload
    Level    string
}

type DeliveryPayload struct {
    Kind             PayloadKind
    Content          *RenderedContent
    ProviderTemplate *ProviderTemplatePayload
    Raw              map[string]any
}
```

### Queue（任务队列）

```go
type Queue interface {
    Push(ctx context.Context, task *DeliveryTask) error
    Pop(ctx context.Context) (*DeliveryTask, error)
    Ack(ctx context.Context, taskID string) error
    Nack(ctx context.Context, taskID string, reason error) error
    Size() int
    Close() error
}
```

## 模块列表

### 1. NotificationService

核心编排层，流程：

1. **去重** — 基于内容的 SHA256 稳定 key
2. **路由** — 根据 type/level 解析目标渠道
3. **模板渲染** — 替换模板变量
4. **投递规划** — 为每个渠道生成 DeliveryTask
5. **入队** — 推送到 Queue

### 2. DeliveryPlanner

根据 Provider 能力和模板 Binding 生成 `DeliveryTask`：

- **SMS Provider** → `ProviderTemplatePayload`
- **内容 Provider** → `RenderedContent`

### 3. Template + Binding

一个业务模板可映射到多个渠道配置：

```yaml
templates:
  server_alert:
    bindings:
      email:        { format: html }
      telegram:     { format: markdown }
      aliyunsms:
        template_code: "SMS_123456"
        params: { "主机": "host", "状态": "status" }
```

### 4. Route Engine

负责：`Notification Type/Level → Provider`

### 5. Retry

接入 `runtime.Manager.Deliver()`，支持指数退避重试。

### 6. Dedup

基于内容的稳定 key（SHA256），窗口 5 分钟。

### 7. Queue

队列抽象，当前实现：

| 实现 | 适用场景 | Remote Worker |
|------|---------|---------------|
| `memory` | 单机部署 | 不支持 |
| `redis` | 分布式部署 | 支持 |

### 8. WebSocket Hub

远程 Worker 的管理通道（非任务分发通道）：

- Worker 注册与能力记录
- 心跳保活与超时注销
- 状态事件上报

### 9. Provider Capability

每个 Provider 声明自己的能力：

```go
type ProviderCapability struct {
    PayloadKinds     []PayloadKind
    ContentFormats   []string
    SupportsBatch    bool
    SupportsTemplate bool
}
```
