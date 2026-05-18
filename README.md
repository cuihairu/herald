# Herald

> Event-driven Delivery Infrastructure

Herald 是一个事件驱动的通知投递基础设施。

## 特性

- **HTTP First** - curl 友好，无 SDK 依赖
- **Runtime First** - 支持 Builtin 和 Worker 两种 Runtime
- **Event First** - 处理事件而非简单发送消息
- **Worker Model** - 支持复杂场景如 Hook/GUI/DLL
- **WebSocket** - 支持 Worker 实时连接
- **Dashboard** - Web 管理界面

## 支持的 Provider

| Provider        | 类型     | 状态    |
| --------------- | ------ | ----- |
| Log             | Builtin | ✅     |
| Telegram        | Builtin | ✅     |
| Feishu          | Builtin | ✅     |
| WeChat Work     | Builtin | ✅     |
| Email (SMTP)    | Builtin | ✅     |
| Generic Webhook | Builtin | ✅     |
| Discord         | Builtin | ✅     |
| Slack           | Builtin | ✅     |
| WeChat Hook     | Worker  | 计划中 |

## 快速开始

### Docker 部署（推荐）

```bash
# 复制环境变量
cp .env.example .env

# 编辑 .env 文件
vim .env

# 启动服务
make docker-up
```

### 本地运行

```bash
# 构建
make build

# 启动服务
make run

# 启动 Dashboard（另一个终端）
make dashboard-dev
```

### 访问 Dashboard

```bash
# Dashboard 启动后访问
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
    "level": "error"
  }'
```

### 发送事件

```bash
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "node.offline",
    "labels": {
      "level": "error",
      "node": "node-17"
    }
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

## 配置

### Discord

```yaml
providers:
  discord:
    type: builtin
    config:
      webhook_url: "${DISCORD_WEBHOOK_URL}"
      # 或使用 bot API
      bot_token: "${DISCORD_BOT_TOKEN}"
      channel_id: "${DISCORD_CHANNEL_ID}"
```

### Slack

```yaml
providers:
  slack:
    type: builtin
    config:
      webhook_url: "${SLACK_WEBHOOK_URL}"
```

### Telegram

```yaml
providers:
  telegram:
    type: builtin
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"
```

### 飞书

```yaml
providers:
  feishu:
    type: builtin
    config:
      webhook_url: "${FEISHU_WEBHOOK_URL}"
```

### 企业微信

```yaml
providers:
  wecom:
    type: builtin
    config:
      webhook_url: "${WECOM_WEBHOOK_URL}"
```

### Email

```yaml
providers:
  email:
    type: builtin
    config:
      host: "smtp.gmail.com"
      port: 587
      username: "${EMAIL_USERNAME}"
      password: "${EMAIL_PASSWORD}"
      from: "${EMAIL_FROM}"
```

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                         Frontend                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Dashboard  │  │   HTTP API   │  │   WebSocket  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                       Herald Core                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Event Queue  │  │    Router    │  │   Runtime    │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │    Retry     │  │    Dedup     │  │   WebSocket  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
   ┌────▼────┐          ┌────▼────┐          ┌────▼────┐
   │Telegram │          │ Discord │          │  Email  │
   └─────────┘          └─────────┘          └─────────┘
        │
   ┌────▼────┐
   │ Worker  │
   │ Runtime │
   └─────────┘
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
