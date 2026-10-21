# Telegram

Push notifications to a given chat (private / group / channel) via the Telegram Bot API.

## What it does

The `telegram` Builtin Provider calls the Telegram Bot API's `sendMessage` endpoint and sends Herald notifications to the chat given by `chat_id`. Good for personal alert intake, group on-call notifications, and similar scenarios.

## Get credentials

No approval anywhere; you need two things: a Bot Token and a chat_id.

1. Find [@BotFather](https://t.me/BotFather) on Telegram → send `/newbot` → follow the prompts to name it → you get a **Bot Token** (shaped like `123456:ABC-DEF...`)
2. **Send your bot a message** (a private chat must be initiated by the user first, otherwise the bot cannot push to you; in a group it can see messages automatically)
3. Get the **chat_id**: open `https://api.telegram.org/bot<yourToken>/getUpdates` in a browser and look for `result[].message.chat.id` in the returned JSON (positive for private chats, negative like `-100…` for groups)

## Send your first message

After configuring (see below) and starting with `heraldd serve --config config.yaml`:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "demo",
    "level": "error",
    "title": "Herald first push",
    "body": "from curl",
    "channels": ["telegram"]
  }'
```

The API returns `"accepted":["telegram"]` and your Telegram receives `🔴 Herald first push` (the text = your `title`). Nothing arrived? First run `getUpdates` to confirm the token and chat_id are valid, then check the [troubleshooting guide](/guide/troubleshooting).

## Configuration options

| Key | Required | Description | Default |
|----|------|------|--------|
| `token` | ✅ | Bot Token, obtained by creating a bot with [@BotFather](https://t.me/BotFather) | none |
| `chat_id` | ✅ | target chat ID (a numeric ID for private chats, a negative `-100…` ID for groups) | none |
| `parse_mode` | ❌ | message parse mode: `markdown` or `html`; empty sends as plain text | empty |
| `api_url` | ❌ | API root URL; replace when using a self-hosted proxy / reverse proxy | `https://api.telegram.org` |

If `token` or `chat_id` is missing, Provider creation fails immediately (`telegram: token is required` / `telegram: chat_id is required`); it shows up in startup logs.

## Configuration example

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
      parse_mode: "markdown"        # optional: markdown / html, empty for plain text
      # api_url: "https://api.telegram.org"   # replace when using a self-hosted proxy
```

## Environment variables

When loading config, Herald replaces string values in provider config **that start with `$`** with the value of the environment variable of the same name (the `$VAR` form, looked up as `VAR`). Note: the braced form `"${VAR}"` **is not expanded** (it is looked up as `{VAR}` and kept as-is); use `$VAR`.

| Environment variable | Corresponding config key | Description |
|----------|-----------|------|
| `TELEGRAM_BOT_TOKEN` | `token` | Bot Token (the conventional name in `.env.example`) |
| `TELEGRAM_CHAT_ID` | `chat_id` | target chat ID |

## Message template and limits

Message text format (level emoji + bold title + body):

```
{level emoji} *{title}*

{body}
```

| Level | emoji |
|------|-------|
| `error` | 🔴 |
| `warning` | 🟡 |
| `info` | 🔵 |
| other | no prefix |

- Bold (`*…*`) only renders with `parse_mode: "markdown"`; when empty, the asterisks show as-is
- The Provider's capability declaration supports both `markdown` and `plain` content formats
- Telegram-side limits: message text is at most 4096 characters; longer messages are rejected by the API (`message is too long`)
- Request timeout is 30s; HTTP 408/429/5xx are wrapped as retryable errors and go through unified retry

## Common errors

| Error | Cause and fix |
|------|-----------|
| `telegram: token is required` | `token` is not configured |
| `telegram: chat_id is required` | `chat_id` is not configured |
| `telegram API error: Unauthorized` | Token is invalid or revoked; get a new one from @BotFather |
| `telegram API error: chat not found` | wrong `chat_id`, or the user never messaged the bot first (private chats need the user to `/start`) |
| `telegram API error: message is too long` | over the 4096-character limit; trim the body |
| `telegram API error: Bad Request: can't parse entities` | `parse_mode` doesn't match the content (e.g. unclosed markdown asterisks); switch to plain text or fix the syntax |

Error format notes: when the API returns `ok: false` the error is `telegram API error: {description}`; on non-2xx HTTP the error is `unexpected status code: {code}, body: …`.

## Next steps

- [Provider overview](./overview.md) - see all Providers and how to enable them
- [Feishu](./feishu.md) - Feishu bot push
- [WeCom](./wecom.md) - WeCom group bot push
