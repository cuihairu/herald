# Herald

> Event-driven Delivery Infrastructure

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/cuihairu/herald/blob/main/LICENSE)
[![codecov](https://codecov.io/gh/cuihairu/herald/graph/badge.svg)](https://codecov.io/gh/cuihairu/herald)

Herald 是一个事件驱动的通知投递基础设施。

## 特性

- **HTTP First** - curl 友好，无 SDK 依赖
- **API Compatible** - 支持 `/api/v1/notify` 和 `/api/v1/events`
- **Runtime First** - 支持 Builtin 和 Worker 两种 Runtime
- **Event First** - 处理事件而非简单发送消息
- **Template System** - 与渠道无关的模板系统，一次定义多渠道复用
- **Worker Model** - 支持复杂场景如 Hook/GUI/DLL
- **WebSocket** - 支持 Worker 实时连接
- **Dashboard** - Web 管理界面
- **Config First** - 通过配置文件加载 Provider、路由和模板

## 支持的 Provider

| Provider             | 类型     | 状态    |
| -------------------- | ------ | ----- |
| Log                  | Builtin | ✅     |
| Telegram             | Builtin | ✅     |
| Feishu               | Builtin | ✅     |
| WeChat Work          | Builtin | ✅     |
| Email (SMTP)         | Builtin | ✅     |
| Generic Webhook      | Builtin | ✅     |
| Discord              | Builtin | ✅     |
| Slack                | Builtin | ✅     |
| DingTalk             | Builtin | ✅     |
| AliyunSMS            | Builtin | ✅     |
| TencentSMS           | Builtin | ✅     |
| NetEaseSMS           | Builtin | ✅     |
| WeChat Push          | Builtin | ✅     |
| WeChat Official (MP) | Builtin | ✅     |
| Worker               | Proxy  | ✅     |

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
    "type": "alert",
    "title": "Node Offline",
    "body": "node-17 is offline",
    "level": "error",
    "channels": ["telegram", "email"]
  }'
```

### 发送事件

```bash
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "service.down",
    "labels": {
      "level": "error",
      "title": "服务宕机",
      "message": "order-service 不可用"
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

### 使用模板发送消息

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "alert",
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

### 查看模板列表

```bash
curl http://localhost:8080/api/v1/templates
```

## 配置

Herald 使用 `config.yaml` 启动，Provider 类型应与内置工厂名一致，例如 `log`、`telegram`、`feishu`、`email`、`aliyunsms`。

### Discord

```yaml
providers:
  discord:
    type: discord
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
    type: slack
    config:
      webhook_url: "${SLACK_WEBHOOK_URL}"
```

### Telegram

```yaml
providers:
  telegram:
    type: telegram
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"
```

### 飞书

```yaml
providers:
  feishu:
    type: feishu
    config:
      webhook_url: "${FEISHU_WEBHOOK_URL}"
```

### 企业微信

```yaml
providers:
  wecom:
    type: wecom
    config:
      webhook_url: "${WECOM_WEBHOOK_URL}"
```

### Email

```yaml
providers:
  email:
    type: email
    config:
      host: "smtp.gmail.com"
      port: 587
      username: "${EMAIL_USERNAME}"
      password: "${EMAIL_PASSWORD}"
      from: "${EMAIL_FROM}"
```

## 架构

```
External System -> Herald HTTP API -> Queue -> Dispatcher -> Runtime -> Provider
                      │
                      └-> /api/v1/events
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
