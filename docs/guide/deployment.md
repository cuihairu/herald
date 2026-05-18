# 部署

## Docker 部署

### 使用 Docker Compose

1. 复制环境变量模板：

```bash
cp .env.example .env
```

2. 编辑 `.env` 文件，填入你的配置：

```bash
# Telegram Bot
TELEGRAM_BOT_TOKEN=your_telegram_bot_token
TELEGRAM_CHAT_ID=your_telegram_chat_id

# Feishu
FEISHU_WEBHOOK_URL=your_feishu_webhook_url

# WeChat Work
WECOM_WEBHOOK_URL=your_wecom_webhook_url
```

3. 启动服务：

```bash
make docker-up
# 或
docker-compose up -d
```

4. 查看日志：

```bash
make docker-logs
# 或
docker-compose logs -f herald
```

5. 停止服务：

```bash
make docker-down
# 或
docker-compose down
```

### 手动构建 Docker 镜像

```bash
docker build -t herald:latest .
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -e TELEGRAM_BOT_TOKEN=your_token \
  -e TELEGRAM_CHAT_ID=your_chat_id \
  --name herald \
  herald:latest
```

## 二进制部署

### 构建

```bash
make build
```

二进制文件会输出到 `bin/heraldd`。

### 运行

```bash
./bin/heraldd --config config.yaml
```

### 配置文件

将 `config.yaml` 放在当前目录或指定路径：

```bash
./bin/heraldd --config /etc/herald/config.yaml
```

## 系统服务 (systemd)

创建 `/etc/systemd/system/herald.service`：

```ini
[Unit]
Description=Herald Notification Service
After=network.target

[Service]
Type=simple
User=herald
WorkingDirectory=/opt/herald
ExecStart=/opt/herald/bin/heraldd --config /etc/herald/config.yaml
Restart=always
RestartSec=5

EnvironmentFile=/etc/herald/herald.conf

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable herald
sudo systemctl start herald
sudo systemctl status herald
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
