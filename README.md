<div align="center">
  <img src="docs/public/logo.svg" width="120" alt="Herald logo" />

  # Herald

  **Lightweight Notification Orchestration & Delivery Infrastructure**

  [![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
  [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/cuihairu/herald/blob/main/LICENSE)
  [![codecov](https://codecov.io/gh/cuihairu/herald/graph/badge.svg)](https://codecov.io/gh/cuihairu/herald)
  [![Coverage Gate](https://img.shields.io/badge/coverage%20gate-100%25%20excl.%20ledger-brightgreen)](./docs/architecture/coverage.md)
</div>

Herald 是一个轻量的通知编排与投递基础设施：业务只描述「发生了什么、通知什么」，谁、什么时候、通过什么渠道、以什么强度收到、是否聚合、是否过滤、是否升级，由 Herald 编排收口；被通知者做主——谁在什么渠道、以什么频率收到什么品类，由受众自己的订阅关系与偏好决定，而不是由调用方写死。

## 两种使用形态

Herald 可以作为独立服务运行，也可以作为 Go 库嵌入你的进程，二者共享同一套核心管道（队列、路由、去重、模板、Provider）：

**CLI 网关** — 独立进程部署，REST API + Dashboard，适合作为组织级通知网关（见下方[快速开始](#快速开始)）：

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{"type": "deploy", "channels": ["feishu-ops"], "title": "v1.2.0 已发布"}'
```

**Go 库** — 一个 `App` 完成入队与投递，零配置即可运行（内存队列 + 本地 worker 池），适合把通知能力直接嵌进自己的服务：

```go
import (
    "github.com/cuihairu/herald"
    "github.com/cuihairu/herald/core"
)

app, _ := herald.New(nil) // 默认：内存队列、后台投递池、去重
defer app.Close()

app.Dispatch(ctx, &core.Notification{
    Type:     "deploy",
    Channels: []string{"log"},
    Content:  &core.DirectContent{Title: "v1.2.0 已发布"},
})
```

完整导出面与嵌入指南见 [docs/library-usage.md](docs/library-usage.md)，可运行示例见 [examples/quickstart](examples/quickstart/main.go)。

## 特性

- **统一订阅与投递中枢** - 受众是唯一跨系统身份（应用只持 `user:<id>`/`group:<id>`，不碰渠道凭据）；订阅/指派两类关系承载退订权与必达底线，取关即全停
- **联系面绑定** - 受众在运行时绑定可达渠道（TG chat、邮箱、公众号、RSS token），deep-link 一次性 token 换绑，失效即停投
- **偏好中心** - 品类×渠道×频率三元组（实时/每日/每周/不收），品类默认策略表打底；营销默认每周汇总，告警可降频不可静默
- **关系过滤** - 渠道×关系矩阵（订阅全开、必达拒拉式、营销限邮件/站内信）+ 发送前四步交集复核，交集为空不投并审计
- **投递编排** - 表达式规则引擎（shadow 先观察后生效，for/group_by/inhibit/silence/escalation）、Digest 时间窗聚合（每日/每周摘要、实时豁免、可选 redis 租约选主）、内容折叠去重与请求幂等
- **投递审计** - 关系/联系面变更流水、去重折叠明细（为什么这条没投）、关系类型与入口来源快照进任务与日志
- **HTTP First** - curl 友好，REST API，无 SDK 依赖
- **Queue as Backbone** - Queue 是唯一的任务分发通道，支持 memory/redis
- **Unified Worker** - 统一 Worker 模型，local/remote 只区分部署方式
- **Template System** - 与渠道无关的模板系统，一次定义多渠道复用
- **Multi-channel** - 统一接口对接 18 个内置通知渠道
- **Dashboard** - Web 管理界面
- **Config First** - 通过配置文件加载 Provider、路由和模板

## 支持的 Provider

| Provider             | 类型     | 状态 |
| -------------------- | -------- | ---- |
| Log                  | Builtin  | ✅   |
| Telegram             | Builtin  | ✅   |
| Feishu               | Builtin  | ✅   |
| WeChat Work          | Builtin  | ✅   |
| Email (SMTP)         | Builtin  | ✅   |
| Generic Webhook      | Builtin  | ✅   |
| Discord              | Builtin  | ✅   |
| Slack                | Builtin  | ✅   |
| DingTalk             | Builtin  | ✅   |
| AliyunSMS            | Builtin  | ✅   |
| TencentSMS           | Builtin  | ✅   |
| NetEaseSMS           | Builtin  | ✅   |
| FCM                  | Builtin  | ✅   |
| Apple Push (APNs)    | Builtin  | ✅   |
| JPush                | Builtin  | ✅   |
| Getui                | Builtin  | ✅   |
| WeChat Push          | Builtin  | ✅   |
| WeChat Official (MP) | Builtin  | ✅   |

## 快速开始

### Docker 部署（推荐）

```bash
# 复制环境变量
cp .env.example .env

# 编辑 .env 文件
vim .env

# 启动服务
docker-compose up -d herald
```

### 本地运行

```bash
# 构建
make build

# 启动调度器
./bin/heraldd serve --config config.yaml

# 启动远程 Worker（分布式部署时）
./bin/heraldd worker --config worker.yaml

# 启动 Dashboard（另一个终端）
make dashboard-dev
```

### 访问 Dashboard

```
http://localhost:3000
```

登录后侧边栏可用的页面：仪表盘、Providers、通知规则、通知群组、Workers、日志、发送消息。其中「通知规则」与「通知群组」对应规则引擎与命名受众的完整 CRUD，也是配置路由与改道的主入口——Dashboard 保存规则时**同时编译表达式**，非法表达式当场被拒，不会等到派发时才发现。

## 使用

### 发送通知

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Node Offline",
    "body": "node-17 is offline",
    "level": "error",
    "channels": ["telegram", "email"]
  }'
```

### 使用模板发送

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "template": "server_alert",
    "params": {
      "Level": "CRITICAL",
      "Service": "order-service",
      "Server": "order-01",
      "Error": "CPU 使用率 95%"
    },
    "channels": ["email", "telegram"]
  }'
```

### 查看状态

```bash
curl http://localhost:8080/api/v1/status
```

### 查看 Providers

```bash
curl http://localhost:8080/api/v1/providers
```

## 架构

```mermaid
graph TB
    Client["External Client<br/>curl / CI/CD / SDK"]

    subgraph Orchestration["编排层"]
        API["HTTP API<br/>(规则 + 模板渲染)"]
        Filter["受众层：关系过滤<br/>订阅/指派 × 渠道矩阵"]
        Fold["去重/频控 · 投递模式"]
    end

    Digest["Digest 时间窗聚合<br/>(旁路：实时豁免不入窗)"]

    Queue["Queue<br/>memory / redis"]

    subgraph Workers["Worker Pool"]
        W1["Worker<br/>mode: local"]
        W3["Worker<br/>mode: remote"]
    end

    subgraph Providers
        P1["Telegram / Email"]
        P3["WeChat MP / ..."]
    end

    Client -->|POST /api/v1/notify| API
    API --> Filter
    Filter --> Fold
    Fold -.->|"低频偏好事件入窗"| Digest
    Digest -.->|"窗口到点，摘要走同一管道"| Queue
    Fold -->|"展开成投递任务"| Queue
    Queue -->|Pop + Ack/Nack| W1
    Queue -->|Pop + Ack/Nack| W3
    W1 --> P1
    W3 --> P3
```

关系过滤的交集为空时不投，原因写入投递审计；命令与部署形态不变：

| 命令 | 模式 | 说明 |
|------|------|------|
| `heraldd serve` | 调度器 | API + Queue + local workers |
| `heraldd worker` | 远程 Worker | 从共享 Queue 消费，独立部署 |

### 部署模式

**单机**（memory 队列，所有 Worker 在同一进程内）：

```yaml
queue:
  type: memory
  workers: 0    # 自动：CPU核心数*2+1
```

**分布式**（redis 队列，调度器和 Worker 独立部署，需要 Redis 6.2+）：

```yaml
queue:
  type: redis
  workers: 4
  redis:
    addr: "localhost:6379"
    stream: "herald:tasks"
    group: "herald-workers"
```

## 配置

详见 [配置文档](./docs/guide/configuration.md)。

```yaml
server:
  addr: ":8080"
  timeout: 30s

providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"     # 环境变量只支持 $VAR 写法（${VAR} 不展开）
      chat_id: "$TELEGRAM_CHAT_ID"

queue:
  type: memory
  workers: 0

routes:
  error: [telegram, email]

# level_routes（可选）：type 路由未命中时按 level 兜底
# level_routes:
#   warning: [email]

# 渠道块（可选）：给渠道组合起名，channels/channel 字段写块名即展开成成员 provider
# channels:
#   ci:
#     providers: [telegram, email]

# 规则引擎（可选）：按表达式决定 放行/抑制/改道，未命中回落静态路由
# rules:
#   - id: prod-payment-failure
#     priority: 100
#     match: 'params.fail_rate > 0.05 && env == "prod"'
#     mode: active                # shadow 先观察，active 生效
#     route:
#       - channels: [feishu-oncall]
# rules_default_policy: allow     # 无 active 规则命中时：allow 保留静态路由，deny 扣下

# 通知群组（可选）：命名受众，任何渠道位都可写 "group:<id>"
# groups:
#   - id: ops-oncall
#     members:
#       - channel: feishu
#         recipients: ["@zhang"]
#       - channel: sms-duty

# Digest 时间窗聚合（可选）：低频偏好事件按 受众×品类 收成每日/每周摘要
# digest:
#   enabled: true
#   daily: "09:00"                 # 每日翻转时刻（默认 09:00）
#   weekly: "Mon 09:00"            # 每周翻转时刻（默认 Mon 09:00）
#   location: Asia/Shanghai        # 翻转时刻时区（默认 Asia/Shanghai）
#   redis_addr: "localhost:6379"   # 可选：多实例租约选主；不配则单机进程内定时器直跑
```

## 文档

完整文档请访问 [docs/](./docs/)

- [配置指南](./docs/guide/configuration.md)（规则引擎、通知群组、受众与联系面、Digest、队列、Provider 全量配置项）
- [Provider 文档](./docs/providers/overview.md)（各渠道单页：Telegram / 飞书 / 企业微信 / 钉钉 / Slack / Discord / 微信 / Email / Webhook / SMS / Log）
- [受众领域模型（总纲）](./docs/design-audience-model.md) · [受众订阅与投递中枢（关系详设）](./docs/design-audience-relations.md) · [概念边界与分层审计](./docs/design-audience-boundaries.md)
- [规则引擎决策层设计](./docs/design-rule-engine.md) · [通知群组设计](./docs/design-notification-groups.md)

## 开发

```bash
# 安装依赖
go mod download

# 运行测试
make test

# 构建
make build

# 运行文档服务
make docs-dev

# 运行 Dashboard
make dashboard-dev
```

## 底座来源

Herald 基于开源组件构建，核心外部依赖见 [go.mod](./go.mod)：

- [gorilla/websocket](https://github.com/gorilla/websocket) - 远程 Worker 的 WebSocket 传输
- [redis/go-redis](https://github.com/redis/go-redis) - Redis Stream 分布式队列客户端
- [expr-lang/expr](https://github.com/expr-lang/expr) - 规则引擎的表达式求值
- [gopkg.in/yaml.v3](https://gopkg.in/yaml.v3) - 配置文件解析
- Dashboard 基于 React + Ant Design（Vite 构建），文档站基于 VitePress

路由、规则引擎、队列抽象、模板与 18 个渠道 Provider 的业务代码在仓库内实现。

## 许可证

[Apache License 2.0](./LICENSE)
