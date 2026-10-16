> English · [中文](./fcm.md)

# FCM (Firebase Cloud Messaging)

Send push notifications to Android / iOS / Web devices via the Firebase Cloud Messaging HTTP v1 API.

## What It Does

The `fcm` Builtin Provider authenticates with a Firebase **service account**: it signs an RS256 JWT with `client_email` + the RSA private key, exchanges it at the Google OAuth endpoint for a short-lived access token (cached in memory, refreshed automatically 5 minutes before expiry), and then calls FCM v1 `messages:send` with a Bearer token. Each delivery target is an **FCM registration token**, which suits app push, device alerts, and similar scenarios.

## Obtaining Credentials

Everything is done in the [Firebase console](https://console.firebase.google.com/):

1. **Create/select the project**: the **Project ID** in project settings is your `project_id` (like `my-firebase-project`)
2. **Generate a private key**: **Project settings ⚙️ → Service accounts → Generate new private key**, and download the JSON file
3. **Extract three fields**:
   - `project_id` → the JSON's `project_id`
   - `client_email` → the JSON's `client_email` (like `firebase-adminsdk-xxx@<project>.iam.gserviceaccount.com`)
   - `private_key` → the JSON's `private_key` (PKCS#8 PEM, starting with `-----BEGIN PRIVATE KEY-----`)

> ⚠️ The service account needs the `firebase.messaging` permission (already included in Firebase's default service account). The app must integrate the Firebase SDK and pass the registration token to Herald as a recipient.

## Sending Your First Message

Configure as below and start with `heraldd serve --config config.yaml` (**a registration token recipient is required**):

```yaml
# config.yaml append (the three fields come from the JSON above)
providers:
  fcm:
    type: fcm
    enabled: true
    config:
      project_id: "my-firebase-project"
      client_email: "firebase-adminsdk-xxx@my-firebase-project.iam.gserviceaccount.com"
      private_key: "$FCM_PRIVATE_KEY"   # put the full PEM content in an environment variable, see Environment Variables
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "device.alert",
    "level": "error",
    "title": "磁盘告警",
    "body": "/dev/sda1 使用率 95%",
    "channels": ["fcm"],
    "recipients": {"fcm": ["cX618eW0VP6:APA91bH..."]}
  }'
```

The device notification center shows "磁盘告警 / /dev/sda1 使用率 95%" (Disk alert / /dev/sda1 usage 95%). Didn't arrive? Did the app get a token, and was the token passed correctly? See the [Troubleshooting Guide](/guide/troubleshooting.en) and [Common Errors](#common-errors) below.

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `project_id` | ✅ | The Firebase project ID | None |
| `client_email` | ✅ | The service account email (the JSON's `client_email`) | None |
| `private_key` | ✅ | The service account RSA private key (full PKCS#8 PEM content) | None |
| `endpoint` | ❌ | The FCM API endpoint (override for testing/proxy scenarios) | `https://fcm.googleapis.com` |

If any required field is missing or the private key cannot be parsed, Provider creation fails immediately (`fcm: project_id is required`, etc.); this is visible in the startup logs. Only PKCS#8 (`PRIVATE KEY`) PEM private keys are accepted — the JSON downloaded from Firebase is in that format; a non-RSA key yields `fcm: private_key must be an RSA key`.

## Configuration Example

```yaml
providers:
  fcm:
    type: fcm
    enabled: true
    config:
      project_id: "my-firebase-project"
      client_email: "firebase-adminsdk-xxx@my-firebase-project.iam.gserviceaccount.com"
      private_key: "$FCM_PRIVATE_KEY"       # recommended: put the full PEM content in an environment variable
      # endpoint: "https://fcm.googleapis.com"  # optional, usually not needed
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `FCM_PROJECT_ID` | `project_id` | The Firebase project ID |
| `FCM_CLIENT_EMAIL` | `client_email` | The service account email |
| `FCM_PRIVATE_KEY` | `private_key` | Full PKCS#8 PEM content (including real newlines) |

> ⚠️ The newlines inside `private_key` must be **real newline characters**. If copying from JSON produces `-----BEGIN PRIVATE KEY-----\n...` (with a literal `\n`), you must replace it with real newlines first, or put the whole thing into an environment variable and let shell/YAML handle it.

## Message Templates and Limits

- `Content` (title/body, plain text) → the FCM `notification` segment, rendered by the system notification area
- `Raw` payload → the FCM `data` segment (values are stringified uniformly, since FCM data only supports strings)
- When both are omitted, a pure data message is sent; FCM requires at least one of the two
- Recipients (registration tokens) are sent to one token at a time; partial failure is aggregated into a single error (`fcm: 1/2 succeeded - failed: ...`), matching the partial-success semantics of the other Providers
- 4KB maximum per token message (FCM platform limit)

### Capability Declaration

- `PayloadKinds`: `Content`, `Raw`
- `ContentFormats`: `plain`

### Retry Semantics

- Request timeouts, 408/429/5xx are wrapped by `httpclient` as retryable errors and go through unified retry
- Other 4xx (404 for an unregistered token, 400 for an invalid payload, etc.) are deterministic errors and are not retried
- Network/5xx failures while exchanging the OAuth token are likewise retryable errors

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `fcm: project_id/client_email/private_key is required` | Missing config fields |
| `fcm: private_key is not valid PEM data` | The private key is not PEM (the header/footer lines may have been lost during copying) |
| `fcm: private_key must be an RSA key` | The private key is not RSA (check that you did not grab the wrong key file) |
| `fcm: token error: invalid_grant` | JWT exchange failed: the private key does not match `client_email`, the clock skew is too large, or the key has been revoked — regenerate the service account key |
| `unexpected status code: 401/403` | The access token is invalid, or FCM is not enabled for the project |
| `unexpected status code: 404, body: ... NOT_FOUND` | The registration token is no longer valid (app uninstalled / token rotated); remove it from the recipients |
| `unexpected status code: 400, body: ... INVALID_ARGUMENT` | Invalid payload (over 4KB, both `notification` and `data` empty, etc.) — read the `message` in the body |

Error format notes: non-2xx HTTP responses return `unexpected status code: {code}, body: {response body}`; FCM's structured error is in the body's `error.message` / `error.status`. For overall troubleshooting, see the [Troubleshooting Guide](/guide/troubleshooting.en).

## Security Recommendations

1. **The private key is a credential**: the service account private key can send pushes on behalf of the project — always keep it in an environment variable or a secrets-management system, and never commit it to the repository
2. **Least privilege**: create a dedicated service account for Herald and keep only messaging permissions
3. **Key rotation**: if it is leaked, delete the old key in the Firebase console and regenerate it, then update the config accordingly
4. **Cleaning up invalid tokens**: a 404 `NOT_FOUND` means the token is deregistered; continuous re-delivery is pointless — remove it from your audience

## Next Steps

- [Provider Overview](./overview.en.md) - see all Providers and how to enable them
- [Webhook](./webhook.en.md) - a custom HTTP receiver
- [Troubleshooting Guide](/guide/troubleshooting.en) - startup failure, message not delivered, retries not taking effect
