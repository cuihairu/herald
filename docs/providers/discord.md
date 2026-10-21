# Discord

Push notifications into a Discord channel via Webhook (or the Bot API).

## What It Does

The `discord` Builtin Provider sends Herald's notifications to a Discord channel as Embed cards. Sending goes through the Webhook first; when no Webhook is configured, it falls back to the Bot API channel.

## Obtaining Credentials

Webhook method is recommended and needs no approval:

1. Open the target Discord channel → **Edit Channel** (the gear icon) → **Integrations** → **Create Webhook**
2. Click **Copy Webhook URL** (like `https://discord.com/api/webhooks/xxxx/yyyy`)

> ⚠️ The Bot API method does not attach an `Authorization` header in the current implementation and is rejected by Discord with 401 — **use the Webhook method**.

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
    "channels": ["discord"]
  }'
```

The API returns `"accepted":["discord"]` (Discord responds 204 on webhook success), and a red **Herald** Embed card appears in the channel. Didn't arrive? See the [Troubleshooting Guide](/guide/troubleshooting).

## Configuration

Three methods, listed by priority (Webhook first):

| Key | Description |
|----|------|
| `webhook_url` | The full webhook URL (channel → Edit → Integrations → Webhook). Takes precedence when configured |
| `webhook_id` + `webhook_token` | The two parameters of the webhook URL; Herald auto-composes `https://discord.com/api/webhooks/{id}/{token}`. If only `webhook_id` is configured without `webhook_token`, Provider creation fails |
| `bot_token` + `channel_id` | The Bot API channel: sends to `{api_url}/channels/{channel_id}/messages`. **Note: the current implementation does not attach the `Authorization: Bot …` header, so the Bot channel is actually rejected by Discord (401) — use the Webhook method in production** |
| `api_url` | Bot API root URL override, for self-hosted proxies; default `https://discord.com/api/v10` |

If none of the keys are configured (or only one of `bot_token` / `channel_id` is set), Provider creation fails immediately (`discord: either webhook_url or bot_token+channel_id is required`).

## Configuration Example

```yaml
providers:
  discord:
    type: discord
    enabled: true
    config:
      # Option 1 (recommended): full webhook URL
      webhook_url: "$DISCORD_WEBHOOK_URL"
      # Option 2: the two webhook parameters, Herald composes the URL
      # webhook_id: "$DISCORD_WEBHOOK_ID"
      # webhook_token: "$DISCORD_WEBHOOK_TOKEN"
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `DISCORD_WEBHOOK_URL` | `webhook_url` | The full webhook URL (conventional name in `.env.example`) |
| `DISCORD_WEBHOOK_ID` | `webhook_id` | The webhook ID (conventional name — reference it as `$VAR`) |
| `DISCORD_WEBHOOK_TOKEN` | `webhook_token` | The webhook Token (conventional name) |

## Message Templates and Limits

Messages are sent as Embeds, with the sender name **Herald**:

| Embed field | Value |
|------------|------|
| `title` / `description` | The notification title / body (verbatim, no level prefix) |
| `color` | Colored by level, see the table below |
| `timestamp` | The notification creation time (RFC 3339) |

Level colors:

| Level | color (decimal) | Color |
|------|----------------|------|
| `error` | `16711680` | Red |
| `warning` | `16776960` | Orange |
| `info` | `3447003` | Blue |
| Others | `9807270` | Gray |

- The Provider capability declaration supports both `markdown` and `plain` content formats (the Embed description supports Discord markdown)
- Request timeout 30s; HTTP 408/429/5xx are wrapped as retryable errors and go through unified retry (Discord returns 429 when rate limited and is retried automatically)
- Discord-side limits: about 30 Webhook messages per minute per channel (sustained excess returns 429); the Embed `description` is limited to 4096 characters and `title` to 256 characters

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `discord: webhook_token is required when using webhook_id` | Only `webhook_id` was configured — add `webhook_token` (or use the full `webhook_url` directly) |
| `discord: either webhook_url or bot_token+channel_id is required` | Neither the Webhook nor the Bot channel is configured (or only half of a pair is set) |
| `discord API error: Unknown Webhook` | The webhook URL is wrong or has been deleted; create a new one |
| `discord API error: 401: Unauthorized` | Invalid token; or the Bot channel was used — the current implementation does not attach a Bot auth header, switch to Webhook |
| `unexpected status code: 429, body: …` | Discord rate limit triggered; this is a retryable error type and backs off and retries automatically |

Error format notes: when the Discord response body contains `code != 0` the error is `discord API error: {message}`; for non-2xx HTTP responses it is `unexpected status code: {code}, body: …`. A successful Webhook send returns 204 with no response body.

## Next Steps

- [Provider Overview](./overview.md) - see all Providers and how to enable them
- [Slack](./slack.md) - Slack webhook push
- [Telegram](./telegram.md) - Telegram Bot push
