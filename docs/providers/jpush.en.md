> English · [中文](./jpush.md)

# JPush (JiGuang Push)

Send push notifications to Android / iOS / HarmonyOS devices, with delivery-rate optimization for domestic vendor channels.

## What It Does

The `jpush` Builtin Provider calls the JPush REST API v3 directly (`POST https://api.jpush.cn/v3/push`, see the [official documentation](https://docs.jiguang.cn/jpush/server/push/rest_api_v3_push)). Authentication is HTTP Basic: `base64(app_key:master_secret)`.

Each delivery target is a **registration_id** (the unique identifier the JPush SDK generates per device). Sending happens device by device, and one message has two carriers:

- Content (title/body) → the `notification` segment, which enters the system notification center (with a title line on Android, a default sound on iOS)
- Raw payload → the `message` passthrough segment, which does not enter the notification center and is handled by the JPush SDK callback integrated in the app
- `options.apns_production` controls whether iOS goes through the production or development APNs environment

## Obtaining Credentials

Prerequisite: a [JiGuang account](https://www.jiguang.cn/) (registration is free).

1. **Create the app**: the [JiGuang console](https://www.jiguang.cn/) → create the app (enter the package name for Android, the Bundle ID for iOS)
2. **Note the AppKey**: enter the app from the app list → the **AppKey** on the **应用设置 (App Settings)** page (like `7d431e42dfa6a6d693ac2d04`)
3. **Note the Master Secret**: the **Master Secret** on the same page (used on the server side only, equivalent to full push credentials, resettable in the console)
4. **Device registration_id**: after the app integrates the [JPush SDK](https://docs.jiguang.cn/jpush/client/Android/android_guide), obtained via the SDK's RegistrationID interface. Your business service stores it in a roster/user table and passes it in as the recipient at delivery time; Herald does not fetch registration_ids from JPush.

## Sending Your First Message

Configure as below and start with `heraldd serve --config config.yaml` (**a registration_id recipient is required**):

```yaml
# config.yaml append (AppKey/Master Secret come from the application steps above)
providers:
  jpush:
    type: jpush
    enabled: true
    config:
      app_key: "7d431e42dfa6a6d693ac2d04"
      master_secret: "$JPUSH_MASTER_SECRET"   # see Environment Variables
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order.notice",
    "level": "info",
    "title": "订单已发货",
    "body": "您的订单 SF123456 已发出",
    "channels": ["jpush"],
    "recipients": {"jpush": ["registration-id-字符串"]}
  }'
```

The phone's notification center shows "订单已发货" (Order shipped). Didn't arrive? Check that the registration_id is correct and that the app has integrated the JPush SDK and is in a pushable state; see the [Troubleshooting Guide](/guide/troubleshooting.en) and [Common Errors](#common-errors) below.

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `app_key` | ✅ | The app AppKey (console app settings page) | None |
| `master_secret` | ✅ | The app Master Secret (console app settings page) | None |
| `platform` | ❌ | Push platforms: `all`, a single one (`android`), comma-separated (`android,ios`), or a YAML list; official values `android`/`ios`/`quickapp`/`hmos` | `all` |
| `apns_production` | ❌ | Whether iOS targets go through the production (`true`) or development (`false`) APNs environment; set `false` when building a debug package for development | `true` (JPush defaults to production when unspecified) |
| `time_to_live` | ❌ | Offline message retention in seconds; `0` means no offline retention | JPush's default `86400` (1 day; up to 3 days for regular users, 10 days for VIP) |
| `endpoint` | ❌ | API endpoint (override for testing/proxy scenarios) | `https://api.jpush.cn` |

Missing `app_key` / `master_secret` makes Provider creation fail immediately (`jpush: app_key is required`, etc.); this is visible in the startup logs.

## Configuration Example

```yaml
providers:
  jpush:
    type: jpush
    enabled: true
    config:
      app_key: "7d431e42dfa6a6d693ac2d04"
      master_secret: "$JPUSH_MASTER_SECRET"
      platform: "android,ios"
      apns_production: false   # when building a debug package for development
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `JPUSH_APP_KEY` | `app_key` | The app AppKey (conventional name in `.env.example`) |
| `JPUSH_MASTER_SECRET` | `master_secret` | The app Master Secret |

## Message Templates and Limits

### Content Mapping

| Scenario | body structure |
|------|-----------|
| Notification with title/body | `notification.alert` (shared) + `android{title,alert}` + `ios{alert,sound:default}` |
| Pure Raw payload | `message{msg_content,extras}` — passed through to the app, not shown in the notification center; `msg_content` is the Raw JSON |
| Both Content and Raw | `notification` and `message` are sent together, with `msg_content` taking the readable body |

- When `body` is empty, `alert` takes the value of `title` (a title-only message still sends)
- The `android` segment is included only when `title` is non-empty (JPush requires `android.alert`; the shared `alert` already covers all platforms); the `ios` segment always carries `sound: default`
- Raw keys go into `extras`, with values stringified uniformly; `message.msg_content` is required by JPush, so the Raw JSON is the fallback when there is no body
- Content and Raw — at least one is required, otherwise you get `jpush: payload needs content or raw data`

### Delivery Semantics

- Recipients are sent to one registration_id at a time; partial failure is aggregated into `jpush: N/M succeeded - failed: ...` (consistent with FCM/APNs), and a dead device (error 1011) only counts as its own failure
- JPush rate-limits by API call count (per AppKey, minute-level window); triggering it returns 429 and goes through unified retry — keep concurrency low when delivering large volumes of identical content

### Capability Declaration

- `PayloadKinds`: `Content`, `Raw`
- `ContentFormats`: `plain`

### Retry Semantics

- Request timeouts, 408/429/5xx are wrapped by `httpclient` as retryable errors and go through unified retry
- 400 (1011 no target, 1003 invalid parameter, etc.), 401 (1004 authentication failure), 403 (restricted/allowlist) are deterministic errors and are not retried

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `jpush: app_key is required` | AppKey not configured |
| `jpush: master_secret is required` | Master Secret not configured |
| `jpush: time_to_live must be >= 0` | The offline retention was configured as a negative number |
| `jpush: payload needs content or raw data` | Both Content and Raw are empty |
| `{"error":{"code":1004,...}}` (401) | Authentication failure: the AppKey and Master Secret do not match (compare against the console; watch for stray spaces) |
| `{"error":{"code":1011,...}}` (400) | No push target satisfies the conditions: the registration_id is invalid, or the device was excluded for being inactive for over 255 days |
| `{"error":{"code":1003,...}}` (400) | Invalid parameter value: a registration_id is empty or malformed |
| `{"error":{"code":1005,...}}` (400) | Message body too large: Android Notification+Message combined is limited to 4000 bytes, the iOS `notification.ios{}` segment to 3584 bytes |
| `{"error":{"code":2002,...}}` (429) | API call rate limit exceeded; goes through unified retry (spread the requests over the time window) |
| `{"error":{"code":2003,...}}` (403) | The appKey is restricted from calling the API; contact JPush technical support |
| `{"error":{"code":2004,...}}` (403) | The source IP is not in the app's IP allowlist |
| `{"error":{"code":2010,...}}` (400) | The content contains sensitive words, or the push volume limit was exceeded |

Error format: non-2xx HTTP responses return `unexpected status code: {code}, body: {"error":{"code":..,"message":..}}`. For overall troubleshooting, see the [Troubleshooting Guide](/guide/troubleshooting.en).

## Security Recommendations

1. **The Master Secret is equivalent to full push credentials**: keep it only in server-side environment variables / a secrets-management system, never in clients or code repositories; if you suspect a leak, reset it immediately in the console
2. **The registration_id is a device identifier**: treat it as personal data and mind compliance when storing and transmitting it
3. **Separate development from production**: use `apns_production: false` for debug packages during development, and switch back to `true` (the default) for release

## Next Steps

- [Provider Overview](./overview.en.md) - see all Providers and how to enable them
- [FCM](./fcm.en.md) - Android/Web push channel
- [APNs](./apns.en.md) - iOS/macOS push channel
- [Getui](./getui.en.md) - another domestic vendor push channel
- [Troubleshooting Guide](/guide/troubleshooting.en) - startup failure, message not delivered, retries not taking effect
