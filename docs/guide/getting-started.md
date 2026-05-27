# 快速开始

## 安装

```bash
# 从源码构建
git clone https://github.com/cuihairu/herald
cd herald
make build

# 或下载预编译版本
wget https://github.com/cuihairu/herald/releases/latest/download/heraldd-linux-amd64
```

## 配置

创建配置文件 `config.yaml`：

```yaml
server:
  addr: ":8080"

providers:
  telegram:
    type: bot-api
    token: "${TELEGRAM_BOT_TOKEN}"
    chat_id: "@my-channel"

  email:
    type: builtin
    config:
      host: "smtp.example.com"
      port: 587
      username: "${EMAIL_USER}"
      password: "${EMAIL_PASS}"
      from: "notify@example.com"

routes:
  error:
    - telegram
  warning:
    - telegram
    - email

retry:
  max: 3
  backoff: exponential
  initial_delay: 1s
  max_delay: 1m

dedup:
  enabled: true
  window: 5m
```

## 启动服务

```bash
heraldd --config config.yaml
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

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "server.alert",
    "level": "error",
    "template": "server_alert",
    "params": {
      "host": "node-17",
      "status": "offline"
    },
    "channels": ["telegram", "email"]
  }'
```

### 响应

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

## 查看状态

```bash
# 服务状态
curl http://localhost:8080/api/v1/status

# Provider 列表
curl http://localhost:8080/api/v1/providers

# 投递日志
curl http://localhost:8080/api/v1/logs?limit=10
```
