> English · [中文](./log.md)

# Log (Local Log Output)

Write notifications directly to stdout / structured logs, for development debugging, test verification, and local loopback.

## What It Does

The `log` Builtin Provider sends no network requests at all — it only records the notification content as a structured entry via `logger.Info` and prints it to stdout with `fmt.Printf`. Good for:

- Local development / unit-test verification of notification flow
- Capturing notification content in a CI pipeline for assertions
- Acting as a "fallback Provider" together with the rule engine's `inhibit`/`silence` to observe inhibited notifications
- Local loopback testing in Worker mode

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `name` | ❌ | The Provider instance name (to distinguish multiple instances in logs) | `log` |

There are no required keys; creation succeeds even with an empty config.

## Configuration Example

```yaml
providers:
  log:
    type: log
    enabled: true
    config:
      name: "debug-log"   # optional: distinguish multiple log providers
```

```yaml
# minimal config
providers:
  log:
    type: log
    enabled: true
    config: {}
```

## Environment Variables

There are no sensitive configuration keys, so they are usually written in place. If you want to set the name via an environment variable:

```yaml
config:
  name: "$LOG_PROVIDER_NAME"  # optional
```

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

## Message Templates and Limits

### Output Format

**Structured log** (`logger.Info`, JSON format, pairs with `zap` / a production logging system):

```json
{
  "level": "info",
  "ts": "2026-09-28T12:34:56.789+0800",
  "msg": "delivering task",
  "provider": "debug-log",
  "task_id": "uuid-xxx",
  "title": "Server Alert",
  "body": "CPU usage above 90%",
  "level": "warning"
}
```

**Console print** (`fmt.Printf`, human-readable):

```
[warning] Server Alert: CPU usage above 90%
```

Format: `[{task.Level}] {title}: {body}`

### Limits

- **No network requests at all** — no network errors, timeouts, or retries
- No persistence, no dedup, no rate limiting
- `Deliver` always returns `nil` (except when `task == nil`, which returns `task is nil`)

### Capability Declaration

- `PayloadKinds`: `Content`
- `ContentFormats`: `plain`

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `task is nil` | The passed-in `DeliveryTask` is nil (a caller bug) |

Apart from `task is nil`, no other error is returned.

## Typical Usage

### 1. Local Development Verification

```yaml
providers:
  log:
    type: log
    enabled: true
```

Start Herald, send a notification, and see the output directly in the terminal.

### 2. Distinguishing Multiple Instances

```yaml
providers:
  log-debug:
    type: log
    enabled: true
    config:
      name: "debug"
  log-audit:
    type: log
    enabled: true
    config:
      name: "audit"
```

The `provider` field in the logs is `debug` / `audit` respectively, which makes filtering easy.

### 3. Rule Engine Observation Mode

Combined with `shadow` mode or an `inhibit` rule, route inhibited/silenced notifications to the log provider as well, as an audit trail:

```yaml
rules:
  - id: "silence-night"
    inhibit:
      time: "22:00-08:00"
    route:
      - provider: "log"   # night-time notifications skip real channels and only go to the log
```

## Sending Your First Message

`log` is the only zero-credential channel and the standard way to verify the Herald delivery path (the same steps are in step 1 of the [Getting Started](/guide/getting-started.en) guide):

```bash
# start
./bin/heraldd serve --config config.yaml

# send
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{"type":"demo","level":"error","title":"Node Offline","body":"node-17 is offline","channels":["log"]}'
```

The server's stdout immediately shows two lines — the structured log plus the human-readable delivery line:

```
{"time":"...","level":"INFO","msg":"delivering task","provider":"log","task_id":"...","title":"Node Offline","body":"node-17 is offline","level":"error"}
[error] Node Offline: node-17 is offline
```

## Next Steps

- [Provider Overview](./overview.en.md) - see all Providers and how to enable them
- [Webhook](./webhook.en.md) - a custom HTTP receiver
- [Email](./email.en.md) - the SMTP email channel
