# Bark

Send push notifications to an iPhone (or iPad) running the [Bark](https://github.com/Ashengzxb/Bark) app, through the public `api.day.app` server or your own bark-server.

## What It Does

The `bark` Builtin Provider posts Herald notifications to `POST {server}/push` on the public Bark endpoint `https://api.day.app` by default. A single `device_key` identifies the target device; `title`, `body` and `url` carry the message, while `group`, `ttl`, `sound` and `level_sound` shape how the push looks and behaves. It is a good fit for "ping my own phone" alerting — no approval flow and no developer account.

Bark answers with `{"code":200}` on success; any other code is reported as a delivery failure.

## Getting Your Device Key

1. Install **Bark** from the App Store on the iPhone.
2. Open the app: the home screen shows an example push URL like `https://api.day.app/AbCdEf1234567890/ping?title=...`.
3. The path segment right after `https://api.day.app/` (in the example, `AbCdEf1234567890`) is your **device key** — that is the `device_key` value.
4. Run the example push shown in the app once to make sure the device is reachable, then put the key in Herald's config.

Using your own [bark-server](https://github.com/Ashengzxb/bark-server) instead? Point `server` at it (step 3's key comes from that server's device list instead of `api.day.app`).

## Sending Your First Message

Configure the provider (see below), start Herald, then:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "demo",
    "level": "error",
    "title": "Herald first push",
    "body": "delivered via Bark",
    "channels": ["bark"]
  }'
```

The API returns `"accepted":["bark"]`, and the push lands on the phone in a couple of seconds. Nothing arrived? Check `device_key` first, then see the [Troubleshooting Guide](/guide/troubleshooting).

## Configuration Options

| Key | Required | Description | Default |
|-----|----------|-------------|---------|
| `device_key` | Yes | The Bark device key of the target iPhone | — |
| `server` | No | Bark server base URL; points at a self-hosted bark-server when overridden | `https://api.day.app` |
| `group` | No | The notification group shown in the Bark app | empty (Bark's own default group) |
| `ttl` | No | Message lifetime in seconds, as reported to Bark | `0` (Bark decides) |
| `sound` | No | The notification sound name | empty (system default) |
| `level_sound` | No | Map of notification level (`error` / `warning` / `info` / …) to sound name; an entry for the task's level wins over `sound` | unset |
| `url` | No | The URL opened when the user taps the push | empty |

The Bark API also accepts an `icon` field; it is part of the payload surface but has no configuration mapping yet, so Herald never sets it.

## Configuration Example

```yaml
providers:
  bark:
    type: bark
    enabled: true
    config:
      device_key: "$BARK_DEVICE_KEY"
      # Optional — defaults to the public server
      # server: "https://api.day.app"
      group: "herald"
      ttl: 600
      sound: "minuet"
      level_sound:
        error: "alarm"
        warning: "bell"
      url: "https://herald.example.com"
```

## Environment Variables

- `BARK_DEVICE_KEY` - the device key (`device_key`)

## Message Template and Limits

- `title` and `body` come from the notification content, exactly as delivered on other text channels; no channel-specific trimming is applied by the provider.
- Sound resolution: `level_sound[level]` when the task's level has an entry, otherwise `sound`.
- `ttl` is forwarded as seconds; pass `0` or omit it and Bark applies its own default retention.

## Common Errors

| Error | Cause and Fix |
|-------|---------------|
| `bark: device_key is required` | `config.device_key` is empty or unset. Put the key in `device_key`, or export `$BARK_DEVICE_KEY` and reference it as `"$BARK_DEVICE_KEY"`. |
| `bark API error: code <n> <message>` | Bark answered HTTP 200 but a non-`200` business code, so the push was rejected by the Bark server. Wrong or revoked `device_key` is the usual cause — copy the key again from the Bark app and make sure it belongs to the `server` you configured. |
| No push arrives but the API says accepted | Check the phone's Bark app is installed and online, and that `server` points at the server that issued the key (a public key posted at a self-hosted server, or vice versa, fails silently). |

## Next Steps

- [Provider Overview](./overview.md) - the full channel table and error/retry classification
- [APNs](./apns.md) - push to devices that run your own app (needs an Apple Developer account)
- [JPush](./jpush.md) / [Getui](./getui.md) - China-region app push
- [Troubleshooting Guide](/guide/troubleshooting) - delivery failures and log inspection