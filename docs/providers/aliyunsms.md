# Aliyun SMS

Send domestic and international SMS via the Aliyun SMS service. Suited to verification codes, notifications, marketing, and similar scenarios.

## What It Does

The `aliyunsms` Builtin Provider calls the Aliyun SMS API (`dysmsapi.aliyuncs.com/SendSms`) directly, sending template SMS with the RPC signing mechanism (HMAC-SHA1). It supports batch sending to multiple numbers, template parameter mapping, and signature configuration.

> ⚠️ **Prerequisites**: You must enable the Aliyun SMS service, complete real-name verification, and have your SMS signature and templates applied for and approved.

## Obtaining Credentials

SMS is a heavily reviewed channel in China. Four steps (all done in the Aliyun console):

1. **Enable the service**: search the console for "SMS Service" and enable it; the account must have completed real-name verification
2. **Apply for a signature**: Domestic messages → Signature management → Add signature (individuals can use an app or WeChat official account name, etc.; enterprise qualifications are approved faster). You cannot send until the review passes
3. **Apply for a template**: Template management → Add template (verification code / notification category) to obtain the **template CODE** (like `SMS_123456789`). The placeholder `${code}` in the template corresponds to the send parameters
4. **AccessKey**: RAM Access Control → Create user → grant `AliyunDysmsFullAccess` → generate the AccessKey ID / Secret

## Sending Your First Message

SMS **must go through a template** (this Provider does not support sending `title`/`body` directly). Configure a template plus parameters, then send:

```yaml
# config.yaml append
templates:
  verify_code:
    name: "verification code"
    level: "info"
    template_code: "SMS_123456789"
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
    "channels": ["aliyunsms"],
    "recipients": {"aliyunsms": ["13800000000"]}
  }'
```

Your phone receives `【Your Signature】Your verification code is 884275...` (the text = your template). Didn't arrive? Check the signature/template review status, balance, and error codes — see [Common Errors](#common-errors) and the [Troubleshooting Guide](/guide/troubleshooting).

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `access_key_id` | ✅ | Aliyun AccessKey ID | None |
| `access_key_secret` | ✅ | Aliyun AccessKey Secret | None |
| `sign_name` | ✅ | SMS signature name (must be approved in the Aliyun console) | None |
| `region` | ❌ | Region ID | `cn-hangzhou` |
| `endpoint` | ❌ | API endpoint | `dysmsapi.aliyuncs.com` |
| `enabled` | ❌ | Whether enabled | `true` |

Missing `access_key_id`, `access_key_secret`, or `sign_name` makes Provider creation fail immediately (`aliyunsms: access_key_id is required`, etc.); this is visible in the startup logs.

## Configuration Example

```yaml
providers:
  aliyunsms:
    type: aliyunsms
    enabled: true
    config:
      access_key_id: "$ALIYUN_ACCESS_KEY_ID"
      access_key_secret: "$ALIYUN_ACCESS_KEY_SECRET"
      sign_name: "Your Signature"
      region: "cn-hangzhou"
      endpoint: "dysmsapi.aliyuncs.com"
```

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `ALIYUN_ACCESS_KEY_ID` | `access_key_id` | Aliyun AccessKey ID |
| `ALIYUN_ACCESS_KEY_SECRET` | `access_key_secret` | Aliyun AccessKey Secret |

## Message Templates and Limits

### Sending Modes

Only **Provider Template** mode is supported (`PayloadKind: ProviderTemplate`). You must supply, in the task or the template Binding:

- `template_code`: the Aliyun SMS template CODE (e.g. `SMS_123456789`)
- `params`: a named parameter map (`map[string]string`, field label → template variable)

### Phone Number Format

- Numbers are sent to Aliyun as-is; Herald does not pad a country prefix (only the Tencent Cloud Provider auto-prepends `+86`)
- Multiple numbers: pass several in the task's `targets` field; they are joined with commas internally

### Capability Declaration

- `PayloadKinds`: `provider_template`
- `SupportsTemplate`: `true`

### Limitations

| Limitation | Description |
|--------|------|
| **Template review** | Templates must be approved in the Aliyun console |
| **Signature review** | The signature must be approved and must be specified when sending |
| **Rate limiting** | Defaults to 1 per minute, 5 per hour, 10 per day per number (plan dependent) |
| **Content length** | A single message ≤ 70 characters (including the signature); long SMS is billed at 67 characters per message |
| **Retry** | Network-layer errors go through unified retry; business codes such as `isv.BUSINESS_LIMIT_CONTROL` return a bare error and are not retried |

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `aliyunsms: access_key_id is required` | `access_key_id` not configured |
| `aliyunsms: access_key_secret is required` | `access_key_secret` not configured |
| `aliyunsms: sign_name is required` | `sign_name` not configured |
| `phone numbers are required` | `targets` is empty when sending |
| `aliyun SMS error: isp.RAM_PERMISSION_DENY` | The AccessKey lacks SMS permission; grant `AliyunDysmsapiFullAccess` |
| `aliyun SMS error: isv.SMS_SIGN_ILLEGAL` | The signature is not approved or does not exist |
| `aliyun SMS error: isv.SMS_TEMPLATE_ILLEGAL` | Wrong template CODE, or the template is not approved |
| `aliyun SMS error: isv.MOBILE_NUMBER_ILLEGAL` | Malformed phone number |
| `aliyun SMS error: isv.DAY_LIMIT_CONTROL` | Daily send limit reached |
| `aliyun SMS error: isv.BUSINESS_LIMIT_CONTROL` | Rate limit reached; reduce the send frequency |

Error format: `aliyun SMS error: {Code} - {Message}`; network/HTTP errors go through unified retry (429/5xx).

## Next Steps

- [Provider Overview](./overview.md) - see all Providers and how to enable them
- [Tencent Cloud SMS](./tencentsms.md) - the Tencent Cloud SMS channel
- [NetEase YunXin SMS](./neteasesms.md) - the NetEase YunXin SMS channel
- [SMS Providers](./sms.md) - comparison of the SMS Providers
