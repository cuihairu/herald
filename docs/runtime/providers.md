# Provider 分类

## Provider 类别

| 类型 | 说明 | 示例 |
|------|------|------|
| Bot API | 官方 Bot API | Telegram / Discord |
| Webhook | Webhook 接口 | 飞书 / 企业微信 / 钉钉 / Slack |
| SMTP | 邮件协议 | Email |
| SMS | 短信服务 | 阿里云 / 腾讯云 / 网易云 |
| Third-party Push | 第三方推送 | Server酱 / PushPlus |
| Worker Proxy | Worker 代理 | 微信公众号 |

## Builtin Providers

| Provider | 类型 | 描述 |
|----------|------|------|
| Log | 内置 | 日志输出，用于测试 |
| Telegram | Bot API | Telegram Bot API |
| Discord | Webhook/Bot | Discord Webhook 或 Bot API |
| Feishu | Webhook | 飞书机器人 |
| WeCom | Webhook | 企业微信机器人 |
| DingTalk | Webhook | 钉钉机器人 |
| Slack | Webhook | Slack Webhook |
| Email | SMTP | 邮件发送 |
| Webhook | 通用 | 通用 HTTP Webhook |
| AliyunSMS | SMS | 阿里云短信 |
| TencentSMS | SMS | 腾讯云短信 |
| NetEaseSMS | SMS | 网易云短信 |
| WeChat | Third-party | 微信个人推送（Server酱等） |

## Worker Providers

Worker Provider 是一种代理类型，通过 WebSocket 将任务分发给远程 Worker。

| Provider | 描述 | 平台 |
|----------|------|------|
| WeChatMP | 微信公众号 | Worker |
| WeChat | 微信个人推送（复杂场景） | Worker |

## 配置格式

### Builtin Provider

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"
```

### Worker Provider

```yaml
providers:
  wechatmp:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"
```

## Provider 工厂

每个 Provider 都有一个对应的工厂类：

```go
type Factory interface {
    Create(config map[string]interface{}) (Provider, error)
    Name() string
}
```

工厂注册：

```go
manager.RegisterFactory(&telegram.Factory{})
manager.RegisterFactory(&email.Factory{})
manager.RegisterFactory(&worker.Factory{})
```
