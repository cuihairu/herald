# Webhook (generic HTTP callback)

POST/PUT notifications as JSON to any HTTP endpoint.

## What it does

The `webhook` Builtin Provider wraps Herald notifications into a unified JSON payload and sends it to the configured `url`. Good for wiring up self-hosted alerting platforms, IM bots, CI/CD systems, Serverless functions — any HTTP receiver.

## Get credentials

Nothing to apply for — the receiver is **your own** HTTP service. To try it at zero cost first, you can:

- generate a temporary URL with an online debugging service like [webhook.site](https://webhook.site), or
- start a local listener: `nc -l 8899` / write your own receive route

For production, just swap in your alerting platform, Serverless function, or internal service address.

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
    "channels": ["webhook"]
  }'
```

The receiver gets a `POST` whose body looks like:

```json
{
  "id": "task-uuid",
  "provider": "webhook",
  "level": "error",
  "title": "Herald first push",
  "body": "from curl",
  "timestamp": "2026-10-02T12:00:00+08:00"
}
```

Nothing arrived? Check the receiver's logs plus the [troubleshooting guide](/guide/troubleshooting).

## Configuration options

| Key | Required | Description | Default |
|----|------|------|--------|
| `url` | ✅ | target HTTP endpoint (full URL, scheme included) | none |
| `method` | ❌ | HTTP method: `POST`, `PUT`, `GET`, `DELETE` | `POST` |
| `headers` | ❌ | custom request headers (`map[string]string`) | empty |

If `url` is missing, Provider creation fails immediately (`webhook: url is required`).

> ⚠️ `headers` is a `map[string]string`; see below for the YAML spelling. **headers do not take part in environment variable expansion** (`ExpandEnv` only handles top-level string values) — write literal values directly or inject them at the application layer.

## Configuration example

```yaml
providers:
  webhook:
    type: webhook
    enabled: true
    config:
      url: "$WEBHOOK_URL"
      method: "POST"                       # optional: POST / PUT / GET / DELETE
      headers:                             # optional: custom request headers
        Authorization: "Bearer your-token" # literal value, environment variables not expanded
        X-Source: "herald"
        Content-Type: "application/json"   # POST/PUT already defaults to application/json
```

## Environment variables

When loading config, Herald replaces string values in provider config **that start with `$`** with the value of the environment variable of the same name (the `$VAR` form, looked up as `VAR`). Note: the braced form `"${VAR}"` **is not expanded** (it is looked up as `{VAR}` and kept as-is); use `$VAR`.

| Environment variable | Corresponding config key | Description |
|----------|-----------|------|
| `WEBHOOK_URL` | `url` | target HTTP endpoint (the conventional name in `.env.example`) |

> ⚠️ `method` and `headers` usually carry nothing sensitive and can be written directly in the config file.

## Message template and limits

### Payload structure (WebhookPayload)

```json
{
  "id": "task-uuid",
  "provider": "webhook",
  "level": "warning",
  "targets": ["group:ops", "user:alice"],
  "timestamp": "2026-09-28T12:34:56+08:00",
  "title": "Server alert",
  "body": "CPU usage exceeds 90%",
  "raw": { "custom": "field" }
}
```

| Field | Source |
|------|------|
| `id` | `task.ID` |
| `provider` | `task.Provider` |
| `level` | `task.Level` |
| `targets` | `task.Targets` |
| `timestamp` | `task.CreatedAt` (RFC3339) |
| `title` | `task.Payload.Content.Title` |
| `body` | `task.Payload.Content.Body` |
| `raw` | `task.Payload.Raw` (passes raw fields through) |

### HTTP behavior

- **POST / PUT**: send the JSON above as `application/json`; `Content-Type: application/json` is set automatically (overridable via `headers`)
- **DELETE**: same as POST/PUT, sends the JSON body as `application/json`
- **GET**: no request body; the payload is flattened into URL query parameters — `id`/`provider`/`level`/`timestamp`/`title`/`body` are included only when non-empty, `targets` is comma-joined, and `raw` is serialized to a JSON string; if the target URL already has a `?`, parameters are appended with `&`
- `headers` are sent as request headers on all four methods (the default `Content-Type` can be overridden)
- `method` only accepts `GET`/`POST`/`PUT`/`DELETE` (case-insensitive); anything else is rejected at Provider creation (`webhook: method must be one of GET, POST, PUT, DELETE`)
- The response body is not parsed; only a response log is recorded (`httpclient.LogResponse`)
- Request timeouts and 408/429/5xx are wrapped by `httpclient` as retryable errors and go through unified retry

### Limits

- No signature / anti-replay mechanism (if you need security, validate `Authorization` or an IP allowlist on the receiver)
- Payload size is bounded by HTTP client/server limits (keeping each message under 1MB is recommended)

### Capability declaration

- `PayloadKinds`: `Content`, `Raw`
- `ContentFormats`: `json`

## Common errors

| Error | Cause and fix |
|------|-----------|
| `webhook: url is required` | `url` is not configured |
| `webhook: method must be one of GET, POST, PUT, DELETE` | `method` is set to an unsupported value (e.g. `PATCH`); use one of the four methods |
| `unexpected status code: 401` | the target endpoint returned 401; check `headers.Authorization` |
| `unexpected status code: 403` | the target endpoint returned 403; check the IP allowlist / signature validation |
| `unexpected status code: 404` | wrong path in `url` |
| `unexpected status code: 500` | internal error on the target endpoint; check its logs |
| `context deadline exceeded` | request timed out (default 30s); check the network / target endpoint performance |
| `connection refused` | target host/port unreachable |

Error format notes: non-2xx HTTP returns `unexpected status code: {code}, body: {response body}`; network/timeout errors are wrapped by `httpclient`.

## Security recommendations

1. **HTTPS**: production must use an `https://` endpoint
2. **Authentication**: configure `Authorization: Bearer <token>` or `X-Api-Key` in `headers` and have the receiver validate it
3. **IP allowlist**: restrict the receiver to the IP range Herald runs in
4. **Idempotency**: dedup by `id` on the receiver (Herald retries carry the same `id`)

## Next steps

- [Provider overview](./overview.md) - see all Providers and how to enable them
- [Email](./email.md) - SMTP email channel
- [Log](./log.md) - local debugging output
