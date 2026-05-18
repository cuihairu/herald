# 快速开始

## 安装

```bash
# 从源码构建
git clone https://github.com/cuihaitao/herald
cd herald
make build

# 或下载预编译版本
wget https://github.com/cuihaitao/herald/releases/latest/download/heraldd-linux-amd64
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

routes:
  error:
    - telegram
  warning:
    - telegram
```

## 启动服务

```bash
heraldd --config config.yaml
```

## 发送通知

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Node Offline",
    "body": "node-17 is offline",
    "level": "error"
  }'
```

## 发送事件

```bash
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "node.offline",
    "labels": {
      "region": "shanghai",
      "node": "node-17"
    }
  }'
```
