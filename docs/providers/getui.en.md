> English · [中文](./getui.md)

# Getui (GeTui Push)

Send push notifications to Android / iOS / HarmonyOS devices, with delivery-rate optimization for domestic vendor channels.

## What It Does

The `getui` Builtin Provider calls the Getui REST API v2 directly (`POST {BaseUrl}/push/single/cid`, where `BaseUrl` is `https://restapi.getui.com/v2/$appId`, see the [API calling specification](https://docs.getui.com/getui/server/rest_v2/standard/)). Authentication is the interface token, obtained via `POST {BaseUrl}/auth`, with the signature being the fixed-order concatenation `sha256(appkey + timestamp + mastersecret)`.

Each delivery target is a **CID** (the user unique identifier Getui assigns per device). Sending happens device by device, and one message has two carriers:

- Content (title/body) → the `push_message.notification` segment, which enters the system notification center
- Raw payload → the `push_message.transmission` passthrough segment, which does not enter the notification center and is handled by the Getui SDK callback integrated in the app
- Notifications on the Getui channel only display on Android/HarmonyOS; iOS goes through `push_channel.ios.aps` instead (added automatically by Herald for messages carrying Content)

## Obtaining Credentials

Prerequisite: a [Getui account](https://dev.getui.com/) (registration is free).

1. **Create the app**: the [Developer Center](https://dev.getui.com/dev/#/appManage) → **应用管理 (App Management)** → create the app (enter the package name for Android, the Bundle ID for iOS)
2. **Note the AppID**: the app's unique identifier, and also a segment of the API path (the `$appId` in `BaseUrl`)
3. **Note the AppKey**: generated when the app is created; used for the `/auth` `appkey` and the signature
4. **Note the MasterSecret**: visible on the app details page; it is equivalent to full push credentials and is used on the server side only
5. **Device CID**: obtained after the app integrates the [Getui SDK](https://docs.getui.com/getui/mobile/android/androidstudio/), via the `onReceiveClientId` callback on Android and the `GeTuiSdkDidRegisterClient` callback on iOS. Your business service stores the CID in a roster/user table and passes it in as the recipient at delivery time; Herald does not fetch CIDs from Getui.

## Sending Your First Message

Configure as below and start with `heraldd serve --config config.yaml` (**a CID recipient is required**):

```yaml
# config.yaml append (AppID/AppKey/MasterSecret come from the application steps above)
providers:
  getui:
    type: getui
    enabled: true
    config:
      app_id: "your_app_id"
      app_key: "your_app_key"
      master_secret: "$GETUI_MASTER_SECRET"   # see Environment Variables
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order.notice",
    "level": "info",
    "title": "订单已发货",
    "body": "您的订单 SF123456 已发出",
    "channels": ["getui"],
    "recipients": {"getui": ["CID-字符串"]}
  }'
```

The phone's notification center shows "订单已发货" (Order shipped). Didn't arrive? Check that the CID is correct and that the app has integrated the Getui SDK and is in a pushable state; see the [Troubleshooting Guide](/guide/troubleshooting.en) and [Common Errors](#common-errors) below.

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `app_id` | ✅ | The app's unique identifier (a segment of the API path) | None |
| `app_key` | ✅ | The app AppKey (the `/auth` `appkey` and signature input) | None |
| `master_secret` | ✅ | The app MasterSecret (signature input, server side only) | None |
| `ttl` | ❌ | Offline retention duration in milliseconds; `-1` means no offline retention; range `-1` to `259200000` (3 days) | Getui's default of 2 hours |
| `click_type` | ❌ | The follow-up action when the notification is tapped, see the table below | `none` |
| `endpoint` | ❌ | API endpoint (override for testing/proxy scenarios) | `https://restapi.getui.com/v2` |

Allowed `click_type` values (Herald supports only the four that need no extra fields):

| Value | Behavior |
|----|------|
| `none` | Plain notification, no follow-up action |
| `startapp` | Open the app home page |
| `payload` | Custom message content, **launches the app** |
| `payload_custom` | Custom message content, **does not launch the app** |

Getui also supports `url` (open a web page) and `intent` (open a specific in-app page), but both require extra `url`/`intent` fields that Herald's message model does not carry, so they are unsupported — configuring them fails at startup with `getui: unsupported click_type`.

Missing `app_id` / `app_key` / `master_secret` makes Provider creation fail immediately (`getui: app_id is required`, etc.); this is visible in the startup logs.

## Configuration Example

```yaml
providers:
  getui:
    type: getui
    enabled: true
    config:
      app_id: "your_app_id"
      app_key: "your_app_key"
      master_secret: "$GETUI_MASTER_SECRET"
      ttl: 86400000          # keep offline for 1 day
      click_type: "startapp" # tapping returns to the app home page
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `GETUI_APP_ID` | `app_id` | The app's unique identifier (conventional name in `.env.example`) |
| `GETUI_APP_KEY` | `app_key` | The app AppKey |
| `GETUI_MASTER_SECRET` | `master_secret` | The app MasterSecret |

## Message Templates and Limits

### Content Mapping

| Scenario | body structure |
|------|-----------|
| Notification with title/body | `push_message.notification{title,body,click_type}` + `push_channel.ios.aps{alert,sound:default}` |
| Pure Raw payload | `push_message.transmission` — passed through to the app, not shown in the notification center; the content is the Raw JSON |
| Both Content and Raw | Only `notification` is sent, with Raw going into `notification.payload`; carried only when `click_type` is `payload`/`payload_custom` |

- Getui marks both `notification.title`/`body` as required, so for a title-only or body-only message the other field falls back to the same value
- Getui requires exactly one of `notification` and `transmission` (mutually exclusive with `revoke`); they cannot be sent together — Herald therefore downgrades Raw into the tap-callback `payload`
- The Raw value is JSON-serialized and its length limit is counted in **characters** (`utf8.RuneCountInString`)
- Content and Raw — at least one is required, otherwise you get `getui: payload needs content or raw data`

### Length and Value Limits

| Field | Limit | Who enforces it |
|------|------|--------|
| `transmission`, `notification.payload` | ≤ 3072 characters | An over-limit `transmission` errors out immediately; an over-limit `payload` drops that field (the notification still sends) |
| `notification.title` | ≤ 50 characters | Validated by the Getui server (returns 20001); Herald does not pre-check |
| `notification.body` | ≤ 256 characters | Same as above |
| `settings.ttl` | `-1` to `259200000` ms | Validated by Herald at startup |
| `request_id` | 10-32 characters, and must not repeat | Herald generates a 24-character random id per send; duplicates make Getui **drop the message** |

### Delivery Semantics

- Recipients are sent to one CID at a time; partial failure is aggregated into `getui: N/M succeeded - failed: ...` (consistent with FCM/APNs/JPush), and an invalid CID only counts as its own failure
- Business code `10001` (invalid token) triggers the passive refresh Getui recommends: drop the cached token, `/auth` again, and retry once with the **same `request_id`** (the first attempt was never delivered, so reusing the id will not collide with Getui's dedup)
- `/auth` allows at most 100 calls per minute and 100,000 per day. The token is valid for "call time + 1 day"; Herald caches by the returned `expire_time` and refreshes 5 minutes early, and the refresh happens inside the cache lock — concurrent deliveries at cold start trigger only one `/auth`

### Capability Declaration

- `PayloadKinds`: `Content`, `Raw`
- `ContentFormats`: `plain`

### Retry Semantics

- Request timeouts, 408/429/5xx are wrapped by `httpclient` as retryable errors and go through unified retry
- 400 (20001 invalid parameter), 401 (10001/10002/10003), 403 (the `30000` range) are deterministic errors and are not retried
- Business code `10001` is the sole exception: Herald has already done one passive refresh + retry inside the provider

## Common Errors

### Fails at Startup

| Error | Cause and remedy |
|------|-----------|
| `getui: app_id is required` | AppID not configured |
| `getui: app_key is required` | AppKey not configured |
| `getui: master_secret is required` | MasterSecret not configured |
| `getui: ttl must be between -1 and 259200000 ms` | The offline retention is outside Getui's range (note the unit is milliseconds) |
| `getui: unsupported click_type "url"` | `url`/`intent` require extra fields; Herald does not support them |
| `getui: transmission exceeds 3072 chars: N` | The pure passthrough message is too long; trim the Raw payload |
| `getui: payload needs content or raw data` | Both Content and Raw are empty |

### Business Codes Returned by the API

| code | HTTP | Meaning and remedy |
|------|------|-----------|
| `10001` | 401 | Token wrong/expired: Herald has already refreshed and retried once; if it keeps happening, `/auth` is being rate limited or the credentials are wrong |
| `10002` | 401 | The appId or IP is on a blacklist |
| `10003` | 401 | Per-minute auth frequency exceeded (limit 100 per minute) |
| `10005` | 401 | Per-minute call frequency exceeded |
| `20001` | 400 | Invalid parameter; `msg` names the field (e.g. `title is invalid`, `ios长度不能超过...` — "iOS length must not exceed ...") |
| `30007` | 403 | `app/all` push count over the daily limit (at most 20 per day) — this provider sends single pushes, so you normally never hit it |
| `30014` | 403 | App push frequency over the per-minute limit (at most 5 per minute) |
| `30015` | 403 | List push frequency over the per-minute limit |
| `5000` | — | The request never reached Getui's server: check the local network, proxy, and IP allowlist |

The `30000` range (30000-30028) covers plan/permission restrictions; you hit them when a non-VIP account configures vendor policies or uses scheduled pushes.

Error format: non-2xx HTTP responses return `unexpected status code: {code}, body: {"code":..,"msg":".."}`; when HTTP is 200 but `code` is non-zero, `getui: push failed (code N): msg` is returned. For overall troubleshooting, see the [Troubleshooting Guide](/guide/troubleshooting.en).

## Security Recommendations

1. **The MasterSecret is equivalent to full push credentials**: keep it only in server-side environment variables / a secrets-management system, never in clients or code repositories; if you suspect a leak, reset it immediately in the Developer Center
2. **The token is a global interface credential**: Herald caches it only in memory — never on disk, never in logs; `master_secret` is flagged by `core.SensitiveFields`, and the API returns a masked value when the config is queried
3. **The CID is a device identifier**: treat it as personal data and mind compliance when storing and transmitting it
4. **Don't exhaust `/auth`**: Herald already caches by `expire_time` and dedups concurrent refreshes; if you add signing instances of your own, watch the 100-per-minute limit

## Next Steps

- [Provider Overview](./overview.en.md) - see all Providers and how to enable them
- [JPush](./jpush.en.md) - another domestic vendor push channel
- [FCM](./fcm.en.md) - Android/Web push channel
- [APNs](./apns.en.md) - iOS/macOS push channel
- [Troubleshooting Guide](/guide/troubleshooting.en) - startup failure, message not delivered, retries not taking effect
