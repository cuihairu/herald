# WeChat Official Account Push

This guide covers how to push messages through your own WeChat Official Account.

## Two approaches

Herald supports two ways to push via a WeChat Official Account:

| Approach | Description | When to use |
|------|------|---------|
| **Builtin Provider** | uses the WeChat template message API directly | simple scenarios; you already have an Official Account |
| **Worker Provider** | handled by a Worker process | complex scenarios that need extra logic |

## Option 1: Builtin Provider (recommended)

For the full `wechatmp` Provider configuration reference (template field mapping, token management, common errors) see [WeChat Official Account template messages](./wechatmp.md); here is a quick pass.

### Configuration

```yaml
providers:
  wechatmp:
    type: wechatmp
    enabled: true
    config:
      app_id: "$WECHATMP_APP_ID"
      app_secret: "$WECHATMP_APP_SECRET"
      template_id: "$WECHATMP_TEMPLATE_ID"
      default_url: "https://your-domain.com"
```

### Send a message

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order",
    "title": "Order notice",
    "body": "Your order has shipped",
    "level": "info",
    "channels": ["wechatmp"],
    "recipients": {
      "wechatmp": ["user OpenID"]
    }
  }'
```

### Template data

Herald maps content to WeChat template fields automatically:

| WeChat field | Herald content | Notes |
|---------|------------|------|
| `thing1` | title | title (20 characters max) |
| `thing2` | body | content (30 characters max) |
| `character_string1` | level | level |
| `time3` | current time | timestamp |

### Prerequisites

**Business qualifications:**
- business license
- corporate bank account
- CNY 300/year verification fee
- review cycle: 7-30 business days

**Technical requirements:**
- a WeChat service account (an Official Account of the service type)
- WeChat verification completed
- template message feature enabled

### Application process

1. **Register a service account**: visit https://mp.weixin.qq.com/ and register a service account
2. **Business verification**: submit the business license and corporate account information, and pay the verification fee
3. **Enable template messages**: after verification, enable the template message feature in the Official Account admin console
4. **Create a template**: pick or create a template in the template library and note the template ID

### Configuration notes

| Parameter | Description | Example |
|------|------|------|
| `app_id` | Official Account AppID | `wx-your-appid` |
| `app_secret` | Official Account AppSecret | `your-app-secret` |
| `template_id` | template message ID | `AT0001` |
| `default_url` | URL to open when the template is tapped (optional) | `https://your-domain.com` |

### Limit notes

| Limit | Notes |
|--------|------|
| **Push frequency** | 100 messages per minute per user |
| **Template count** | at most 25 templates |
| **Template review** | WeChat review required (1-7 days) |
| **Industry restriction** | must match the Official Account's industry |
| **Keywords** | content must not contain sensitive words |

## Option 2: Worker Provider

For scenarios that need complex processing logic, such as:
- fetching user info from a database
- calling other services
- custom message formats

### Configuration

```yaml
providers:
  wechatmp:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"
```

### Worker development

Develop with the Worker SDK:

```go
package main

import (
    "context"
    "time"

    "github.com/cuihairu/herald/core"
    "github.com/cuihairu/herald/protocol"
    workersdk "github.com/cuihairu/herald/worker-sdk/go"
)

func main() {
    config := &protocol.WorkerConfig{
        WorkerID:          "wechat-worker-01",
        CoreURL:           "ws://localhost:8081/worker",
        ReconnectDelay:    5 * time.Second,
        HeartbeatInterval: 30 * time.Second,
        Capabilities:      []string{"wechatmp"},
    }

    client := workersdk.NewClient(config, nil) // nil queue uses the built-in in-memory queue

    client.OnTask(func(task *core.DeliveryTask) error {
        // handle the task (acknowledgement is handled by the SDK/queue, no manual Ack needed)
        return sendWechatTemplateMessage(task)
    })

    _ = client.Run(context.Background())
}
```

See the [Worker SDK](/runtime/sdk) documentation for details.

## Get a user's OpenID

After a user follows the Official Account you need their OpenID to send messages:

### Method 1: user authorization (web authorization)

Generate an authorization link and have the user authorize:

```
https://open.weixin.qq.com/connect/oauth2/authorize?appid=APPID&redirect_uri=REDIRECT_URI&response_type=code&scope=snsapi_base#wechat_redirect
```

### Method 2: the follow event

Handle the subscribe event in the Official Account server configuration to obtain the OpenID.

## FAQ

### Q: My template was rejected — what now?

A: fix it based on the rejection reason:
- content does not fit industry norms
- contains promotional material
- template format is not standard

### Q: Users don't receive messages?

A: check:
- whether the user follows the Official Account
- whether the OpenID is correct
- whether the template passed review
- whether a rate limit was triggered

### Q: Access token expired?

A: the Provider manages the access token automatically; no manual handling needed.

## Next steps

- [Provider overview](./overview.md) - see all Providers
- [WeChat Official Account template messages](./wechatmp.md) - wechatmp Provider configuration reference
- [WeChat personal push](./wechat.md) - the easy way with third-party services
- [Worker SDK](/runtime/sdk) - learn Worker development
