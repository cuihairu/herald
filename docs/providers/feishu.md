# Feishu

Push notifications into Feishu groups via a custom-robot webhook.

## What It Does

The `feishu` Builtin Provider calls the custom-robot webhook interface and sends Herald's notifications to a group chat as plain text or as an interactive card. Alert notifications can carry an Acknowledge Alert button that integrates with Herald's alert callbacks.

## Obtaining Credentials

No approval needed — get a webhook URL in one minute:

1. Open the target Feishu group → **Settings** in the top-right → **Group Robots** → **Add Robot** → choose **Custom Robot**
2. Name and describe it, then create it; **copy the webhook URL** (like `https://open.feishu.cn/open-apis/bot/v2/hook/xxxx`)
3. Choose one of three security settings: **Custom keyword** (the message must contain that word), **Signature verification** (⚠️ not supported by the current implementation — **do not pick this**), **IP allowlist**

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
    "channels": ["feishu"]
  }'
```

The API returns `"accepted":["feishu"]` and the group receives `[Error] Herald first push`. If you set a custom keyword, remember to include that word in the title/body. Didn't arrive? See the [Troubleshooting Guide](/guide/troubleshooting).

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `webhook_url` | ✅ | The custom-robot webhook URL (Group Settings → Group Robots → Add Custom Robot) | None |
| `sign_secret` | ❌ | The robot's Signature verification secret. **Note: the current implementation only parses and stores it — it is not used in signing at send time** (reserved field) | Empty |
| `interactive_cards` | ❌ | Whether alert notifications are sent as interactive cards (with an Acknowledge Alert button) | `false` |

If `webhook_url` is missing, Provider creation fails immediately (`feishu: webhook_url is required`).

> **⚠️ Signature verification**: If the robot has Signature verification enabled in its Feishu-side security settings, the current implementation does not compute a signature, so Feishu rejects the request (it returns a signature-mismatch error). Please change the robot's security setting to a non-signing option such as Custom keyword, or wait for signature support to land.

## Configuration Example

```yaml
providers:
  feishu:
    type: feishu
    enabled: true
    config:
      webhook_url: "$FEISHU_WEBHOOK_URL"
      interactive_cards: true      # optional: send alerts as interactive cards
      # sign_secret: "$FEISHU_SIGN_SECRET"   # reserved, not used in signing yet
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `FEISHU_WEBHOOK_URL` | `webhook_url` | The custom-robot webhook URL (conventional name in `.env.example`) |

## Message Templates and Limits

**Default plain text** (level prefix + title + body):

```
[Error] {title}

{body}
```

| Level | Prefix |
|------|------|
| `error` | `[Error] ` |
| `warning` | `[Warning] ` |
| `info` | `[Info] ` |
| Others | No prefix |

**Interactive card** (when `interactive_cards: true` and the notification is associated with an alert ID):

- Card header color: `error` / `critical` → red, `warning` → orange, others → blue
- The card body uses the `lark_md` format
- An Acknowledge Alert button at the bottom, which calls back to Herald's alert acknowledgement API on click (see the rule engine / alert callback documentation)

- The Provider capability declaration supports only `plain`: markdown syntax in the default plain text message is **not** rendered by Feishu
- Request timeout 30s; HTTP 408/429/5xx are wrapped as retryable errors and go through unified retry
- Feishu-side limits: custom robots are rate limited to 100 per minute and 5 per second by default; exceeding the limit returns `code: 9499` (rate limiting)

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `feishu: webhook_url is required` | `webhook_url` not configured |
| `feishu API error: sign match fail` | The robot has Signature verification enabled while the current implementation does not sign — switch to the Custom keyword security setting |
| `feishu API error: key words not found` | The robot has Custom keyword set but the message does not contain that keyword (include the keyword in the message body) |
| `feishu API error: webhook is invalid` / `url not found` | The webhook URL is wrong, or the robot has been deleted/disabled; obtain the URL again |
| `feishu API error: Forbidden` | The robot was rate limited or removed from the group chat |

Error format notes: when Feishu returns `code != 0` the error is `feishu API error: {msg}`; for non-2xx HTTP responses it is `unexpected status code: {code}, body: …`.

## Next Steps

- [Provider Overview](./overview.md) - see all Providers and how to enable them
- [WeCom](./wecom.md) - WeCom group-robot push
- [DingTalk](./dingtalk.md) - DingTalk group-robot push
