> English · [中文](./wechat.md)

# WeChat Personal Push

Deliver messages to your **personal WeChat** via third-party push services — no business qualification, no Official Account. Good for personal alerting and reminders.

## What it does

The `wechat` Builtin Provider reuses the HTTP APIs of three third-party services; `service` picks the backend:

| `service` | Service | Official API |
|-----------|------|----------|
| `serverchan` (default) | [ServerChan](https://sct.ftqq.com/) | `POST https://sctapi.ftqq.com/{send_key}.send` |
| `pushplus` | [PushPlus](http://www.pushplus.plus/) | `POST https://www.pushplus.plus/send` |
| `wxpusher` | [WxPusher](https://wxpusher.zjiecode.com/) | `POST https://wxpusher.zjiecode.com/api/send/message` |

All three services "push to my WeChat" rather than "push to a specific user", so `recipients` (delivery targets) has **no effect** on this Provider — messages go uniformly to the account tied to the credential or to all of its followers. To target individuals, use [FCM](./fcm.en.md) / [APNs](./apns.en.md) / [WeChat Official Account template messages](./wechatmp.en.md).

## Get credentials

Prerequisite: register an account with any one of the third-party services (all free, all require WeChat QR-code authorization).

### ServerChan (default)

1. Visit [sct.ftqq.com](https://sct.ftqq.com/) and log in with WeChat QR code
2. Top-right "Message Push" → the **SendKey** page; copy the SendKey (shaped like `SCT1234567890abcdef`)
3. Free quota is 5 messages/day; beyond that you need a paid membership or wait for the next-day reset

### PushPlus

1. Visit [pushplus.plus](http://www.pushplus.plus/) and log in with WeChat QR code
2. The "Token" page on the left; copy the Token (shaped like `a1b2c3...`)
3. Free quota is 200 messages/day; under "Message Channels" you can bind WeChat, email, an Official Account, etc., and messages are delivered there in parallel

### WxPusher

1. Visit [wxpusher.zjiecode.com](https://wxpusher.zjiecode.com/) and log in with WeChat QR code
2. "App Management" on the left → Create App, fill in name and description → after creation you get an **AppToken** (shaped like `AT_xxxxxxxx`)
3. After following the app, your **UID** shows under "User List"; leave the UID unset to push to all followers
4. Free quota is 1000 messages/day

## Send your first message

After configuring as below and starting with `heraldd serve --config config.yaml`:

```yaml
# append to config.yaml (ServerChan; take the SendKey from the steps above)
providers:
  wechat:
    type: wechat
    enabled: true
    config:
      service: serverchan        # can be omitted, defaults to serverchan
      send_key: "$WECHAT_SERVERCHAN_SEND_KEY"
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "alert.disk",
    "level": "warning",
    "title": "磁盘告警",
    "body": "/dev/sda1 已用 95%",
    "channels": ["wechat"]
  }'
```

WeChat receives "🟡 磁盘告警 / /dev/sda1 已用 95%" with the send time appended at the end. Nothing arrived? First confirm the credentials are right and the service is the one you meant, then check the [troubleshooting guide](/guide/troubleshooting.en) and Common Errors below.

> Note that config key names change with `service`: ServerChan uses `send_key`, PushPlus uses `token`, WxPusher uses `app_token`. A wrong key name fails at startup with `... is required` — it does not fail silently.

## Configuration options

| Key | Required | Description | Default |
|----|------|------|--------|
| `service` | ❌ | backend service: `serverchan` / `pushplus` / `wxpusher` | `serverchan` |
| `send_key` | conditionally required | ServerChan SendKey (required when `service: serverchan`) | none |
| `token` | conditionally required | PushPlus Token (required when `service: pushplus`) | none |
| `app_token` | conditionally required | WxPusher AppToken (required when `service: wxpusher`) | none |
| `uid` | ❌ | WxPusher targeting UID; empty pushes to all followers, multiple values comma-separated | none |

When `service` is absent the default is ServerChan — **filling in credentials for another service while forgetting `service` fails at startup with `send_key is required for serverchan`**.

## Configuration examples

One block per backend; pick one as needed:

```yaml
providers:
  # ServerChan
  wechat:
    type: wechat
    enabled: true
    config:
      service: serverchan
      send_key: "$WECHAT_SERVERCHAN_SEND_KEY"
```

```yaml
providers:
  # PushPlus
  wechat:
    type: wechat
    enabled: true
    config:
      service: pushplus
      token: "$WECHAT_PUSHPLUS_TOKEN"
```

```yaml
providers:
  # WxPusher (optionally target your own UID)
  wechat:
    type: wechat
    enabled: true
    config:
      service: wxpusher
      app_token: "$WECHAT_WXPUSHER_APP_TOKEN"
      uid: "$WECHAT_WXPUSHER_UID"   # can be left empty; empty pushes to all followers
```

For high availability you can configure multiple `wechat` instances (different provider names) pointing at different services and switch lanes when one rate-limits or goes down:

```yaml
providers:
  wechat-sc:
    type: wechat
    enabled: true
    config:
      service: serverchan
      send_key: "$WECHAT_SERVERCHAN_SEND_KEY"
  wechat-wx:
    type: wechat
    enabled: true
    config:
      service: wxpusher
      app_token: "$WECHAT_WXPUSHER_APP_TOKEN"
```

## Environment variables

When loading config, Herald replaces string values in provider config **that start with `$`** with the value of the environment variable of the same name (the `$VAR` form, looked up as `VAR`). Note: the braced form `"${VAR}"` **is not expanded** (it is looked up as `{VAR}` and kept as-is); use `$VAR`.

| Environment variable | Corresponding config key | Description |
|----------|-----------|------|
| `WECHAT_SERVERCHAN_SEND_KEY` | `send_key` | ServerChan SendKey |
| `WECHAT_PUSHPLUS_TOKEN` | `token` | PushPlus Token |
| `WECHAT_WXPUSHER_APP_TOKEN` | `app_token` | WxPusher AppToken |
| `WECHAT_WXPUSHER_UID` | `uid` | WxPusher UID (can be empty) |

## Message template and limits

### Content mapping

| Field | ServerChan | PushPlus | WxPusher |
|------|-----------|----------|----------|
| Title | `title` | `title` | `summary` (the WeChat notification-bar title) |
| Body | `desp` (Markdown) | `content` (template defaults to HTML) | `content` with `contentType=3` (Markdown) |
| Summary | `short`, the first 64 characters of the body | — | — |
| Targeting | — | — | `uids`, included only when `uid` is configured |

The Provider assembles the message body: **level icon + body + send time**, so in curl you only provide `title`/`body`:

| `level` | Leading icon |
|---------|----------|
| `critical` / `error` | 🔴 |
| `warning` | 🟡 |
| `info` | 🟢 |
| `debug` | ⚪ |

To send rich text (bold, lists), just write Markdown in `body`:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "deploy.done",
    "level": "info",
    "title": "部署成功",
    "body": "**项目**: Herald\n**版本**: v1.0.0\n**状态**: ✅ 成功",
    "channels": ["wechat"]
  }'
```

### Delivery semantics

- One delivery sends exactly one message; the target is fixed to the credential's account/followers and `recipients` plays no part
- A timestamp in the form `---\n2026-10-03 10:20:30` is appended to the body automatically, handy for looking back
- The ServerChan `short` summary is truncated at **64 characters**; the body is kept in full

### Capability declaration

- `PayloadKinds`: `Content` (for passthrough-style payloads, use a solution other than FCM/APNs/WxPusher)
- `ContentFormats`: `plain`

### Retry semantics

- Business error codes returned by the third party (ServerChan `code != 0`, PushPlus `code != 200`, WxPusher `code` other than `1000`/`0`) are **deterministic errors** and are not retried — resending would just hit the quota or rate limit again
- Request timeouts and HTTP 408/429/5xx are wrapped by `httpclient` as retryable errors and go through unified retry with backoff

## Common errors

| Error | Cause and fix |
|------|-----------|
| `wechat: unsupported service 'xxx', use: serverchan, pushplus, or wxpusher` | `service` is misspelled or set to an illegal value other than empty |
| `wechat: send_key is required for serverchan` | ServerChan was selected but `send_key` is unset (common when PushPlus/WxPusher credentials are configured yet `service` is missing) |
| `wechat: token is required for pushplus` | PushPlus was selected but `token` is unset |
| `wechat: app_token is required for wxpusher` | WxPusher was selected but `app_token` is unset |
| `wechat: serverchan error: <message>` | rejected on the ServerChan side: wrong `SendKey`, quota exhausted, or the message contains sensitive words |
| `wechat: pushplus error: <msg>` | rejected on the PushPlus side: `Token` invalid, quota exhausted, or `IP` not allowlisted (configurable in the console under "Account Settings") |
| `wechat: wxpusher error: <msg>` | rejected on the WxPusher side: wrong `AppToken`, the app not followed, or quota exhausted |
| `wechat: failed to parse response: ...` | the service response body is not the expected JSON — usually an HTML error page from a proxy/gateway |
| `wechat: task is nil` | an internal call passed a nil task; does not occur on the normal delivery path |

All three have small free quotas (5 / 200 / 1000 messages per day). Quota-type errors are **not retried**; you need to reduce alert noise or switch to a channel without message-count limits ([Webhook](./webhook.en.md) with a self-built receiver, [Feishu](./feishu.en.md), and [WeCom](./wecom.en.md) group bots all have no message-count limit). For end-to-end troubleshooting see the [troubleshooting guide](/guide/troubleshooting.en).

## Security recommendations

1. **Credentials via environment variables**: a SendKey / Token / AppToken is equivalent to account access — inject via `.env` or a secrets manager, and don't commit it in the repo's config
2. **A ServerChan SendKey can end up public**: it appears in the push URL — be careful before sharing logs or screenshots
3. **Switching services means switching credentials**: the three are not interchangeable — never put a PushPlus Token into `send_key`
4. **Before sending sensitive content on a personal channel**: content passes through the third-party service's servers; mind compliance

## Next steps

- [WeChat Official Account template messages](./wechatmp.en.md) - switch to an Official Account when you need per-user targeting
- [FCM](./fcm.en.md) / [APNs](./apns.en.md) - targeted app push
- [Webhook](./webhook.en.md) - a self-built channel with no message-count limit
- [Provider overview](./overview.en.md) - see all Providers and how to enable them
