# Builtin Runtime

The Builtin Runtime runs directly inside the Core and suits simple HTTP API style Providers.

## Suitable Scenarios

Telegram Bot API, Discord Webhook, Feishu/WeCom bots, Email SMTP, generic Webhook, SMS (Aliyun, Tencent Cloud, NetEase Yunxin) — any channel that can go out with a single HTTP call fits.

## Key Features

Delivery is a single in-process function call, with no IPC and no extra process; adding a new channel takes just one `providers:` configuration block.

## Interface

```go
type Provider interface {
    Deliver(ctx context.Context, task *DeliveryTask) error
    Name() string
    Type() string
    Status() *ProviderStatus
}
```

## Implemented Providers

### Log Provider

A log Provider for testing and debugging.

**Configuration:**

```yaml
providers:
  log:
    type: log
    enabled: true
    config:
      name: "log"
```

### Telegram Provider

Sends messages via the Telegram Bot API.

**Configuration:**

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
      parse_mode: "markdown"  # Optional: markdown, html
```

**Environment variables:**

- `TELEGRAM_BOT_TOKEN` - Bot Token
- `TELEGRAM_CHAT_ID` - Chat ID or channel username

### Feishu Provider

Sends messages via Feishu Webhook.

**Configuration:**

```yaml
providers:
  feishu:
    type: feishu
    enabled: false
    config:
      webhook_url: "$FEISHU_WEBHOOK_URL"
      sign_secret: "$FEISHU_SIGN_SECRET"  # Optional
```

**Environment variables:**

- `FEISHU_WEBHOOK_URL` - Webhook URL
- `FEISHU_SIGN_SECRET` - Signing secret (optional)

### WeCom Provider

Sends messages via WeCom (WeChat Work) Webhook.

**Configuration:**

```yaml
providers:
  wecom:
    type: wecom
    enabled: false
    config:
      webhook_url: "$WECOM_WEBHOOK_URL"
      # or use key
      key: "$WECOM_KEY"
```

**Environment variables:**

- `WECOM_WEBHOOK_URL` - Full Webhook URL
- `WECOM_KEY` - Webhook key

### DingTalk Provider

Sends messages via DingTalk Webhook.

**Configuration:**

```yaml
providers:
  dingtalk:
    type: dingtalk
    enabled: false
    config:
      access_token: "$DINGTALK_ACCESS_TOKEN"
      secret: "$DINGTALK_SECRET"
```

### Slack Provider

Sends messages via Slack Webhook.

**Configuration:**

```yaml
providers:
  slack:
    type: slack
    enabled: false
    config:
      webhook_url: "$SLACK_WEBHOOK_URL"
```

### Discord Provider

Sends messages via Discord Webhook or Bot API.

**Configuration:**

```yaml
providers:
  discord:
    type: discord
    enabled: false
    config:
      webhook_url: "$DISCORD_WEBHOOK_URL"
      # or use the bot API
      bot_token: "$DISCORD_BOT_TOKEN"
      channel_id: "$DISCORD_CHANNEL_ID"
```

### Email Provider

Sends email via SMTP.

**Configuration:**

```yaml
providers:
  email:
    type: email
    enabled: false
    config:
      host: "smtp.gmail.com"
      port: 587
      username: "$EMAIL_USERNAME"
      password: "$EMAIL_PASSWORD"
      from: "$EMAIL_FROM"
      from_name: "Herald"
```

**Usage example:**

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

**Specifying recipients:**

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

A generic Webhook Provider that can send POST requests to any URL.

**Configuration:**

```yaml
providers:
  webhook:
    type: webhook
    enabled: false
    config:
      url: "$WEBHOOK_URL"
      method: "POST"
      headers:
        Authorization: "Bearer your_webhook_token"
```

**Payload format:**

```json
{
  "id": "task-id",
  "provider": "webhook",
  "level": "error",
  "targets": ["group:ops"],
  "timestamp": "2026-05-18T12:00:00Z",
  "title": "Test Alert",
  "body": "This is a test",
  "raw": {"custom": "field"}
}
```

### SMS Providers

#### Aliyun SMS

```yaml
providers:
  aliyunsms:
    type: aliyunsms
    enabled: false
    config:
      access_key_id: "$ALIYUN_ACCESS_KEY_ID"
      access_key_secret: "$ALIYUN_ACCESS_KEY_SECRET"
      sign_name: "$ALIYUN_SMS_SIGN_NAME"
      region: "cn-hangzhou"
```

#### Tencent Cloud SMS

```yaml
providers:
  tencentsms:
    type: tencentsms
    enabled: false
    config:
      secret_id: "$TENCENT_SECRET_ID"
      secret_key: "$TENCENT_SECRET_KEY"
      app_id: "$TENCENT_SMS_APP_ID"
      region: "ap-guangzhou"
```

#### NetEase Yunxin SMS

```yaml
providers:
  neteasesms:
    type: neteasesms
    enabled: false
    config:
      app_key: "$NETEASE_APP_KEY"
      app_secret: "$NETEASE_APP_SECRET"
```

### WeChat Provider

WeChat personal push with a choice of three backends: ServerChan (default) / PushPlus / WxPusher, selected via the `service` key (see the [Provider Handbook](/providers/wechat)).

```yaml
providers:
  wechat:
    type: wechat
    enabled: false
    config:
      send_key: "$WECHAT_SERVERCHAN_SEND_KEY"
```

### Other Builtin Providers

See the [Provider Handbook](/providers/overview) for the full configuration of the four mobile push providers and the Official Account; only the type names are listed here:

| type | Channel |
|------|---------|
| `fcm` | Firebase Cloud Messaging |
| `apns` | Apple Push Notification service |
| `jpush` | JPush (Aurora Push) |
| `getui` | Getui |
| `wechatmp` | WeChat Official Account (template messages / customer-service messages) |
| `worker` | Remote Worker proxy (not a real channel; forwards to a Worker process for execution) |

## Custom Builtin Provider

To create a custom Provider:

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
    // Implement the send logic
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

type Factory struct{}

func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
    return &Provider{name: "myprovider"}, nil
}

func (f *Factory) Name() string {
    return "myprovider"
}

func (f *Factory) Type() string {
    return "myprovider"
}
```

Register it in `providers/builtin/registry/registry.go`:

```go
import "github.com/cuihairu/herald/providers/builtin/myprovider"

func RegisterBuiltinProviders(manager *runtime.Manager) {
    // ...
    manager.RegisterFactory(&myprovider.Factory{})
}
```
