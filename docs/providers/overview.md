# Providers

Herald supports a variety of notification channels, including instant messaging, email, SMS, push, and Webhook.

## Find a channel by task

| What you want to do | Which to use | Application difficulty |
|-----------|--------|---------|
| Send messages to a Feishu/WeCom/DingTalk group | [`feishu`](./feishu.md) / [`wecom`](./wecom.md) / [`dingtalk`](./dingtalk.md) | add a bot to the group and you're done, **zero approval** |
| Push to your own phone | [`telegram`](./telegram.md) or [`wechat`](./wechat.md) (ServerChan etc.) | Telegram needs zero approval; the WeChat third-party services work right after a QR-code login |
| Send verification codes / transactional SMS | [`aliyunsms`](./aliyunsms.md) / [`tencentsms`](./tencentsms.md) / [`neteasesms`](./neteasesms.md) | requires signature + template review |
| Send email | [`email`](./email.md) | any SMTP account works (authorization code) |
| Push to a Discord / Slack channel | [`discord`](./discord.md) / [`slack`](./slack.md) | create a Webhook and you're done, zero approval |
| Integrate with self-built systems / local debugging | [`webhook`](./webhook.md) / [`log`](./log.md) | zero credentials |
| WeChat Official Account template messages | [`wechatmp`](./wechatmp.md) | requires a verified service account |
| App push (Android/Web) | [`fcm`](./fcm.md) | needs a Firebase project; generate a service account in the console and you're set |
| App push (iOS/macOS) | [`apns`](./apns.md) | needs an Apple Developer account; generate a .p8 on the Keys page |
| App push (China, Android/iOS/HarmonyOS) | [`jpush`](./jpush.md) / [`getui`](./getui.md) | needs a JPush/Getui account; create an app in the console to get the AppKey/Master Secret |

Every channel page contains: **get credentials → configure → send your first message (commands you can follow along) → common errors**.

## Provider Categories

### Instant Messaging

| Provider | Description | Status |
|----------|------|------|
| [`telegram`](./telegram.md) | Telegram Bot | ✅ |
| [`feishu`](./feishu.md) | Feishu bot | ✅ |
| [`wecom`](./wecom.md) | WeCom bot | ✅ |
| [`wechat`](./wechat.md) | WeChat personal push (ServerChan/PushPlus/WxPusher) | ✅ |
| [`wechatmp`](./wechatmp.md) | WeChat Official Account template messages | ✅ |
| [`dingtalk`](./dingtalk.md) | DingTalk bot | ✅ |
| [`slack`](./slack.md) | Slack | ✅ |
| [`discord`](./discord.md) | Discord | ✅ |

### Email

| Provider | Description | Status |
|----------|------|------|
| [`email`](./email.md) | SMTP email | ✅ |

### SMS

| Provider | Description | Status |
|----------|------|------|
| [`aliyunsms`](./aliyunsms.md) | Alibaba Cloud SMS | ✅ |
| [`tencentsms`](./tencentsms.md) | Tencent Cloud SMS | ✅ |
| [`neteasesms`](./neteasesms.md) | NetEase Yunxin SMS | ✅ |

### Push

| Provider | Description | Status |
|----------|------|------|
| [`fcm`](./fcm.md) | Firebase Cloud Messaging (Android/Web) | ✅ |
| [`apns`](./apns.md) | Apple Push Notification service (iOS/macOS) | ✅ |
| [`jpush`](./jpush.md) | JPush (Android/iOS/HarmonyOS) | ✅ |
| [`getui`](./getui.md) | Getui (Android/iOS/HarmonyOS) | ✅ |

### Others

| Provider | Description | Status |
|----------|------|------|
| [`webhook`](./webhook.md) | Generic Webhook | ✅ |
| [`log`](./log.md) | Log output | ✅ |

## Builtin vs Worker

### Builtin Providers

Builtin Providers run directly inside the Herald core process; the config block looks like this:

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "your_token"
      chat_id: "your_chat_id"
```

### Worker Providers

Worker Providers run in a separate process and connect to Herald over WebSocket:

```yaml
providers:
  custom-provider:
    type: worker
    config:
      target: "custom-worker-01"   # equivalent to the name key
```

## Enable/Disable a Provider

### Via the config file

```yaml
providers:
  telegram:
    type: telegram
    enabled: true    # enable
    config:
      token: "your_token"

  slack:
    type: slack
    enabled: false   # disable
    config:
      webhook_url: "your_url"
```

### Via the API

```bash
# enable
curl -X POST http://localhost:8080/api/v1/providers/telegram/enable

# disable
curl -X POST http://localhost:8080/api/v1/providers/telegram/disable
```

### Via the Dashboard

On the Dashboard's Providers page, click the enable/disable button on each Provider card.

## Configuration Priority

1. API calls (runtime changes)
2. Config file (loaded at startup)
3. Default configuration

The `enabled` field in the config file sets the Provider's initial state; afterwards it can be changed dynamically via the API or the Dashboard.

## Error Classification and Retry

Whether a failed delivery is retried is decided by **error classification** (a six-value vocabulary, see the [Audience Domain Model](/design-audience-model)):

| Class | Trigger | Retried? |
|------|------|---------|
| `temporary` | upstream 5xx, general network errors | ✅ retried with backoff per the `retry` config |
| `rate_limited` | upstream 429 | ✅ when `Retry-After` is present, wait as instructed (capped at `retry.max_delay`); otherwise as above |
| `timeout` | request timed out (408 or transport timeout) | ✅ as above |
| `permanent` | upstream explicitly rejects and retrying is pointless | ❌ immediately failed |
| `authentication` | invalid credentials | ❌ immediately failed |
| `invalid_request` | the request itself is invalid | ❌ immediately failed |

- Providers that go through the built-in HTTP client (`core/httpclient`) get classification for free: 408 maps to `timeout`, 429 to `rate_limited`, 5xx and transport errors to `temporary` (the three retryable classes); other 4xx are unclassified and cannot be retried (in the words of the `classifyStatus` comment: retrying them cannot succeed)
- "HTTP 200 but business-code failure" is outside automatic classification and is up to each Provider: Tencent Cloud/NetEase Yunxin wrap business errors with `httpclient.WithRetry` overall (business-code failures are retried too), while Alibaba Cloud returns a bare error for business codes (no retry) — see each Provider page for channel-specific differences
- Retry waiting happens on the queue side: a retryable failed task is re-enqueued with `next_retry_at` and delivered again when due, while the worker delivers other tasks in the meantime; `Retry-After` waits are likewise absorbed on the queue side (capped at `retry.max_delay`). In the logs (`/api/v1/logs`) the task has a single entry and stays at `pending` while retrying
- When a retryable class exhausts `retry.max`, the task goes to dead (the `dead` vs permanently `failed` distinction exists only on the delivery status surface `/api/v1/deliveries`); the terminal error stays in the task's `last_error`, and the logs (`/api/v1/logs`) report both terminal states simply as `failed`

## Best Practices

1. Put credentials in environment variables and write `$VAR` placeholders in the config file
2. Configure multiple instances of the same type (e.g. two webhooks pointing at different receivers); when one fails, switch routing without touching business code
3. `enabled: false` only turns delivery off; the Provider is still created per its config and its credentials are still validated. For temporary downtime use the dynamic switches via API or Dashboard
4. When troubleshooting delivery issues, check `/api/v1/logs` first; failure reasons include the channel's original error

## Registration Mechanism and the Worker Placeholder

- Factories for all Builtin Providers are registered centrally in `providers/builtin/registry` (`RegisterBuiltinProviders`); the tables above are the full registered set, no manual registration needed
- A `worker`-type provider is a **local placeholder for a remote Worker**: its local Deliver is a no-op, and tasks are routed by the scheduler to the matching Worker node per `target` (`name` defaults to `worker`; `target` defaults to `name`). For applicable scenarios and how to develop one, see [Worker Runtime](/runtime/worker) and [Worker SDK](/runtime/sdk)

## Next Steps

- [WeChat personal push](./wechat.md) - push via third-party services
- [WeChat Official Account guide](./wechat-official.md) - guide to pushing through your own Official Account
- [WeChat Official Account template messages](./wechatmp.md) - wechatmp Provider configuration reference
- [Email](./email.md) - SMTP email channel
- [Webhook](./webhook.md) - custom HTTP receiver
- [Log](./log.md) - local debugging output
- [SMS Providers](./sms.md) - detailed SMS Provider configuration
