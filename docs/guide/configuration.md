# 配置

## 配置文件

Herald 使用 YAML 配置文件。

## 完整配置示例

```yaml
# 服务配置
server:
  addr: ":8080"
  timeout: 30s

# Provider 配置
providers:
  telegram:
    type: bot-api
    token: "${TELEGRAM_BOT_TOKEN}"
    chat_id: "@my-channel"

  feishu:
    type: webhook
    webhook_url: "${FEISHU_WEBHOOK_URL}"

  wechat:
    type: worker
    platform: windows

# 路由配置
routes:
  error:
    - telegram
    - feishu
  warning:
    - feishu

# 队列配置
queue:
  type: memory  # 或 redis
  size: 10000

# 重试配置
retry:
  max: 3
  backoff: exponential
  initial_delay: 1s

# 去重配置
dedup:
  enabled: true
  window: 5m

# 模板配置
templates:
  server_alert:
    name: "服务器告警"
    title: "【{{.Level}}】{{.Service}} 服务异常"
    level: "error"
    fields:
      - label: "服务器"
        value: "{{.Server}}"
        type: "text"
      - label: "错误信息"
        value: "{{.Error}}"
        type: "text"
```

## 环境变量

支持使用 `${VAR_NAME}` 引用环境变量。

## 模板配置

详见 [模板系统](/guide/templates)。
