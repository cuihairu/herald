# SMS Providers

Herald supports integration with the mainstream Chinese SMS vendors.

## Supported vendors

| Vendor | Provider name | Description |
|--------|--------------|------|
| Alibaba Cloud | `aliyunsms` | Alibaba Cloud SMS service |
| Tencent Cloud | `tencentsms` | Tencent Cloud SMS service |
| NetEase Yunxin | `neteasesms` | NetEase Yunxin SMS service |

## Configuration

### Alibaba Cloud SMS

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
```

### Tencent Cloud SMS

```yaml
providers:
  tencentsms:
    type: tencentsms
    enabled: true
    config:
      secret_id: "$TENCENT_SECRET_ID"
      secret_key: "$TENCENT_SECRET_KEY"
      app_id: "your SMS app ID"
      region: "ap-guangzhou"
```

### NetEase Yunxin SMS

```yaml
providers:
  neteasesms:
    type: neteasesms
    enabled: true
    config:
      app_key: "$NETEASE_APP_KEY"
      app_secret: "$NETEASE_APP_SECRET"
```

## Usage examples

### Send a template SMS

SMS Providers only support vendor template payloads — sending `title`/`body` directly gets rejected by the planner (`no compatible payload kind`). First define a template with a Binding in your config:

```yaml
templates:
  verify_code:
    name: "verification code"
    fields:
      - label: "code"
        value: "{{.Code}}"
    bindings:
      aliyunsms:
        template_code: "SMS_123456789"
        params:
          code: "code"
```

Then send:

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "template": "verify_code",
    "params": {"Code": "123456"},
    "channels": ["aliyunsms"],
    "recipients": {"aliyunsms": ["+8613800138000"]}
  }'
```

### Send via template Binding

Configure Bindings in the template definition and Herald maps template fields to vendor parameters automatically:

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "template": "verify_code",
    "params": {
      "Code": "123456",
      "Product": "Herald"
    },
    "channels": ["tencentsms"],
    "recipients": {
      "tencentsms": ["+8613800138000"]
    }
  }'
```

### Send SMS via a Worker

The Worker SDK lets you send SMS more flexibly:

```go
task := &core.DeliveryTask{
    Provider: "aliyunsms",
    Targets:  []string{"+8613800138000"},
    Payload: core.DeliveryPayload{
        Kind: core.PayloadProviderTemplate,
        ProviderTemplate: &core.ProviderTemplatePayload{
            TemplateCode: "SMS_123456789",
            Params: map[string]string{
                "code": "123456",
                "time": "5",
            },
        },
    },
}
```

## Template parameter notes

Configure the field-to-vendor-parameter mapping via Template Binding:

### Alibaba Cloud

- `template_code`: SMS template CODE (the `template_code` field of the Binding)
- `params`: named parameter mapping (the `params` field of the Binding, `field_label → vendor_key`)

### Tencent Cloud

- `template_id`: template ID (the `template_id` field of the Binding)
- `param_order`: ordered parameter array (the `param_order` field of the Binding)

### NetEase Yunxin

- `template_id`: template ID (the `template_id` field of the Binding)
- `param_order`: ordered parameter array (the `param_order` field of the Binding)

## Notes

1. **Phone number format**:
   - Mainland Chinese numbers: `13800138000` or `+8613800138000` both work
   - International numbers: must start with `+`, e.g. `+1234567890`
   - Multiple numbers: separate with commas

2. **Template review**:
   - SMS templates must first pass review on the vendor platform
   - Variable parameters must match the template

3. **Signature**:
   - Alibaba Cloud requires a signature name
   - The signature must pass review

4. **Rate limits**:
   - Configure a rate limiter to avoid going over the limit
   - Herald uses Token Bucket rate limiting by default

5. **Enable/disable**:
   - Can be toggled dynamically via the Dashboard or the API
   - A disabled Provider sends no SMS

## Error handling

The three SMS Providers share the same network/HTTP-layer classification; business-code failures differ by channel:

| Error type | Retried? | Notes |
|----------|----------|------|
| Network timeout / transport failure | Yes | classified `timeout`/`temporary`, retried with exponential backoff |
| HTTP 429 / 5xx | Yes | classified `rate_limited`/`temporary` |
| HTTP 200 but business-code failure | Varies by channel | Tencent Cloud/NetEase Yunxin wrap business errors with `WithRetry` (business-code failures are retried too); Alibaba Cloud returns a bare error (no retry) |
| Missing configuration | No | errors immediately at Provider creation |

## Best practices

1. **Manage with environment variables**: keep secrets in a `.env` file
2. **Multiple vendors**: configure several SMS Providers for higher availability
3. **Routing rules**: pick different vendors by level
4. **Dedup configuration**: avoid duplicate sends
