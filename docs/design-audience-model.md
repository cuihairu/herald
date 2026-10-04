# Audience 领域模型：Herald 完整架构改进计划书

> 状态：**收官（2026-10-04）**。Phase 0-9 全批落地，执行记录与留批清单见
> [现状审计](/design-audience-audit#五执行记录)。本文保留为计划书原貌：各 Phase 前的
> 代码审计（第 33 节 Phase 0）是执行起点的存档，本文本身不改代码。

## 1. 改造目标

本次改造不重写 Herald，做的是对现有实现的**领域模型收敛和架构边界明确化**。

Herald 的最终定位：

> **Herald is a lightweight, provider-agnostic notification delivery infrastructure for applications, operations, CI/CD and automation.**

中文：

> **Herald 是一个轻量、Provider 无关的统一通知投递基础设施，为业务系统、运维、CI/CD、Agent 和自动化任务提供统一的通知路由、受众管理与多渠道可靠投递能力。**

核心链路：

```text
Event
  │
  ▼
Notification
  │
  ├──────────────┐
  ▼              ▼
Audience       Template
  │
  ▼
Routing
  │
  ▼
Delivery Task
  │
  ▼
Queue
  │
  ▼
Worker
  │
  ▼
Provider
  │
  ├── Telegram
  ├── Feishu
  ├── Email
  ├── Webhook
  └── Log
```

其中：

* **Event**：发生了什么
* **Notification**：要通知什么
* **Audience**：通知面向谁
* **Recipient**：Audience 中具体的接收者
* **Endpoint**：Recipient 的具体投递地址
* **Template**：通知内容如何生成
* **Channel**：业务定义的通知逻辑通道
* **Routing**：决定通知如何展开成 Delivery
* **Delivery Task**：一次具体的渠道投递任务
* **Queue**：等待执行的任务
* **Worker**：执行投递
* **Provider**：实际调用 Telegram / Feishu / Email 等外部服务

## 2. 本次改造最重要的架构原则

必须明确以下边界：

```text
Notification ≠ Delivery Task

Audience ≠ Channel

Channel ≠ Provider

Recipient ≠ Endpoint

Routing ≠ Delivery

Provider ≠ Business Logic
```

这些边界是本次改造的核心。

## 3. Notification

### 3.1 定义

Notification 表示：

> **一次需要被 Herald 处理和投递的通知。**

例如：

```json
{
  "type": "ci.build.failed",
  "level": "error",
  "title": "Build Failed",
  "body": "CI #123 failed"
}
```

Notification 不应该知道：

```text
Telegram API
Feishu API
SMTP
Webhook URL
```

它只描述：

```text
发生了什么
通知内容是什么
通知级别是什么
面向什么受众
使用什么模板
```

## 4. Audience

### 4.1 定义

Audience 是 Herald 的核心概念：

> **Audience 表示一条 Notification 所面向的受众集合，即"这条通知应该通知谁"。**

例如：

```text
group:ops
group:backend
group:developers
user:alice
user:bob
```

Audience 不负责描述具体的通知技术。

### 4.2 为什么需要 Audience

如果没有 Audience，业务很容易直接写：

```json
{
  "provider": "telegram",
  "chat_id": "123456"
}
```

这样业务系统就开始管理：

```text
Telegram
Feishu
Email
Webhook
```

导致业务与通知渠道耦合。

引入 Audience 后：

```text
Notification
      │
      ▼
Audience: group:ops
      │
      ▼
具体接收者
```

业务只关心：

> "通知运维团队。"

而不是：

> "调用 Telegram 给某个 chat_id 发消息。"

## 5. Recipient

Audience 是"受众集合"，Recipient 是其中的具体接收者。

例如：

```text
Audience
  group:ops
       │
       ├── user:alice
       ├── user:bob
       └── user:charlie
```

所以：

```text
Audience = 谁这一群人
Recipient = 具体是谁
```

## 6. Endpoint

Recipient 不应该直接等于 Telegram / Email。

一个 Recipient 可能拥有多个 Endpoint：

```text
user:alice
   │
   ├── telegram:123456
   ├── email:alice@example.com
   └── feishu:ou_xxx
```

因此：

```text
Audience
    ↓
Recipient
    ↓
Endpoint
    ↓
Provider
```

这是 Herald 后续支持多渠道、多用户偏好的基础。

## 7. Audience 的 MVP 实现

不要一开始做复杂用户系统。

建议第一阶段只实现轻量模型：

```yaml
audiences:
  ops:
    recipients:
      - alice
      - bob

  backend:
    recipients:
      - alice
      - charlie
```

然后：

```yaml
recipients:
  alice:
    endpoints:
      - type: telegram
        target: "123456"
      - type: email
        target: "alice@example.com"

  bob:
    endpoints:
      - type: feishu
        target: "ou_xxx"
```

这样已经可以形成完整链路。

## 8. Channel

Audience 解决：

> **通知谁？**

Channel 解决：

> **这是哪类业务通知/逻辑通知通道？**

例如：

```text
ops
ci
security
backend
game
agent
```

Channel 可以作为 Routing 的输入。

例如：

```yaml
channels:
  ci:
    providers:
      - telegram
      - feishu

  security:
    providers:
      - telegram
      - email
```

因此：

```text
Audience = 谁
Channel = 哪一类通知通道
Provider = 怎么发送
```

三者不要混淆。

## 9. Provider

Provider 表示具体的投递实现：

```text
TelegramProvider
FeishuProvider
EmailProvider
WebhookProvider
LogProvider
```

Provider 负责：

```text
请求构造
认证
调用第三方 API
响应解析
错误转换
```

Provider 不负责：

```text
Audience
Routing
Queue
Retry Policy
业务逻辑
Template 选择
```

## 10. Routing

Routing 是 Herald 的核心协调层。

它负责：

> **根据 Notification、Audience、Channel、Template 等信息，计算最终需要创建哪些 Delivery Task。**

例如：

```text
Notification
type = ci.build.failed
level = error

Audience
group:ops

Channel
ci
```

经过 Routing：

```text
group:ops
      │
      ▼
alice
bob
      │
      ▼
Endpoints
      │
      ├── Telegram
      ├── Feishu
      └── Email
```

最终产生：

```text
DeliveryTask #1 → Alice → Telegram
DeliveryTask #2 → Alice → Email
DeliveryTask #3 → Bob → Feishu
```

## 11. Delivery Task

Delivery Task 表示：

> **一次具体的通知投递。**

例如：

```text
Notification N001

Delivery T001
  recipient = alice
  provider = telegram

Delivery T002
  recipient = alice
  provider = email

Delivery T003
  recipient = bob
  provider = feishu
```

## 12. 为什么 Notification 与 Delivery 必须分离

因为：

```text
Notification N001
```

可能出现：

```text
Telegram → SUCCESS
Email    → SUCCESS
Feishu   → FAILED
```

所以：

```text
Notification 状态
```

和：

```text
Delivery 状态
```

不能混为一谈。

Delivery 必须拥有独立生命周期。

## 13. Delivery 状态机

建议标准化：

```text
accepted
    ↓
queued
    ↓
delivering
    ├── delivered
    │
    └── failed
          ↓
       retrying
          ↓
       delivering
          │
          ├── delivered
          └── dead
```

至少支持：

```text
accepted
queued
delivering
delivered
failed
retrying
dead
```

## 14. Retry

Retry 属于 Delivery。

例如：

```text
T001 Telegram
    delivered

T002 Feishu
    failed
      ↓
    retrying
      ↓
    delivered
```

建议 Delivery 保存：

```text
attempts
max_attempts
last_error
next_retry_at
```

## 15. Provider 错误分类

不同 Provider 的错误应该统一转换为 Herald 错误类型。

建议至少：

```text
TemporaryError
PermanentError
RateLimitedError
AuthenticationError
InvalidRequestError
TimeoutError
```

例如：

```text
HTTP 429
    ↓
RateLimitedError
    ↓
Retry

HTTP 401
    ↓
AuthenticationError
    ↓
不应该无限 Retry
```

Retry Policy 不应该由 Provider 自己决定。

## 16. Queue

当前：

```yaml
queue:
  type: memory
  workers: 2
```

继续保留。

Queue 只负责：

```text
enqueue
dequeue
ack
```

暂时不要增加：

```text
Redis
Kafka
NATS
RabbitMQ
```

等外部依赖。

未来可以实现：

```text
Queue
 ├── MemoryQueue
 ├── RedisQueue
 ├── NATSQueue
 └── KafkaQueue
```

但第一阶段只需要 MemoryQueue。

## 17. Worker

Worker 负责：

```text
从 Queue 获取 Delivery Task
        ↓
调用 Provider
        ↓
成功 → delivered
失败 → retry / dead
```

Worker 不应该决定：

```text
发给谁
使用哪个 Provider
使用什么模板
```

这些应该已经由 Routing 完成。

## 18. Template

Template 解决：

> **通知内容如何生成。**

例如：

```yaml
templates:
  ci.build.failed:
    title: "Build Failed"
    body: |
      Repository: {{repository}}
      Build: {{build_id}}
      Branch: {{branch}}
```

Template 与 Provider 解耦。

例如同一个 Notification：

```text
Template
    ↓
统一 Notification 内容
    ↓
Telegram → Markdown
Feishu   → Card
Email    → HTML
```

Provider 只负责最终格式适配，不负责业务模板逻辑。

## 19. Event

长期支持：

```text
Event
   ↓
Rule
   ↓
Notification
```

例如：

```json
{
  "event": "github.pull_request.merged",
  "repository": "cuihairu/herald",
  "number": 123
}
```

然后：

```text
Rule
github.pull_request.merged
        ↓
Notification
        ↓
Audience: group:developers
```

但是：

> **Event 当前不要变成 Herald 的强制核心依赖。**

Herald 的核心仍然是 Notification Delivery。

## 20. 推荐完整生命周期

最终 Herald 的完整生命周期应该是：

```text
                    Event
                      │
                      ▼
                     Rule
                      │
                      ▼
                Notification
                 /         \
                /           \
               ▼             ▼
          Audience         Template
               │
               ▼
          Recipients
               │
               ▼
           Endpoints
               │
               ▼
            Routing
               │
               ▼
         Delivery Tasks
               │
               ▼
             Queue
               │
               ▼
            Workers
               │
               ▼
           Providers
          /    |     \
         ▼     ▼      ▼
    Telegram Feishu  Email
```

## 21. API 设计

现有：

```http
POST /api/v1/notify
```

可以继续保留。

请求建议逐步演进为：

```json
{
  "type": "ci.build.failed",
  "level": "error",

  "channel": "ci",

  "audience": [
    "group:ops"
  ],

  "template": "ci.build.failed",

  "data": {
    "repository": "cuihairu/herald",
    "build_id": "123",
    "branch": "main"
  }
}
```

如果允许直接发送简单文本，也应该继续支持：

```json
{
  "title": "Build Failed",
  "body": "CI #123 failed",
  "channel": "ci",
  "audience": ["group:ops"]
}
```

这样：

```text
Template
```

是可选的，而不是强制。

## 22. API 返回

建议继续保留现在的：

```json
{
  "code": 0,
  "message": "accepted",
  "data": {
    "notification_id": "N001",
    "task_ids": [
      "T001",
      "T002"
    ]
  }
}
```

注意：

```text
accepted
```

表示：

> Herald 已经接受通知。

不代表：

> 所有 Provider 都已经成功送达。

## 23. ID 模型

至少区分：

```text
notification_id
delivery_id
```

例如：

```text
N001

T001 → Telegram
T002 → Feishu
T003 → Email
```

以后还可以支持：

```text
attempt_id
event_id
```

但不要现在增加不必要的 ID。

## 24. Idempotency

建议增加：

```text
idempotency_key
```

原因是：

```text
GitHub webhook
CI callback
Agent
网络重试
```

都可能导致同一个 Notification 被提交多次。

例如：

```json
{
  "idempotency_key": "github:repo:pr:123:merged"
}
```

Herald 可以保证：

```text
同一个业务事件
不会重复创建多个 Delivery
```

第一阶段可以先完成模型和接口设计，再决定持久化实现。

## 25. Logs 与 Delivery State 分离

不要把日志直接当状态。

Delivery 保存：

```text
status
attempts
last_error
next_retry_at
created_at
updated_at
```

日志保存历史：

```text
created
queued
delivering
failed
retrying
delivered
```

例如：

```text
T001

10:00 created
10:00 queued
10:01 delivering
10:01 failed
10:02 retrying
10:03 delivering
10:03 delivered
```

这样后续做：

```text
Dashboard
Audit
Debug
Metrics
```

都比较自然。

## 26. Provider Capability

未来不同 Provider 能力不同：

```text
Telegram
  text
  markdown
  image

Feishu
  text
  markdown
  card

Email
  text
  html
  attachment
```

建议 Provider 暴露 Capability。

但当前只建立接口，不要过度设计。

## 27. Configuration

建议逐步形成：

```yaml
server:

queue:

providers:

channels:

audiences:

recipients:

templates:

routes:

retry:
```

例如：

```yaml
channels:
  ci:
    providers:
      - telegram
      - feishu

audiences:
  ops:
    recipients:
      - alice
      - bob

recipients:
  alice:
    endpoints:
      - provider: telegram
        target: "123456"

      - provider: email
        target: "alice@example.com"
```

配置关系如下：

```text
Channel
    ↓
Provider

Audience
    ↓
Recipient
    ↓
Endpoint
```

## 28. Getting Started 文档改造

Getting Started 需要明确解释：

## What is Herald?

一句话说明：

> Herald receives notifications from applications and reliably delivers them to one or more notification providers.

## Core concepts

增加：

```text
Notification
Audience
Recipient
Endpoint
Channel
Provider
Delivery
```

## Architecture

增加：

```text
Client
 ↓
Notification API
 ↓
Routing
 ↓
Delivery
 ↓
Queue
 ↓
Worker
 ↓
Provider
```

## First notification

继续保留当前 `log` Provider。

这个设计很好，不要删除。

用户可以：

```text
启动 Herald
 ↓
发送 Notification
 ↓
log Provider
 ↓
立即看到结果
```

然后再配置：

```text
Telegram
Feishu
Email
```

## 29. 文档结构建议

建议：

```text
docs/
├── guide/
│   ├── getting-started
│   ├── configuration
│   └── providers
│
├── concepts/
│   ├── notification
│   ├── audience
│   ├── recipient
│   ├── endpoint
│   ├── channel
│   ├── delivery
│   └── routing
│
├── architecture/
│   ├── overview
│   ├── queue
│   ├── worker
│   ├── retry
│   └── provider
│
└── api/
    └── notification
```

不要求一次全部完成，可以按照实现进度逐步增加。

## 30. Observability

建议增加基础指标：

```text
notifications_total
deliveries_total
deliveries_success_total
deliveries_failed_total

delivery_latency
queue_depth
retry_total

provider_requests_total
provider_errors_total
provider_latency
```

特别关注：

```text
Queue Depth
Delivery Success Rate
Provider Latency
Retry Count
```

## 31. 测试要求

必须覆盖：

## Notification

```text
创建 Notification
参数验证
```

## Audience

```text
group → recipients
user → recipient
recipient → endpoints
```

## Routing

```text
Notification
+
Audience
+
Channel
→
Delivery Tasks
```

## Multi Provider

```text
Telegram success
Feishu failure

Telegram 不重复
Feishu 可以 retry
```

## Retry

```text
TemporaryError
→ retry

PermanentError
→ dead
```

## State

```text
queued
→ delivering
→ delivered
```

以及：

```text
failed
→ retrying
→ delivered
```

## Idempotency

```text
相同 idempotency_key
→ 不重复创建 Delivery
```

## 32. 当前阶段明确不做

虽然架构需要为未来留下空间，但当前实现不要直接引入：

```text
❌ Kafka
❌ Redis
❌ RabbitMQ
❌ NATS
❌ Kubernetes
❌ Complex Event Bus
❌ Complex Rule Engine
❌ IAM
❌ 完整用户系统
❌ Multi-tenant
❌ 大型 Dashboard
```

尤其不要因为 Audience 而自行引入复杂的：

```text
LDAP
OAuth
User Directory
Identity Provider
```

Audience 当前只需要一个简单、可扩展的本地配置模型。

## 33. 实施顺序

Code Agent 必须按照以下顺序执行。

## Phase 0：代码审计

**禁止直接修改代码。**

先分析当前：

```text
Notification
Provider
Channel
Router
Queue
Worker
Logs
API
Configuration
Tests
```

输出：

```text
当前实现
↓
目标模型
↓
差异
↓
改动文件
↓
兼容性风险
```

## Phase 1：领域模型

优先确定：

```text
Notification
Audience
Recipient
Endpoint
Channel
Delivery
Provider
```

确认对象关系：

```text
Notification
   │
   ├── Audience
   │      └── Recipient
   │             └── Endpoint
   │
   ├── Channel
   │
   └── Template
```

## Phase 2：Routing

实现：

```text
Notification
     ↓
Audience resolution
     ↓
Channel routing
     ↓
Delivery Task
```

## Phase 3：Delivery

实现：

```text
Delivery Task
status
attempts
last_error
next_retry_at
```

并明确状态机。

## Phase 4：Queue / Worker

保持：

```text
MemoryQueue
```

实现：

```text
enqueue
dequeue
ack
worker
retry
```

## Phase 5：Provider

整理 Provider interface。

确保：

```text
Provider
    ↓
Deliver()
```

不包含：

```text
Routing
Audience
Queue
Retry Policy
Business Logic
```

## Phase 6：API

更新：

```text
POST /api/v1/notify
```

支持：

```text
type
level
channel
audience
template
data
idempotency_key
```

同时保持简单文本通知的兼容能力。

> ✅ 已落地（2026-10-04）：`channel` / `audience` / `data` / `idempotency_key` 四个可选字段
> 全部兼容上线（护栏 1"新增字段全部可选"）；`audience` 逐项走既有展开链（`group:` /
> `user:` / 裸渠道名）；`data` 与 `params` 合并、`params` 优先；幂等为进程生命周期内的
> 内存表（约 1000 条 FIFO，仅记录成功结果，命中返回首次结果的 replay、不产生新投递）。
> 落地细节与文档同步见[现状审计](/design-audience-audit#五执行记录)。

## Phase 7：Configuration

完善：

```text
providers
channels
audiences
recipients
templates
routes
queue
retry
```

## Phase 8：测试

补充：

```text
Unit Test
Integration Test
Provider Test
Routing Test
Retry Test
Idempotency Test
```

## Phase 9：Documentation

同步修改：

```text
Getting Started
Architecture
Configuration
Provider
Audience
Delivery
API
```

确保文档与代码完全一致。

## 34. Code Agent 工作要求

执行时必须遵守：

### ① 先分析，再修改

不要看到任务就直接重构。

### ② 尽量保持 API 兼容

已有：

```text
/api/v1/notify
```

以及现有 Provider 配置，应尽可能保持兼容。

如果必须 Breaking Change：

```text
明确说明原因
给出迁移方式
更新文档
增加测试
```

### ③ 不要过度抽象

不要为了：

```text
未来可能存在的需求
```

创建大量 interface。

只抽象真正存在的领域边界。

### ④ 不引入无必要的外部依赖

尤其：

```text
Redis
Kafka
NATS
RabbitMQ
```

本次不需要。

### ⑤ 不删除当前 Log Provider

Log Provider 的用途：

```text
开发
测试
Getting Started
```

基础 Provider。

必须保留。

## 35. 最终架构验收

改造完成后，必须能够清晰回答以下问题：

### "通知什么？"

```text
Notification
```

### "通知谁？"

```text
Audience
```

### "具体是谁？"

```text
Recipient
```

### "这个人通过什么地址接收？"

```text
Endpoint
```

### "属于什么业务通知通道？"

```text
Channel
```

### "应该产生哪些投递？"

```text
Routing
```

### "具体一次投递是什么？"

```text
Delivery Task
```

### "什么时候执行？"

```text
Queue
```

### "谁执行？"

```text
Worker
```

### "通过什么技术发送？"

```text
Provider
```

### "失败怎么办？"

```text
Retry Policy
```

## 36. 最终核心模型

最终希望 Herald 形成下面这个稳定模型：

```text
                         ┌──────────────┐
                         │    Event     │
                         │   optional   │
                         └──────┬───────┘
                                │
                                ▼
                         ┌──────────────┐
                         │ Notification │
                         └──────┬───────┘
                                │
              ┌─────────────────┼─────────────────┐
              │                 │                 │
              ▼                 ▼                 ▼
         Audience            Channel          Template
              │
              ▼
         Recipients
              │
              ▼
          Endpoints
              │
              └──────────────┐
                             ▼
                          Routing
                             │
                             ▼
                     Delivery Tasks
                             │
                             ▼
                           Queue
                             │
                             ▼
                          Workers
                             │
                             ▼
                         Provider
                    ┌────────┼────────┐
                    ▼        ▼        ▼
                 Telegram  Feishu   Email
```

## 37. 最终判断标准

这次改造的验收不看"代码增加了多少"，看 Herald 是否真正做到：

> **业务系统只需要描述"发生了什么、通知什么、通知谁"，而不需要关心消息最终通过什么渠道、由哪个 Worker、什么时候以及如何重试送达。**

最终业务侧应该尽可能接近：

```json
{
  "type": "ci.build.failed",
  "level": "error",
  "channel": "ci",
  "audience": [
    "group:ops"
  ],
  "template": "ci.build.failed",
  "data": {
    "repository": "cuihairu/herald",
    "build_id": "123"
  }
}
```

然后 Herald 自己完成：

```text
Audience Resolution
       ↓
Routing
       ↓
Delivery Creation
       ↓
Queue
       ↓
Worker
       ↓
Provider
       ↓
Retry
       ↓
Delivery Status
```

这就是本次 Herald 架构改造最终应该达到的状态。