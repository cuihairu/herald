# Builtin Runtime

Builtin Runtime 直接运行在 Core 内，适合简单的 HTTP API 类 Provider。

## 适合场景

- Telegram Bot API
- Discord Webhook
- 飞书机器人
- 企业微信机器人
- Email SMTP
- 通用 Webhook

## 特点

- 轻量
- 无 IPC
- 高性能
- 简单

## 接口

```go
type BuiltinProvider interface {
    Deliver(ctx context.Context, task *Task) error
    Name() string
    Type() string
    Status() *ProviderStatus
}
```

## 已实现 Providers

### Log Provider

用于测试和调试的日志 Provider。

**配置：**

```yaml
providers:
  log:
    type: builtin
    config:
      name: "log"
```

### Telegram Provider

通过 Telegram Bot API 发送消息。

**配置：**

```yaml
providers:
  telegram:
    type: builtin
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
    type: builtin
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
    type: builtin
    config:
      webhook_url: "${WECOM_WEBHOOK_URL}"
      # 或使用 key
      key: "${WECOM_KEY}"
```

**环境变量：**

- `WECOM_WEBHOOK_URL` - 完整 Webhook URL
- `WECOM_KEY` - Webhook Key

### Email Provider

通过 SMTP 发送邮件。

**配置：**

```yaml
providers:
  email:
    type: builtin
    config:
      host: "smtp.gmail.com"
      port: 587
      username: "${EMAIL_USERNAME}"
      password: "${EMAIL_PASSWORD}"
      from: "${EMAIL_FROM}"
      from_name: "Herald"  # 可选
```

**使用示例：**

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Test Alert",
    "body": "This is a test email",
    "level": "error"
  }'
```

**指定收件人：**

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Test Alert",
    "body": "This is a test email",
    "level": "error",
    "target": "user1@example.com,user2@example.com"
  }'
```

### Webhook Provider

通用 Webhook Provider，可向任意 URL 发送 POST 请求。

**配置：**

```yaml
providers:
  webhook:
    type: builtin
    config:
      url: "${WEBHOOK_URL}"
      method: "POST"  # 可选: GET, POST, PUT
      headers:        # 可选
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
  "target": "",
  "timestamp": "2026-05-18T12:00:00Z",
  "data": {}
}
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

func (p *Provider) Deliver(ctx context.Context, task *core.Task) error {
    // 实现发送逻辑
    return nil
}

func (p *Provider) Name() string {
    return p.name
}

func (p *Provider) Type() string {
    return "builtin"
}

func (p *Provider) Status() *core.ProviderStatus {
    return &core.ProviderStatus{
        Name:   p.name,
        Type:   "builtin",
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
    return "builtin"
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
