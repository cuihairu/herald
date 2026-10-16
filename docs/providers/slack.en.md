> English · [中文](./slack.md)

# Slack

Push notifications to a Slack channel via a Slack Incoming Webhook.

## What it does

The `slack` Builtin Provider calls a Slack Incoming Webhook and sends Herald notifications to a fixed channel as colored attachments; messages carry level and source-Provider fields.

## Get credentials

No approval needed, but you do need to create an App in the Slack admin console:

1. Open [api.slack.com/apps](https://api.slack.com/apps) → **Create New App** → **From scratch** → name it and pick a workspace
2. In the left sidebar **Incoming Webhooks** → turn on **Activate Incoming Webhooks** → **Add New Webhook to Workspace** → choose the target channel
3. Copy the generated Webhook URL (shaped like `https://hooks.slack.com/services/T00/B00/xxxx`)

> ⚠️ Slack is the only Provider **without startup validation**: a misconfigured `webhook_url` only errors at send time, so once you have the URL, be sure to send a verification message as shown below.

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
    "channels": ["slack"]
  }'
```

The API returns `"accepted":["slack"]` and a red attachment message from **Herald** (with Level/Provider fields) appears in the channel. Nothing arrived? Check the [troubleshooting guide](/guide/troubleshooting.en).

## Configuration options

| Key | Required | Description | Default |
|----|------|------|--------|
| `webhook_url` | ✅ (required in practice) | Incoming Webhook URL (obtained via Slack App → Incoming Webhooks → Add New Webhook) | none |

> **Note**: the code's Config struct also declares `workspace` / `app_id` / `app_secret`, but the current implementation only reads `webhook_url`; the rest are reserved and have no effect.
>
> **⚠️ No startup validation**: unlike other Providers, an empty `webhook_url` still lets the Provider be created successfully; the error surfaces at send time (request construction fails). After the config loads, run one send to verify.

## Configuration example

```yaml
providers:
  slack:
    type: slack
    enabled: true
    config:
      webhook_url: "$SLACK_WEBHOOK_URL"
```

## Environment variables

When loading config, Herald replaces string values in provider config **that start with `$`** with the value of the environment variable of the same name (the `$VAR` form, looked up as `VAR`). Note: the braced form `"${VAR}"` **is not expanded** (it is looked up as `{VAR}` and kept as-is); use `$VAR`.

| Environment variable | Corresponding config key | Description |
|----------|-----------|------|
| `SLACK_WEBHOOK_URL` | `webhook_url` | Incoming Webhook URL (the conventional name in `.env.example`) |

## Message template and limits

Messages are sent as attachments from **Herald** (avatar `:bell:`):

| Field | Value |
|------|------|
| `title` / `text` | notification title / body (as-is; no level prefix or title coloring) |
| `color` | colored by level, see table below |
| `fields` | `Level` (severity) and `Provider` (source Provider name), both short fields |
| `footer` | `Herald` |
| `ts` | notification creation time (Unix seconds) |

Level colors:

| Level | color |
|------|-------|
| `error` | `danger` (red) |
| `warning` | `warning` (yellow) |
| `info` | `#36a64f` (green) |
| other | `#808080` (gray) |

- The Provider's capability declaration supports both `markdown` and `plain` content formats (attachment text supports Slack mrkdwn)
- Request timeout is 30s; HTTP 408/429/5xx are wrapped as retryable errors and go through unified retry
- Slack-side limits: an Incoming Webhook can only post to the channel it was bound to at creation, roughly 1 message per second per Webhook; exceeding that returns `HTTP 429` (with `Retry-After`)

## Common errors

| Error | Cause and fix |
|------|-----------|
| `slack API error: invalid_payload` | request body is invalid (usually too long or malformed fields); trim the body and retry |
| `slack API error: channel_not_found` | the channel bound to the Webhook has been deleted; recreate the Webhook |
| `slack API error: channel_is_archived` | the channel is archived; pick another channel and recreate the Webhook |
| `slack API error: invalid_credentials` / HTTP 404 | the Webhook URL is no longer valid (App uninstalled or Webhook revoked); regenerate it |
| `failed to send request: …` | `webhook_url` is empty or malformed — it only surfaces at send time; check the config |

Error format notes: when Slack returns `{"ok": false, "error": …}` the error is `slack API error: {error}`; on non-2xx HTTP the error is `unexpected status code: {code}, body: …`.

## Next steps

- [Provider overview](./overview.en.md) - see all Providers and how to enable them
- [Discord](./discord.en.md) - Discord Webhook push
- [Telegram](./telegram.en.md) - Telegram Bot push
