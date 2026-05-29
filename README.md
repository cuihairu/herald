# Herald

> Event-driven Delivery Infrastructure

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/cuihairu/herald/blob/main/LICENSE)
[![codecov](https://codecov.io/gh/cuihairu/herald/graph/badge.svg)](https://codecov.io/gh/cuihairu/herald)

Herald 是一个事件驱动的通知投递基础设施。

## 特性

- **HTTP First** - curl 友好，REST API，无 SDK 依赖
- **Queue as Backbone** - Queue 是唯一的任务分发通道，支持 memory/redis
- **Unified Worker** - 统一 Worker 模型，local/remote 只区分部署方式
- **Template System** - 与渠道无关的模板系统，一次定义多渠道复用
- **Multi-channel** - 统一接口对接 15+ 通知渠道
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

```
                         ┌──────────────────────────┐
                         │     Herald Scheduler      │
                         │   (API + 路由 + 模板渲染)  │
                         └─────────┬────────────────┘
                                   │ Push
                            ┌──────▼──────┐
                            │    Queue     │  memory / redis
                            └──────┬──────┘
                                   │ Pop + Ack/Nack
                    ┌──────────────┼──────────────┐
                    ↓              ↓              ↓
              ┌──────────┐  ┌──────────┐  ┌──────────┐
              │ Worker   │  │ Worker   │  │ Worker   │
              │ mode:local│  │ mode:local│  │ mode:remote│
              │ goroutine │  │ goroutine │  │ 独立进程  │
              └──────────┘  └──────────┘  └──────────┘
```

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

**分布式**（redis 队列，调度器和 Worker 独立部署）：

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
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"

queue:
  type: memory
  workers: 0

routes:
  error: [telegram, email]
```

## 文档

完整文档请访问 [docs/](./docs/)

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

## 许可证

[Apache License 2.0](./LICENSE)
