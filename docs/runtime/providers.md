# Provider Classification

## Provider Categories

| Type | Description | Examples |
|------|------|------|
| Bot API | Official Bot API | Telegram / Discord |
| Webhook | Webhook interface | Feishu / WeCom / DingTalk / Slack |
| SMTP | Email protocol | Email |
| SMS | SMS service | Aliyun / Tencent Cloud / NetEase Yunxin |
| Mobile push | APNs / FCM, etc. | FCM / APNs / JPush / Getui |
| Third-party push | Third-party push | ServerChan / PushPlus / WxPusher |
| Worker Proxy | Worker proxy | Forwarding channel with `type: worker` |

## Builtin Providers

All 19 factories are registered in `providers/builtin/registry/registry.go`:

| Provider | Type | Description |
|----------|------|------|
| Log | Builtin | Log output, for testing |
| Telegram | Bot API | Telegram Bot API |
| Discord | Webhook/Bot | Discord Webhook or Bot API |
| Feishu | Webhook | Feishu bot |
| WeCom | Webhook | WeCom bot |
| DingTalk | Webhook | DingTalk bot |
| Slack | Webhook | Slack Webhook |
| Email | SMTP | Email sending |
| Webhook | Generic | Generic HTTP Webhook |
| AliyunSMS | SMS | Aliyun SMS |
| TencentSMS | SMS | Tencent Cloud SMS |
| NetEaseSMS | SMS | NetEase Yunxin SMS |
| WeChat | Third-party | WeChat personal push (ServerChan / PushPlus / WxPusher) |
| WeChatMP | Third-party | WeChat Official Account (template messages / customer-service messages) |
| FCM | Mobile push | Firebase Cloud Messaging |
| APNs | Mobile push | Apple Push Notification service |
| JPush | Mobile push | JPush (Aurora Push) |
| Getui | Mobile push | Getui |
| Worker | Proxy | Forwards to a remote Worker process for execution |

## Worker Provider

The Worker Provider is a proxy type (`type: worker`) that implements no channel of its own: it forwards tasks by `target` (equivalent to `name`) to the matching remote Worker, and the send action happens inside the Worker process. Which Runtime a business channel (including the WeChat Official Account) runs on is a deployment decision: builtin channels use the Builtin Runtime by default; the same channel can also be handed to a remote Worker — just switch to `type: worker` in the configuration.

## Configuration Format

### Builtin Provider

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
```

### Worker Provider

```yaml
providers:
  ops-push:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"   # The name key is also supported; the two are equivalent
```

## Provider Factories

Every Provider has a corresponding factory class:

```go
type Factory interface {
    Create(config map[string]interface{}) (Provider, error)
    Name() string
    Type() string
}
```

Factory registration (`providers/builtin/registry`):

```go
manager.RegisterFactory(&telegram.Factory{})
manager.RegisterFactory(&email.Factory{})
manager.RegisterFactory(&worker.Factory{})
```
