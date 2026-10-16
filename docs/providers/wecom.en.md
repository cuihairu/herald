> English · [中文](./wecom.md)

# WeCom

Push notifications to a WeCom group chat via a WeCom group bot Webhook.

## What it does

The `wecom` Builtin Provider calls a WeCom group bot's Webhook endpoint and sends Herald notifications to the group chat as markdown messages. Good for internal on-call and alert groups.

## Get credentials

No approval needed; get a Webhook in one minute:

1. Open the target WeCom group → top-right **...** → **Group Robots** → **Add a Robot** → create a new one
2. **Copy the Webhook URL** (or just note the `key` parameter from the URL — configuring either one is enough)
3. If the WeCom admin console requires it, add the server's egress IP to the app's "Trusted IPs"

## Send your first message

After configuring (see below) and starting with `heraldd serve --config config.yaml`:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "demo",
    "level": "error",
    "title": "Herald 第一条推送",
    "body": "from curl",
    "channels": ["wecom"]
  }'
```

The API returns `"accepted":["wecom"]` and the group receives a markdown message with a bold orange-red title. Nothing arrived? Check the [troubleshooting guide](/guide/troubleshooting.en).

## Configuration options

Pick one of `webhook_url` and `key` (when both are set, `webhook_url` wins):

| Key | Required | Description | Default |
|----|------|------|--------|
| `webhook_url` | one of the two | full bot Webhook URL | none |
| `key` | one of the two | the bot Webhook's `key` parameter (visible on the group bot creation page); Herald assembles `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key={key}` automatically | none |

If both are missing, Provider creation fails immediately (`wecom: webhook_url or key is required`).

> **Note**: the code reserves `mentioned_list` / `mentioned_mobile_list` (@-mention members) struct fields; current message construction does not use them, so configuring them has no effect.

## Configuration example

```yaml
providers:
  wecom:
    type: wecom
    enabled: true
    config:
      # Option 1: full Webhook URL
      webhook_url: "$WECOM_WEBHOOK_URL"
      # Option 2: fill in only the key; Herald assembles the URL
      # key: "$WECOM_WEBHOOK_KEY"
```

## Environment variables

When loading config, Herald replaces string values in provider config **that start with `$`** with the value of the environment variable of the same name (the `$VAR` form, looked up as `VAR`). Note: the braced form `"${VAR}"` **is not expanded** (it is looked up as `{VAR}` and kept as-is); use `$VAR`.

| Environment variable | Corresponding config key | Description |
|----------|-----------|------|
| `WECOM_WEBHOOK_URL` | `webhook_url` | bot Webhook URL (the conventional name in `.env.example`) |
| `WECOM_WEBHOOK_KEY` | `key` | the bot Webhook's `key` parameter (conventional name; reference it as `$VAR`) |

## Message template and limits

All messages use the WeCom `markdown` type. Titles are colored by level:

| Level | Title rendering |
|------|---------|
| `error` | `<font color='warning'>**{title}**</font>` (orange-red) |
| `warning` | `<font color='info'>**{title}**</font>` (green) |
| `info` and others | `**{title}**` (default color) |

A quote-styled send timestamp is appended after the body:

```
**{title}**

{body}

> 2026-09-28 10:00:00
```

- The Provider's capability declaration supports both `markdown` and `plain` content formats, but sends are always the markdown type
- Request timeout is 30s; HTTP 408/429/5xx are wrapped as retryable errors and go through unified retry
- WeCom-side limits: a bot can send at most 20 messages per minute per group, exceeding it returns `errcode: 45009`; markdown content is at most 4096 bytes

## Common errors

| Error | Cause and fix |
|------|-----------|
| `wecom: webhook_url or key is required` | neither `webhook_url` nor `key` is configured |
| `wecom API error: webhook invalid` | wrong key or the bot has been deleted; recreate the bot to get a new URL |
| `wecom API error: webhook rejected` | the bot is rate-limited (20 per minute) or the IP is not in the "Trusted IPs" list; add the server's egress IP in the admin console |
| `errcode: 45009` | API rate limit hit; send less frequently |

Error format notes: when WeCom returns `errcode != 0` the error is `wecom API error: {errmsg}`; on non-2xx HTTP the error is `unexpected status code: {code}, body: …`.

## Next steps

- [Provider overview](./overview.en.md) - see all Providers and how to enable them
- [WeChat Official Account](./wechat-official.en.md) - Official Account template message push
- [DingTalk](./dingtalk.en.md) - DingTalk group bot push
