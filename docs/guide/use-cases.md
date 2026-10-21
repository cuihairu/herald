# Use Cases and Integration

This page answers "**when should I use Herald, and how do I connect it to my business**". Each of the four typical scenarios gives: when to use it → how to configure → how to send → what it looks like when running. For how to apply for channel credentials see the corresponding [Provider Handbook](/providers/overview) page; here we assume you already have a working channel (if not, start with [Getting Started](/guide/getting-started)).

## Scenario 1: System alert push

**When to use it**: monitoring/inspection/scheduled jobs detect an anomaly and need to send the alert to different channels by severity: error pops up in the on-call group, warning stays as a group message, and low levels are only logged.

**How to connect**: configure the Provider for each channel, then declare routing by **level** with `level_routes`:

```yaml
providers:
  feishu:
    type: feishu
    enabled: true
    config:
      webhook_url: "$FEISHU_WEBHOOK_URL"
  log:
    type: log
    enabled: true
    config: {}

level_routes:
  error:   ["feishu"]
  warning: ["feishu"]
  info:    ["log"]
```

> There are two routing tables: `routes` matches by **notification type** (e.g. `server.alert`), and `level_routes` matches by **level**. If neither matches and the request carries no `channels`, the notification is rejected with `no route found`.

**How to send** — the monitoring system only needs to send one HTTP request:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{"type":"cpu.high","level":"error","title":"CPU usage 95%","body":"order-01 cpu 95%, lasting 5min"}'
```

Without `channels`, `level_routes` decides the destination; writing explicit `channels` overrides the routing table.

**What it looks like when running**: the Feishu group receives `[error] CPU usage 95%`; alert-type notifications can enable a Feishu interactive card with an "Acknowledge Alert" button (`interactive_cards: true`), and the escalation policy for unacknowledged timeouts is in the [Rule Engine](/design-rule-engine).

## Scenario 2: Operational message outreach

**When to use it**: product/operations needs to send an announcement or campaign notice to a batch of users or an on-call group — define the content once and distribute it to Feishu, WeCom, and email at multiple points.

**How to connect**: use a **template** to decouple content from channels. The <code v-pre>{{.xxx}}</code> in the template is exactly the key passed in `params` at send time:

```yaml
templates:
  ops_notice:
    name: "Operational Announcement"
    title: "{{.Title}}"
    level: "info"
    fields:
      - label: "Details"
        value: "{{.Body}}"
```

**How to send** — write multiple instance names in `channels` to distribute to multiple points in one call:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "ops.notice",
    "level": "info",
    "template": "ops_notice",
    "params": {
      "Title": "System maintenance Friday 18:00",
      "Body": "Expected 30 minutes; the console will be unavailable during this time"
    },
    "channels": ["feishu", "wecom", "email"],
    "recipients": {
      "email": ["alice@example.com", "bob@example.com"]
    }
  }'
```

> When you don't need a template you can omit `template`/`params` and pass content directly with the top-level `title`/`body` (Getting Started uses exactly this direct form).

**What it looks like when running**: all three channels receive the same message at the same time; the API returns the three channels in `accepted`. `recipients` only takes effect for channels that address by recipient (email recipients, SMS numbers, official-account OpenIDs); group-bot-style channels ignore it and send to the group fixed in their configuration.

**Fixing an audience into a group**: store the on-call group's roster as a named audience, and from then on any channel slot can just write `group:ops`; personnel changes change the group, not the business. For configuration and API hot updates see [Notification Groups](/design-notification-groups).

## Scenario 3: Verification codes and transactional SMS

**When to use it**: registration/login verification codes, order status, payment reminders, and other time-sensitive messages that must reach a phone.

**How to connect**: the SMS channel requires dual review of "signature + template"; complete the application in the cloud vendor's console first (Alibaba Cloud [application method](/providers/aliyunsms) / Tencent Cloud / NetEase Yunxin). Taking Alibaba Cloud as an example:

```yaml
providers:
  aliyunsms:
    type: aliyunsms          # note it is aliyunsms, not builtin
    enabled: true
    config:
      access_key_id: "$ALIYUN_ACCESS_KEY_ID"
      access_key_secret: "$ALIYUN_ACCESS_KEY_SECRET"
      sign_name: "your signature"
      region: "cn-hangzhou"

templates:
  verify_code:
    name: "Verification Code"
    level: "info"
    fields:
      - label: "code"
        value: "{{.Code}}"
    bindings:
      aliyunsms:
        template_code: "SMS_123456789"     # Alibaba Cloud template CODE
        params:
          code: "code"
```

**How to send**:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "user.verify",
    "level": "info",
    "template": "verify_code",
    "params": {"Code": "884275"},
    "channels": ["aliyunsms"],
    "recipients": {"aliyunsms": ["+8613800000000"]}
  }'
```

**What it looks like when running**: the phone receives an SMS of the form `【your signature】884275`; for differences in template parameters and number formats (the +86 prefix, etc.) see the [SMS Provider Overview](/providers/sms).

**Two key points for the verification-code scenario**:
- **Don't drop the message**: retryable errors like 408/429/5xx are retried automatically per the backoff policy (the `retry` config), so carrier flakiness won't silently swallow verification codes
- **Anti-abuse**: `rate_limit` caps a single Provider's send rate; combined with the `dedup` window it blocks duplicate floods; see the [Configuration Reference](/guide/configuration)

## Scenario 4: Multi-channel fallback and retry

**When to use it**: alerts must not bet on a single channel — if the IM group goes down or the bot is rate-limited, the message still has to get out.

**How to connect**: writing multiple channels in `channels` (or the routing table) means **fanning out at the same time**, with each channel delivering independently and not blocking the others:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "db.down",
    "level": "error",
    "title": "Primary database unreachable",
    "body": "mysql-primary heartbeat timed out",
    "channels": ["feishu", "telegram", "log"]
  }'
```

**What happens on failure** (semantics are consistent across all Providers):

1. A transient failure on a single channel (timeout/408/429/5xx) → the task is marked **retryable** and redelivered automatically per `retry`'s exponential backoff, up to `max` times
2. A deterministic error (invalid token, nonexistent number, and other 4xx-class errors) → no retry, recorded as a failure directly
3. The final result all lands in the [delivery logs](/guide/troubleshooting), where `accepted` / `failed` are clear at a glance:

```json
{"code":0,"message":"ok","data":{
  "notification_id":"...",
  "accepted":["feishu","log"],
  "failed":[{"channel":"telegram","error":"telegram API error: chat not found"}]
}}
```

**What it looks like when running**: as long as any one channel succeeds, the person gets the message; the errors of failed channels can be found and asserted on in the logs. Duplicate alerts within the same event's dedup window are folded by `dedup`; see the [Configuration Reference](/guide/configuration).

> If you want the cascading semantics where the backup is used **only** when the primary fails (rather than fanning out at the same time), express it with the rule engine's routing decisions (`rules` + rerouting); see the [Rule Engine Decision Layer](/design-rule-engine).

## Next Steps

- [Getting Started](/guide/getting-started) - not up and running yet? Start here
- [Configuration Reference](/guide/configuration) - full documentation of every configuration key that appeared above
- [Provider Handbook](/providers/overview) - credential application and first message for every channel
