# 部署

## 运行模式

Herald 使用统一二进制 `heraldd`，通过子命令区分运行模式：

```bash
heraldd serve    # 调度器模式（API + Queue + 本地 Worker）
heraldd worker   # 远程 Worker 模式（从共享 Queue 消费）
```

## 单机部署

单机模式使用 `memory` 队列，所有 Worker 在同一进程内运行。

```bash
# 构建
make build

# 启动
./bin/heraldd serve --config config.yaml
```

配置示例：

```yaml
server:
  addr: ":8080"
  timeout: 30s

queue:
  type: memory
  workers: 0    # 自动：CPU核心数*2+1

providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"
```

## 分布式部署

分布式模式使用 `redis` 队列，调度器和 Worker 可以独立部署。

### 调度器节点

```bash
./bin/heraldd serve --config scheduler.yaml
```

配置示例（scheduler.yaml）：

```yaml
server:
  addr: ":8080"
  timeout: 30s

queue:
  type: redis
  workers: 4
  redis:
    addr: "redis:6379"
    stream: "herald:tasks"
    group: "herald-workers"

websocket:
  addr: ":8081"

providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"
```

### Worker 节点

```bash
./bin/heraldd worker --config worker.yaml
```

配置示例（worker.yaml）：

```yaml
queue:
  type: redis
  workers: 0
  redis:
    addr: "redis:6379"
    stream: "herald:tasks"
    group: "herald-workers"

websocket:
  addr: "scheduler:8081"

providers:
  wechatmp:
    type: builtin
    enabled: true
    config:
      app_id: "${WECHAT_APP_ID}"
      app_secret: "${WECHAT_APP_SECRET}"
```

## Docker 部署

### 使用 Docker Compose

1. 复制环境变量模板：

```bash
cp .env.example .env
```

2. 编辑 `.env` 文件，填入你的配置。

3. 启动服务：

```bash
docker-compose up -d
```

### 手动构建 Docker 镜像

```bash
docker build -t herald:latest .
```

调度器：

```bash
docker run -d \
  -p 8080:8080 -p 8081:8081 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -e TELEGRAM_BOT_TOKEN=your_token \
  --name herald \
  herald:latest serve --config config.yaml
```

远程 Worker：

```bash
docker run -d \
  -v $(pwd)/worker.yaml:/app/worker.yaml:ro \
  --name herald-worker \
  herald:latest worker --config worker.yaml
```

## 二进制部署

### 构建

```bash
make build
```

### 运行调度器

```bash
./bin/heraldd serve --config config.yaml
```

### 运行远程 Worker

```bash
./bin/heraldd worker --config worker.yaml
```

## 系统服务 (systemd)

### 调度器服务

创建 `/etc/systemd/system/herald.service`：

```ini
[Unit]
Description=Herald Notification Scheduler
After=network.target

[Service]
Type=simple
User=herald
WorkingDirectory=/opt/herald
ExecStart=/opt/herald/bin/heraldd serve --config /etc/herald/config.yaml
Restart=always
RestartSec=5

EnvironmentFile=/etc/herald/herald.conf

[Install]
WantedBy=multi-user.target
```

### Worker 服务

创建 `/etc/systemd/system/herald-worker.service`：

```ini
[Unit]
Description=Herald Remote Worker
After=network.target

[Service]
Type=simple
User=herald
WorkingDirectory=/opt/herald
ExecStart=/opt/herald/bin/heraldd worker --config /etc/herald/worker.yaml
Restart=always
RestartSec=5

EnvironmentFile=/etc/herald/worker.conf

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable herald herald-worker
sudo systemctl start herald herald-worker
```

## 健康检查

```bash
curl http://localhost:8080/api/v1/status
```

返回示例：

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
        "status": "available"
      }
    ]
  }
}
```
