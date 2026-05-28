# 配置

## 配置文件

Herald 使用 YAML 配置文件（`config.yaml`）。

## 完整配置示例

```yaml
# 服务配置
server:
  addr: ":8080"
  timeout: 30s

# Provider 配置
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

  # 飞书
  feishu:
    type: feishu
    enabled: false
    config:
      webhook_url: "${FEISHU_WEBHOOK_URL}"

  # 企业微信
  wecom:
    type: wecom
    enabled: false
    config:
      webhook_url: "${WECOM_WEBHOOK_URL}"

  # 钉钉
  dingtalk:
    type: dingtalk
    enabled: false
    config:
      access_token: "${DINGTALK_ACCESS_TOKEN}"
      secret: "${DINGTALK_SECRET}"

  # Slack
  slack:
    type: slack
    enabled: false
    config:
      webhook_url: "${SLACK_WEBHOOK_URL}"

  # Discord
  discord:
    type: discord
    enabled: false
    config:
      webhook_url: "${DISCORD_WEBHOOK_URL}"
      # 或使用 bot API
      bot_token: "${DISCORD_BOT_TOKEN}"
      channel_id: "${DISCORD_CHANNEL_ID}"

  # 邮件
  email:
    type: email
    enabled: false
    config:
      host: "smtp.gmail.com"
      port: 587
      username: "${EMAIL_USERNAME}"
      password: "${EMAIL_PASSWORD}"
      from: "${EMAIL_FROM}"
      from_name: "Herald"

  # Webhook
  webhook:
    type: webhook
    enabled: false
    config:
      url: "${WEBHOOK_URL}"
      method: "POST"

  # 阿里云短信
  aliyunsms:
    type: aliyunsms
    enabled: false
    config:
      access_key_id: "${ALIYUN_ACCESS_KEY_ID}"
      access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
      sign_name: "${ALIYUN_SMS_SIGN_NAME}"
      region: "cn-hangzhou"

  # 腾讯云短信
  tencentsms:
    type: tencentsms
    enabled: false
    config:
      secret_id: "${TENCENT_SECRET_ID}"
      secret_key: "${TENCENT_SECRET_KEY}"
      app_id: "${TENCENT_SMS_APP_ID}"
      region: "ap-guangzhou"

  # 网易云信短信
  neteasesms:
    type: neteasesms
    enabled: false
    config:
      app_key: "${NETEASE_APP_KEY}"
      app_secret: "${NETEASE_APP_SECRET}"

  # 微信个人推送（Server酱）
  wechat:
    type: wechat
    enabled: false
    config:
      sendkey: "${WECHAT_SENDKEY}"

  # 微信公众号（Worker）
  wechatmp:
    type: worker
    enabled: false
    config:
      target: "wechat-worker-01"

# 路由配置
routes:
  error:
    - log
  warning:
    - log
  info:
    - log

# 队列配置
queue:
  type: memory
  size: 10000
  timeout: 5s

# 重试配置
retry:
  max: 3
  backoff: exponential
  initial_delay: 1s
  max_delay: 1m

# 去重配置
dedup:
  enabled: true
  window: 5m

# WebSocket 配置（用于 Worker 连接）
websocket:
  addr: ":8081"
  read_timeout: 60s
  write_timeout: 60s
  ping_interval: 20s

# 模板定义
templates:
  # 服务器告警模板
  server_alert:
    name: "服务器告警"
    title: "【告警】{{.Level}} - {{.Service}}"
    level: "error"
    fields:
      - label: "服务器"
        value: "{{.Server}}"
        type: "text"
      - label: "错误信息"
        value: "{{.Error}}"
        type: "text"
      - label: "时间"
        value: "{{.Timestamp}}"
        type: "text"

  # 部署通知模板
  deploy_notify:
    name: "部署通知"
    title: "部署完成: {{.Env}} 环境"
    level: "info"
    fields:
      - label: "环境"
        value: "{{.Env}}"
        type: "text"
      - label: "版本"
        value: "{{.Version}}"
        type: "text"
      - label: "分支"
        value: "{{.Branch}}"
        type: "text"
      - label: "耗时"
        value: "{{.Duration}}"
        type: "text"
```

## 环境变量

支持使用 `${VAR_NAME}` 引用环境变量：

```yaml
providers:
  telegram:
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
```

## 动态配置

### 启用/禁用 Provider

**通过 API：**

```bash
# 启用
curl -X POST http://localhost:8080/api/v1/providers/telegram/enable

# 禁用
curl -X POST http://localhost:8080/api/v1/providers/telegram/disable
```

**通过 Dashboard：**

在 Dashboard 的 Providers 页面中，点击每个 Provider 卡片的启用/禁用按钮。

### 配置优先级

1. API 调用（运行时修改）
2. 配置文件（启动时加载）
3. 默认配置

## 配置说明

### Provider 类型

| 类型 | 说明 | 配置示例 |
|------|------|---------|
| `log` | 日志输出 | `type: log` |
| `telegram` | Telegram Bot | `type: telegram` |
| `feishu` | 飞书机器人 | `type: feishu` |
| `wecom` | 企业微信机器人 | `type: wecom` |
| `dingtalk` | 钉钉机器人 | `type: dingtalk` |
| `slack` | Slack | `type: slack` |
| `discord` | Discord | `type: discord` |
| `email` | SMTP 邮件 | `type: email` |
| `webhook` | 通用 Webhook | `type: webhook` |
| `aliyunsms` | 阿里云短信 | `type: aliyunsms` |
| `tencentsms` | 腾讯云短信 | `type: tencentsms` |
| `neteasesms` | 网易云短信 | `type: neteasesms` |
| `wechat` | 微信个人推送 | `type: wechat` |
| `worker` | Worker 代理 | `type: worker` |

### 模板配置

详见 [模板系统](/guide/templates)。
