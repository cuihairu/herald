# Provider 分类

## Provider 类别

| 类型 | 说明 | 示例 |
|------|------|------|
| Bot API | 官方 Bot API | Telegram / Discord |
| Webhook | Webhook 接口 | 飞书 / 企业微信 / 钉钉 / Slack |
| SMTP | 邮件协议 | Email |
| SMS | 短信服务 | 阿里云 / 腾讯云 / 网易云 |
| 移动推送 | APNs / FCM 等 | FCM / APNs / 极光 / 个推 |
| Third-party Push | 第三方推送 | Server酱 |
| Worker Proxy | Worker 代理 | type: worker 的转发渠道 |

## Builtin Providers

19 个 Factory 全部注册在 `providers/builtin/registry/registry.go`：

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
| WeChat | Third-party | 微信个人推送（Server酱） |
| WeChatMP | Third-party | 微信公众号（模板消息/客服消息） |
| FCM | 移动推送 | Firebase Cloud Messaging |
| APNs | 移动推送 | Apple Push Notification service |
| JPush | 移动推送 | 极光推送 |
| Getui | 移动推送 | 个推 |
| Worker | 代理 | 转发给远程 Worker 进程执行 |

## Worker Provider

Worker Provider 是一个代理类型（`type: worker`），本身不实现任何渠道：把任务按 `target`（等价 `name`）转发给匹配的远程 Worker，发送动作在 Worker 进程里完成。业务渠道（含微信公众号）跑在哪个 Runtime 由部署决定：内置渠道默认走 Builtin Runtime；同一个渠道也可以交由远程 Worker 承接，配置里换 `type: worker` 即可。

## 配置格式

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
      target: "wechat-worker-01"   # 也支持 name 键，二者等价
```

## Provider 工厂

每个 Provider 都有一个对应的工厂类：

```go
type Factory interface {
    Create(config map[string]interface{}) (Provider, error)
    Name() string
    Type() string
}
```

工厂注册（`providers/builtin/registry`）：

```go
manager.RegisterFactory(&telegram.Factory{})
manager.RegisterFactory(&email.Factory{})
manager.RegisterFactory(&worker.Factory{})
```
