# NetEase YunXin SMS

Send domestic and international SMS via the NetEase YunXin SMS service. Suited to verification codes, notifications, marketing, and similar scenarios.

## What It Does

The `neteasesms` Builtin Provider calls the NetEase YunXin SMS API (`api.netease.im/v1/sms/sendTemplateSms`) directly, sending template SMS with the SHA1 checksum mechanism (`sha1(appSecret + nonce + curTime)`). It supports template SMS as well as sending and verifying verification codes, which suits business scenarios that need verification codes.

> ⚠️ **Prerequisites**: You must enable the NetEase YunXin SMS service, create an app, and have an SMS template applied for and approved.

## Obtaining Credentials

Three steps (in the NetEase YunXin console):

1. **Create the app**: Console → Create App, obtaining the **AppKey / AppSecret**
2. **Enable SMS** and claim/apply for a signature (SMS feature → Signature Management)
3. **Apply for a template**: SMS → Template Management → New Template (approved after review); note down the **template ID**

## Sending Your First Message

SMS **must go through a template** (this Provider does not support sending `title`/`body` directly). Configure a template plus parameters, then send:

```yaml
# config.yaml append
templates:
  verify_code:
    name: "verification code"
    level: "info"
    template_id: "your NetEase Yunxin template ID"
    fields:
      - label: "code"
        value: "{{.Code}}"
```

After starting with `heraldd serve --config config.yaml`:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "user.verify",
    "level": "info",
    "template": "verify_code",
    "params": {"Code": "884275"},
    "channels": ["neteasesms"],
    "recipients": {"neteasesms": ["13800000000"]}
  }'
```

Your phone receives `【Signature】Your verification code is 884275...` (the text = your template). Didn't arrive? Template/signature review status and error codes are covered in [Common Errors](#common-errors) and the [Troubleshooting Guide](/guide/troubleshooting).

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `app_key` | ✅ | The NetEase YunXin AppKey | None |
| `app_secret` | ✅ | The NetEase YunXin AppSecret | None |
| `endpoint` | ❌ | API endpoint | `api.netease.im` |
| `enabled` | ❌ | Whether enabled | `true` |

Missing `app_key` or `app_secret` makes Provider creation fail immediately (`neteasesms: app_key is required` / `neteasesms: app_secret is required`); this is visible in the startup logs.

## Configuration Example

```yaml
providers:
  neteasesms:
    type: neteasesms
    enabled: true
    config:
      app_key: "$NETEASE_APP_KEY"
      app_secret: "$NETEASE_APP_SECRET"
      endpoint: "api.netease.im"
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `NETEASE_APP_KEY` | `app_key` | The NetEase YunXin AppKey |
| `NETEASE_APP_SECRET` | `app_secret` | The NetEase YunXin AppSecret |

## Message Templates and Limits

### Sending Modes

Only **Provider Template** mode is supported (`PayloadKind: ProviderTemplate`). You must supply, in the task or the template Binding:

- `template_id` / `template_code`: the NetEase YunXin SMS template ID (`template_id` is read first, falling back to `template_code`)
- `params` / `param_order`: an ordered parameter array (`[]string`, joined with commas internally)

### Phone Number Format

- Domestic numbers: `13800138000` or `+8613800138000` (kept as-is, no automatic prefix added)
- International numbers: must start with `+`, e.g. `+1234567890`
- Multiple numbers: pass several in the task's `targets` field; they are joined with commas internally

### Extended Capabilities

Besides the standard `Deliver`, the Provider exposes two methods for Worker/advanced usage:

- `SendCode(ctx, mobile, authCode, deviceID)` - send a verification code
- `VerifyCode(ctx, mobile, code)` - verify a verification code

### Capability Declaration

- `PayloadKinds`: `provider_template`
- `SupportsTemplate`: `true`

### Limitations

| Limitation | Description |
|--------|------|
| **Template review** | Templates must be approved in the NetEase YunXin console |
| **Rate limiting** | Plan dependent; configuring a rate limiter is recommended |
| **Content length** | A single message ≤ 70 characters; long SMS is billed at 67 characters per message |
| **Retry** | Network errors / rate limiting go through unified retry (429/5xx equivalents) |

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `neteasesms: app_key is required` | `app_key` not configured |
| `neteasesms: app_secret is required` | `app_secret` not configured |
| `phone numbers are required` | `targets` is empty when sending |
| `no valid phone numbers` | `targets` entries are all empty or blank strings |
| `template_id is required` | The task/template did not provide `template_id` |
| `neteasesms error: 403 - Forbidden` | Wrong AppKey/AppSecret, or the app is disabled |
| `neteasesms error: 414 - invalid parameters` | Malformed phone number, nonexistent template ID, or mismatched parameters |
| `neteasesms error: 416 - rate limit` | Rate limit reached; reduce the send frequency |
| `neteasesms error: 417 - insufficient balance` | The SMS balance is insufficient; top up |
| `neteasesms error: 420 - template not approved` | The template has not been reviewed, or the review did not pass |

Error format: `neteasesms error: {code} - {msg}`; network/HTTP errors go through unified retry (429/5xx).

## Next Steps

- [Provider Overview](./overview.md) - see all Providers and how to enable them
- [Aliyun SMS](./aliyunsms.md) - the Aliyun SMS channel
- [Tencent Cloud SMS](./tencentsms.md) - the Tencent Cloud SMS channel
- [SMS Providers](./sms.md) - comparison of the SMS Providers
