# APNs (Apple Push Notification service)

Send push notifications to iOS / iPadOS / macOS / watchOS devices.

## What It Does

The `apns` Builtin Provider calls APNs directly (`POST https://api.push.apple.com/3/device/{token}`). It supports two authentication methods:

- **Token authentication (recommended)**: an ES256 JWT signed with your `.p8` private key (`key_id` + `team_id`), cached for one hour, re-signed automatically 5 minutes before expiry; one key can be used for both the development and production environments
- **Certificate authentication**: a TLS client certificate (PEM certificate chain + private key), for compatibility with the old .p12 workflow

Each delivery target is an **APNs device token**. A message splits into the `aps` segment (alert notification / background silent push) and custom data keys; `apns-push-type` and `apns-priority` are derived automatically from the notification type.

## Obtaining Credentials

Prerequisite: a **paid Apple Developer Program account** ($99/year, individual or organization).

### Token Authentication (recommended, four steps)

1. **Note your Team ID**: the **Team ID** (like `ABCDE12345`) under Membership Information on the [Account](https://developer.apple.com/account) page
2. **Create an APNs key**: [Certificates, Identifiers & Profiles → Keys](https://developer.apple.com/account/resources/authkeys/list) → **+** → tick **Apple Push Notifications service (APNs)** → after registering, **download the `.p8` file immediately** (like `AuthKey_XXXXXXXXXX.p8`) — it can only be downloaded once; if lost, you must recreate it
3. **Note your Key ID**: the 10-digit **Key ID** on the download page and in the key list
4. **Register the App ID (Bundle ID)**: [Identifiers → App IDs](https://developer.apple.com/account/resources/identifiers/list/bundleId) → create one, like `com.example.app` — this is the `topic` in your config

### Certificate Authentication (alternative)

1. Identifiers → App IDs → tick the **Push Notifications** capability
2. Certificates → create an **APNs SSL** certificate (one for development, one for production), export the .p12 from your local Keychain
3. Convert it to PEM (certificate chain + private key in one file):

```bash
openssl pkcs12 -in cert.p12 -out apns_cert.pem -nodes -legacy
```

### Where the device token comes from

After the app integrates APNs, the token is obtained in the `didRegisterForRemoteNotificationsWithDeviceToken` callback (a 64+ character hex string on iOS 13+). Your business service stores it in a roster/user table and passes it in as the recipient at delivery time. Herald does not fetch tokens from APNs.

## Sending Your First Message

Configure as below and start with `heraldd serve --config config.yaml` (**a device token recipient is required**):

```yaml
# config.yaml append (the three token-auth fields come from the application steps above)
providers:
  apns:
    type: apns
    enabled: true
    config:
      key_id: "ABC123DEFG"
      team_id: "ABCDE12345"
      private_key: "$APNS_PRIVATE_KEY"   # full PKCS#8 PEM content of the .p8 file, see Environment Variables
      topic: "com.example.app"
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order.notice",
    "level": "info",
    "title": "Order shipped",
    "body": "Your order SF123456 has shipped",
    "channels": ["apns"],
    "recipients": {"apns": ["device-token-hex-string"]}
  }'
```

The phone's notification center shows "Order shipped". Didn't arrive? Check whether the token is valid and the app is in a pushable state; see the [Troubleshooting Guide](/guide/troubleshooting) and [Common Errors](#common-errors) below.

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `topic` | ✅ | The app Bundle ID (append `.voip` for VoIP push, etc., per APNs rules) | None |
| `key_id` | Either one | APNs Key ID (token authentication) | None |
| `team_id` | Either one | Apple Team ID (token authentication) | None |
| `private_key` | Either one | The `.p8` private key, full PKCS#8 PEM content (token authentication) | None |
| `cert_pem` | Either one | The certificate chain + private key, full PEM content (certificate authentication) | None |
| `push_type` | ❌ | Overrides the auto-derived `apns-push-type` (`voip` / `liveactivity` / `location`, etc.) | Derived from content |
| `priority` | ❌ | Overrides the auto-derived `apns-priority` (10=immediately, 5=power saving) | Derived from type |
| `endpoint` | ❌ | Endpoint; use `https://api.sandbox.push.apple.com` for the sandbox | `https://api.push.apple.com` |

Provider creation fails if any of `key_id` / `team_id` / `private_key` is missing for token authentication, if `cert_pem` is missing for certificate authentication, or if `topic` is missing (`apns: key_id is required`, etc.); this is visible in the startup logs. The private key must be on the **P-256** curve (the `.p8` file already is); a wrong curve yields `apns: private_key must be a P-256 EC key`.

## Configuration Example

```yaml
providers:
  apns:
    type: apns
    enabled: true
    config:
      key_id: "ABC123DEFG"
      team_id: "ABCDE12345"
      private_key: "$APNS_PRIVATE_KEY"
      topic: "com.example.app"
      # endpoint: "https://api.sandbox.push.apple.com"  # development sandbox
```

Certificate authentication (either one — replace `key_id`/`team_id`/`private_key`):

```yaml
    config:
      topic: "com.example.app"
      cert_pem: "$APNS_CERT_PEM"   # certificate chain + private key PEM, converted with openssl
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `APNS_KEY_ID` | `key_id` | APNs Key ID (conventional name in `.env.example`) |
| `APNS_TEAM_ID` | `team_id` | Apple Team ID |
| `APNS_PRIVATE_KEY` | `private_key` | Full PKCS#8 PEM content of the `.p8` file (including real newlines) |
| `APNS_CERT_PEM` | `cert_pem` | Full PEM content of the certificate chain + private key (certificate authentication) |

> ⚠️ The newlines inside the private key must be **real newline characters**. When copying from a file, keep the `-----BEGIN PRIVATE KEY-----` header and footer lines and the internal newlines, or put the whole thing into an environment variable.

## Message Templates and Limits

### Content Mapping

| Scenario | push-type | priority | body structure |
|------|-----------|----------|-----------|
| Notification with title/body | `alert` | 10 (immediately) | `aps.alert` + `aps.sound=default` |
| Pure data payload | `background` | 5 (power saving) | `aps.content-available=1` + custom top-level keys |
| `push_type`/`priority` configured | Explicit override | Explicit override | As above |

- Keys of the Raw payload go at the JSON **top level** (outside `aps`); duplicate `aps` keys are ignored so they cannot overwrite the computed segment
- Content and Raw — at least one is required, otherwise you get `apns: payload needs content or raw data`

### Delivery Semantics

- Recipients are sent to one device token at a time; partial failure is aggregated into `apns: N/M succeeded - failed: ...` (consistent with FCM / WeChat official accounts)
- The token-authentication JWT is cached in memory and re-signed 5 minutes before expiry
- 4KB maximum per message (APNs platform limit)

### Capability Declaration

- `PayloadKinds`: `Content`, `Raw`
- `ContentFormats`: `plain`

### Retry Semantics

- Request timeouts, 408/429/5xx are wrapped by `httpclient` as retryable errors and go through unified retry
- 400 (`BadDeviceToken`, etc.) and 410 (`Unregistered`) are deterministic errors and are not retried

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `apns: topic is required` | Bundle ID not configured |
| `apns: key_id/team_id/private_key is required` | Missing token-auth fields (and `cert_pem` is not configured either) |
| `apns: private_key is not valid PEM data` | The private key is not PEM (the header/footer lines were lost during copying) |
| `apns: private_key must be an EC key` | Wrong key (not an EC key) |
| `apns: private_key must be a P-256 EC key` | Not the P-256 curve; APNs only accepts P-256 |
| `apns: cert_pem has no CERTIFICATE block` | The certificate-authentication PEM contains no certificate block |
| `apns: cert_pem: ...` | Certificate and private key do not match (X509KeyPair validation failed) |
| `{"reason":"BadDeviceToken"}` (400) | The device token is invalid, or it came from the sandbox but hit the production endpoint (or vice versa); verify `endpoint` against the token's origin |
| `{"reason":"TopicDisallowed"}` (400) | `topic` does not match the App ID of the certificate/JWT |
| `{"reason":"Unregistered"}` (410) | The token is no longer valid (app uninstalled / token rotated); remove it from the recipients |
| `{"reason":"ExpiredProviderToken"}` (403) | A large clock skew made the JWT expire — calibrate the server clock |
| `{"reason":"InvalidProviderToken"}` (403) | The `.p8` private key does not match `key_id`, or the Key has been revoked |
| `{"reason":"TooManyRequests"}` (429) | Rate limit exceeded; goes through unified retry |

Error format: non-2xx HTTP responses return `unexpected status code: {code}, body: {APNs JSON}` (the body contains `reason`). For overall troubleshooting, see the [Troubleshooting Guide](/guide/troubleshooting).

## Security Recommendations

1. **The `.p8` file can only be downloaded once**: store it in a secrets-management system; if lost, you must revoke and recreate it on the Keys page (the Key ID will change)
2. **Least privilege**: tick only APNs when creating the Key
3. **Certificate authentication caveats**: the `.p12`/PEM contains the private key and is equivalent to a credential; keep development and production certificates in separate environments
4. **Cleaning up invalid tokens**: a 410 `Unregistered` means the token is deregistered; continuous re-delivery is pointless — remove it from your audience

## Next Steps

- [Provider Overview](./overview.md) - see all Providers and how to enable them
- [FCM](./fcm.md) - Android/Web push channel
- [Troubleshooting Guide](/guide/troubleshooting) - startup failure, message not delivered, retries not taking effect
