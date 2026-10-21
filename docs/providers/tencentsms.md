# Tencent Cloud SMS

Send domestic/international SMS via Tencent Cloud SMS, suitable for verification codes, notifications, marketing, and similar scenarios.

## What it does

The `tencentsms` Builtin Provider calls the Tencent Cloud SMS API directly (`sms.tencentcloudapi.com/SendSms`) and sends template SMS using the TC3-HMAC-SHA256 signing scheme (API v3). It supports batch sends to multiple numbers, ordered parameter arrays, and app ID and signature configuration.

> ⚠️ **Prerequisites**: you must enable Tencent Cloud SMS, complete real-name verification, create an SMS app, and apply for an SMS signature and templates that pass review.

## Get credentials

Four steps (Tencent Cloud console):

1. **Enable the service**: search for "SMS" in the console and enable it; the account must have completed real-name verification
2. **Create an app**: App Management → Create App; you get an **SDKAppID** (the `app_id` in the config, shaped like `1400000000`)
3. **Signature and body templates**: Domestic SMS → Signature Management / Body Template Management; submit each for review (signatures need supporting qualifications). Once approved, note the **body template ID** (shaped like `1234567`)
4. **API keys**: Access Management CAM → API Key Management → Create Key; you get a SecretId / SecretKey

## Send your first message

SMS **must go through templates** (this Provider does not support direct title/body); configure a template + parameters, then send:

```yaml
# append to config.yaml
templates:
  verify_code:
    name: "verification code"
    level: "info"
    template_id: "1234567"
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
    "channels": ["tencentsms"],
    "recipients": {"tencentsms": ["13800000000"]}
  }'
```

The phone receives `【Your Signature】Your verification code is 884275...` (the text = your template). Numbers are automatically prefixed with `+86`; nothing arrived? Check the signature/template review status and error codes under Common Errors and in the [troubleshooting guide](/guide/troubleshooting).

## Configuration options

| Key | Required | Description | Default |
|----|------|------|--------|
| `secret_id` | ✅ | Tencent Cloud SecretId | none |
| `secret_key` | ✅ | Tencent Cloud SecretKey | none |
| `app_id` | ✅ | SMS app ID (SDK AppID) | none |
| `sign_name` | ❌ | SMS signature content (must pass review in the console) | empty |
| `region` | ❌ | region | `ap-guangzhou` |
| `endpoint` | ❌ | API endpoint | `sms.tencentcloudapi.com` |
| `enabled` | ❌ | whether to enable | `true` |

If `secret_id`, `secret_key`, or `app_id` is missing, Provider creation fails immediately (`tencentsms: secret_id is required` etc.); it shows up in startup logs.

## Configuration example

```yaml
providers:
  tencentsms:
    type: tencentsms
    enabled: true
    config:
      secret_id: "$TENCENT_SECRET_ID"
      secret_key: "$TENCENT_SECRET_KEY"
      app_id: "1400000000"
      sign_name: "Your Signature"
      region: "ap-guangzhou"
      endpoint: "sms.tencentcloudapi.com"
```

## Environment variables

When loading config, Herald replaces string values in provider config **that start with `$`** with the value of the environment variable of the same name (the `$VAR` form, looked up as `VAR`). Note: the braced form `"${VAR}"` **is not expanded** (it is looked up as `{VAR}` and kept as-is); use `$VAR`.

| Environment variable | Corresponding config key | Description |
|----------|-----------|------|
| `TENCENT_SECRET_ID` | `secret_id` | Tencent Cloud SecretId |
| `TENCENT_SECRET_KEY` | `secret_key` | Tencent Cloud SecretKey |

## Message template and limits

### Send mode

Only the **Provider Template** mode is supported (`PayloadKind: ProviderTemplate`); provide these in the task or template Binding:

- `template_id`: Tencent Cloud SMS template ID (a number, e.g. `1234567`)
- `params` / `param_order`: ordered parameter array (`[]string`, filled in template-variable order)

### Phone number format

- Mainland Chinese numbers: `13800138000` or `+8613800138000` (the `+86` prefix is added automatically)
- International numbers: must start with `+`, e.g. `+1234567890`
- Multiple numbers: pass several in the task's `targets` field

### Capability declaration

- `PayloadKinds`: `provider_template`
- `SupportsTemplate`: `true`

### Limit notes

| Limit | Notes |
|--------|------|
| **Template review** | templates must pass review in the Tencent Cloud console |
| **Signature review** | the signature must pass review; `sign_name` takes the signature content, not an ID |
| **Rate limits** | by default 1 message per 30 seconds, 10 per hour, 30 per day for a single number (depends on your plan) |
| **Content length** | up to 70 characters per message (signature included); long SMS is billed at 67 characters per segment |
| **Retry** | network errors / rate limiting go through unified retry (429/5xx equivalent semantics) |

## Common errors

| Error | Cause and fix |
|------|-----------|
| `tencentsms: secret_id is required` | `secret_id` is not configured |
| `tencentsms: secret_key is required` | `secret_key` is not configured |
| `tencentsms: app_id is required` | `app_id` is not configured |
| `phone numbers are required` | `targets` was empty at send time |
| `template_id is required` | the task/template did not provide `template_id` |
| `tencent SMS error: FailedOperation.SignNotPass` | the signature has not passed review |
| `tencent SMS error: FailedOperation.TemplateNotPass` | the template has not passed review |
| `tencent SMS error: FailedOperation.MobileNumberNotExist` | the phone number is malformed or not in service |
| `tencent SMS error: FailedOperation.FrequencyLimit` | the contact-frequency limit was hit; send less frequently |
| `tencent SMS error: FailedOperation.InsufficientBalance` | insufficient SMS balance; top up |
| `tencent SMS error: AuthFailure.SecretIdNotFound` | the SecretId does not exist or is disabled |
| `tencent SMS error: AuthFailure.SignatureFailure` | signature computation failed; check the SecretKey and system clock |

Error format: `tencent SMS error: {Code} - {Message}` (top-level Error) or `tencent SMS error for {phone}: {Code} - {Message}` (per-number status); network/HTTP errors go through unified retry (429/5xx).

## Next steps

- [Provider overview](./overview.md) - see all Providers and how to enable them
- [Alibaba Cloud SMS](./aliyunsms.md) - Alibaba Cloud SMS channel
- [NetEase Yunxin SMS](./neteasesms.md) - NetEase Yunxin SMS channel
- [SMS Providers](./sms.md) - side-by-side comparison of SMS Providers
