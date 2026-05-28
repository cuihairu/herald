# 快速开始

## 安装

### Docker 部署（推荐）

```bash
# 克隆仓库
git clone https://github.com/cuihairu/herald
cd herald

# 复制环境变量模板
cp .env.example .env

# 编辑 .env 文件
vim .env

# 启动服务
make docker-up
```

### 本地运行

```bash
# 克隆仓库
git clone https://github.com/cuihairu/herald
cd herald

# 构建
make build

# 启动服务
make run

# 启动 Dashboard（另一个终端）
make dashboard-dev
```

## 配置

创建配置文件 `config.yaml`：

```yaml
server:
  addr: ":8080"
  timeout: 30s

providers:
  # 日志 Provider（默认启用）
  log:
    type: log
    enabled: true
    config:
      name: "log"

  # Telegram 机器人
  telegram:
    type: telegram
    enabled: true
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"

  # 邮件
  email:
    type: email
    enabled: true
    config:
      host: "smtp.example.com"
      port: 587
      username: "${EMAIL_USER}"
      password: "${EMAIL_PASS}"
      from: "notify@example.com"

routes:
  error:
    - telegram
    - email
  warning:
    - telegram
  info:
    - log

retry:
  max: 3
  backoff: exponential
  initial_delay: 1s
  max_delay: 1m

dedup:
  enabled: true
  window: 5m

websocket:
  addr: ":8081"
```

## 启动服务

```bash
heraldd --config config.yaml
```

## 访问 Dashboard

Dashboard 启动后访问：

```
http://localhost:3000
```

## 发送通知

### 使用直接内容

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "server.alert",
    "level": "error",
    "title": "Node Offline",
    "body": "node-17 is offline",
    "channels": ["telegram"]
  }'
```

### 使用模板

在 `config.yaml` 中定义模板：

```yaml
templates:
  server_alert:
    name: "服务器告警"
    title: "【{{.Level}}】{{.Service}} 服务异常"
    level: "error"
    fields:
      - label: "服务器"
        value: "{{.Server}}"
      - label: "错误信息"
        value: "{{.Error}}"
```

发送通知：

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "server.alert",
    "level": "error",
    "template": "server_alert",
    "params": {
      "Level": "CRITICAL",
      "Service": "order-service",
      "Server": "order-01",
      "Error": "CPU 使用率 95%"
    },
    "channels": ["telegram", "email"]
  }'
```

### 指定接收人

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "alert",
    "title": "Test Alert",
    "body": "This is a test",
    "level": "info",
    "channels": ["email"],
    "recipients": {
      "email": ["user1@example.com", "user2@example.com"]
    }
  }'
```

## 响应

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
      { "channel": "email", "error": "connection timeout" }
    ]
  }
}
```

## 查看状态

```bash
# 服务状态
curl http://localhost:8080/api/v1/status

# Provider 列表
curl http://localhost:8080/api/v1/providers

# 投递日志
curl http://localhost:8080/api/v1/logs?limit=10

# 模板列表
curl http://localhost:8080/api/v1/templates
```

## 下一步

- [配置](/guide/configuration) - 详细配置说明
- [模板系统](/guide/templates) - 模板使用指南
- [Providers](/providers/overview) - 支持的 Provider 列表
