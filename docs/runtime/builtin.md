# Builtin Runtime

Builtin Runtime 直接运行在 Core 内，适合简单的 HTTP API 类 Provider。

## 适合场景

- Telegram Bot API
- Discord Webhook
- 飞书机器人
- 企业微信机器人
- Email SMTP
- 通用 Webhook
- SMS（阿里云、腾讯云、网易云）

## 特点

- 轻量
- 无 IPC
- 高性能
- 配置简单

## 接口

```go
type Provider interface {
    Deliver(ctx context.Context, task *DeliveryTask) error
    Name() string
    Type() string
    Status() *ProviderStatus
    Close() error
}
```

## 已实现 Providers

### Log Provider

用于测试和调试的日志 Provider。

**配置：**

```yaml
providers:
  log:
    type: log
    enabled: true
    config:
      name: "log"
```

### Telegram Provider

通过 Telegram Bot API 发送消息。

**配置：**

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"
      parse_mode: "markdown"  # 可选: markdown, html
```

**环境变量：**

- `TELEGRAM_BOT_TOKEN` - Bot Token
- `TELEGRAM_CHAT_ID` - Chat ID 或 Channel 用户名

### Feishu Provider

通过飞书 Webhook 发送消息。

**配置：**

```yaml
providers:
  feishu:
    type: feishu
    enabled: false
    config:
      webhook_url: "${FEISHU_WEBHOOK_URL}"
      sign_secret: "${FEISHU_SIGN_SECRET}"  # 可选
```

**环境变量：**

- `FEISHU_WEBHOOK_URL` - Webhook URL
- `FEISHU_SIGN_SECRET` - 签名密钥（可选）

### WeCom Provider

通过企业微信 Webhook 发送消息。

**配置：**

```yaml
providers:
  wecom:
    type: wecom
    enabled: false
    config:
      webhook_url: "${WECOM_WEBHOOK_URL}"
      # 或使用 key
      key: "${WECOM_KEY}"
```

**环境变量：**

- `WECOM_WEBHOOK_URL` - 完整 Webhook URL
- `WECOM_KEY` - Webhook Key

### DingTalk Provider

通过钉钉 Webhook 发送消息。

**配置：**

```yaml
providers:
  dingtalk:
    type: dingtalk
    enabled: false
    config:
      access_token: "${DINGTALK_ACCESS_TOKEN}"
      secret: "${DINGTALK_SECRET}"
```

### Slack Provider

通过 Slack Webhook 发送消息。

**配置：**

```yaml
providers:
  slack:
    type: slack
    enabled: false
    config:
      webhook_url: "${SLACK_WEBHOOK_URL}"
```

### Discord Provider

通过 Discord Webhook 或 Bot API 发送消息。

**配置：**

```yaml
providers:
  discord:
    type: discord
    enabled: false
    config:
      webhook_url: "${DISCORD_WEBHOOK_URL}"
      # 或使用 bot API
      bot_token: "${DISCORD_BOT_TOKEN}"
      channel_id: "${DISCORD_CHANNEL_ID}"
```

### Email Provider

通过 SMTP 发送邮件。

**配置：**

```yaml
providers:
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
```

**使用示例：**

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "alert",
    "title": "Test Alert",
    "body": "This is a test email",
    "level": "error",
    "channels": ["email"]
  }'
```

**指定收件人：**

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "alert",
    "title": "Test Alert",
    "body": "This is a test email",
    "level": "error",
    "channels": ["email"],
    "recipients": {
      "email": ["user1@example.com", "user2@example.com"]
    }
  }'
```

### Webhook Provider

通用 Webhook Provider，可向任意 URL 发送 POST 请求。

**配置：**

```yaml
providers:
  webhook:
    type: webhook
    enabled: false
    config:
      url: "${WEBHOOK_URL}"
      method: "POST"
      headers:
        Authorization: "Bearer ${TOKEN}"
```

**发送格式：**

```json
{
  "id": "task-id",
  "provider": "webhook",
  "title": "Test Alert",
  "body": "This is a test",
  "level": "error",
  "timestamp": "2026-05-18T12:00:00Z",
  "data": {}
}
```

### SMS Providers

#### 阿里云短信

```yaml
providers:
  aliyunsms:
    type: aliyunsms
    enabled: false
    config:
      access_key_id: "${ALIYUN_ACCESS_KEY_ID}"
      access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
      sign_name: "${ALIYUN_SMS_SIGN_NAME}"
      region: "cn-hangzhou"
```

#### 腾讯云短信

```yaml
providers:
  tencentsms:
    type: tencentsms
    enabled: false
    config:
      secret_id: "${TENCENT_SECRET_ID}"
      secret_key: "${TENCENT_SECRET_KEY}"
      app_id: "${TENCENT_SMS_APP_ID}"
      region: "ap-guangzhou"
```

#### 网易云信短信

```yaml
providers:
  neteasesms:
    type: neteasesms
    enabled: false
    config:
      app_key: "${NETEASE_APP_KEY}"
      app_secret: "${NETEASE_APP_SECRET}"
```

### WeChat Provider

微信个人推送（Server酱）。

```yaml
providers:
  wechat:
    type: wechat
    enabled: false
    config:
      sendkey: "${WECHAT_SENDKEY}"
```

## 自定义 Builtin Provider

创建自定义 Provider：

```go
package myprovider

import (
    "context"
    "github.com/cuihairu/herald/core"
)

type Provider struct {
    name string
}

func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
    // 实现发送逻辑
    return nil
}

func (p *Provider) Name() string {
    return p.name
}

func (p *Provider) Type() string {
    return "myprovider"
}

func (p *Provider) Status() *core.ProviderStatus {
    return &core.ProviderStatus{
        Name:   p.name,
        Type:   "myprovider",
        Status: "available",
    }
}

func (p *Provider) Close() error {
    return nil
}

type Factory struct{}

func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
    return &Provider{name: "myprovider"}, nil
}

func (f *Factory) Name() string {
    return "myprovider"
}
```

在 `providers/builtin/registry/registry.go` 中注册：

```go
import "github.com/cuihairu/herald/providers/builtin/myprovider"

func RegisterBuiltinProviders(manager *runtime.Manager) {
    // ...
    manager.RegisterFactory(&myprovider.Factory{})
}
```
