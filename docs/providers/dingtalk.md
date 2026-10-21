# DingTalk

Push notifications into DingTalk groups via a custom-robot webhook.

## What It Does

The `dingtalk` Builtin Provider calls the custom-robot webhook interface and sends Herald's notifications to a group chat as markdown messages.

## Obtaining Credentials

No approval needed — get a webhook in one minute:

1. Open the target DingTalk group → **Group Settings** → **Robots** → **Add Robot** → choose **Custom — connect your own service via Webhook**
2. Choose one of three security settings: **Custom keyword** (the message must contain that word — recommended), **Signature** (⚠️ not supported by the current implementation — **do not pick this**), **IP allowlist, ranges**
3. After creation, **copy the webhook URL** (like `https://oapi.dingtalk.com/robot/send?access_token=xxxx`; filling in just the `access_token` parameter also works)

## Sending Your First Message

Configure it (see below) and start with `heraldd serve --config config.yaml`:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "demo",
    "level": "error",
    "title": "Herald first push",
    "body": "from curl",
    "channels": ["dingtalk"]
  }'
```

The API returns `"accepted":["dingtalk"]` and the group receives a markdown message with a red title. If you set a custom keyword, put the keyword in the title (e.g. name the title `XX Alert`). Didn't arrive? See the [Troubleshooting Guide](/guide/troubleshooting).

## Configuration

`webhook_url` and `access_token` are mutually exclusive — one is required (`webhook_url` takes precedence when both are configured):

| Key | Required | Description | Default |
|----|------|------|--------|
| `webhook_url` | Either one | The full robot webhook URL | None |
| `access_token` | Either one | The `access_token` parameter from the robot webhook URL; Herald auto-composes `https://oapi.dingtalk.com/robot/send?access_token={access_token}` | None |
| `secret` | ❌ | The robot's Signature secret (starts with SEC). **Note: the current implementation only parses and stores it — it is not used in signing at send time** (reserved field) | Empty |

If neither is present, Provider creation fails immediately (`dingtalk: webhook_url or access_token is required`).

> **⚠️ Signature verification**: If the robot has Signature selected in its DingTalk-side security settings, the current implementation does not compute a signature, so DingTalk rejects the request (`sign not match`). Please switch to the Custom keyword or IP allowlist security settings instead. The code likewise reserves an @-mention (`at`) field that is currently unused.

## Configuration Example

```yaml
providers:
  dingtalk:
    type: dingtalk
    enabled: true
    config:
      # Option 1: full webhook URL
      webhook_url: "$DINGTALK_WEBHOOK_URL"
      # Option 2: only fill in access_token, Herald composes the URL
      # access_token: "$DINGTALK_ACCESS_TOKEN"
      # secret: "$DINGTALK_SECRET"   # reserved, not used in signing yet
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `DINGTALK_WEBHOOK_URL` | `webhook_url` | The robot webhook URL (conventional name) |
| `DINGTALK_ACCESS_TOKEN` | `access_token` | The `access_token` in the webhook URL (conventional name in `.env.example`) |
| `DINGTALK_SECRET` | `secret` | The Signature secret (reserved, not used in signing yet) |

## Message Templates and Limits

All messages always use the DingTalk `markdown` type. The title is a level-3 heading rendered in a color by level:

| Level | Title rendering |
|------|---------|
| `error` | `### <font color='#ff0000'>{title}</font>` (red) |
| `warning` | `### <font color='#ff9900'>{title}</font>` (orange) |
| `info` and others | `### {title}` (default color) |

A separator line and a send timestamp are appended after the body:

```
### {title}

{body}

---

> 2026-09-28 10:00:00
```

- The Provider capability declaration supports both `markdown` and `plain` content formats, but sends always use the markdown type
- Request timeout 30s; HTTP 408/429/5xx are wrapped as retryable errors and go through unified retry
- DingTalk-side limits: at most 20 messages per minute per robot; exceeding the limit returns `errcode: 130101` (rate limiting); markdown messages are limited to 5000 bytes

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `dingtalk: webhook_url or access_token is required` | Neither `webhook_url` nor `access_token` is configured |
| `dingtalk API error: sign not match` | The robot's security setting is Signature while the current implementation does not sign — switch to Custom keyword or the IP allowlist |
| `dingtalk API error: keywords not in content` | The security setting is Custom keyword but the message does not contain that keyword (simply include the keyword as one word in the title) |
| `dingtalk API error: ip is not in white list` | The server's egress IP is not in the robot's IP allowlist; add it in the DingTalk console |
| `errcode: 130101` | Rate limit triggered (20 per minute); reduce the send frequency |

Error format notes: when DingTalk returns `errcode != 0` the error is `dingtalk API error: {errmsg}`; for non-2xx HTTP responses it is `unexpected status code: {code}, body: …`.

## Next Steps

- [Provider Overview](./overview.md) - see all Providers and how to enable them
- [WeCom](./wecom.md) - WeCom group-robot push
- [Feishu](./feishu.md) - Feishu robot push
