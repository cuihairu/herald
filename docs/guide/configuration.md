# Configuration

## Configuration file

Herald uses a YAML configuration file (`config.yaml`).

## Run modes

Herald ships as a single binary; subcommands select the run mode:

```bash
# Scheduler mode (API + Queue + local workers)
heraldd serve --config config.yaml

# Remote worker mode (consumes tasks from the shared Queue)
heraldd worker --config worker.yaml
```

## Complete configuration example

### Scheduler configuration (scheduler.yaml)

```yaml
# Server configuration
server:
  addr: ":8080"
  timeout: 30s
  max_request_body_bytes: 1048576  # global default: 1 MiB
  request_body_limits:             # optional: tighten per-route limits by URL path glob
    "/api/v1/notify": 262144       # 256 KiB

# Provider configuration
providers:
  # Log provider (enabled by default)
  log:
    type: log
    enabled: true
    config:
      name: "log"

  # Telegram bot
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
      # Optional: self-hosted Bot API server or mirror (default https://api.telegram.org)
      # api_url: "https://my-bot-api.example.com"

  # Feishu
  feishu:
    type: feishu
    enabled: false
    config:
      webhook_url: "$FEISHU_WEBHOOK_URL"
      # Optional: card mode (send alerts as interactive cards carrying an "Acknowledge Alert" button,
      # requires the card_callback endpoint, see "Feishu card button callback (P3)")
      # interactive_cards: true

  # WeCom (WeChat Work)
  wecom:
    type: wecom
    enabled: false
    config:
      webhook_url: "$WECOM_WEBHOOK_URL"

  # DingTalk
  dingtalk:
    type: dingtalk
    enabled: false
    config:
      access_token: "$DINGTALK_ACCESS_TOKEN"
      secret: "$DINGTALK_SECRET"

  # Slack
  slack:
    type: slack
    enabled: false
    config:
      webhook_url: "$SLACK_WEBHOOK_URL"

  # Discord
  discord:
    type: discord
    enabled: false
    config:
      webhook_url: "$DISCORD_WEBHOOK_URL"
      # or use the bot API
      bot_token: "$DISCORD_BOT_TOKEN"
      channel_id: "$DISCORD_CHANNEL_ID"
      # Optional: self-hosted/proxied Bot API (default https://discord.com/api/v10)
      # api_url: "https://my-discord-api.example.com"

  # Email
  email:
    type: email
    enabled: false
    config:
      host: "smtp.gmail.com"
      port: 587
      username: "$EMAIL_USERNAME"
      password: "$EMAIL_PASSWORD"
      from: "$EMAIL_FROM"
      from_name: "Herald"

  # Webhook
  webhook:
    type: webhook
    enabled: false
    config:
      url: "$WEBHOOK_URL"
      method: "POST"

  # Aliyun SMS
  aliyunsms:
    type: aliyunsms
    enabled: false
    config:
      access_key_id: "$ALIYUN_ACCESS_KEY_ID"
      access_key_secret: "$ALIYUN_ACCESS_KEY_SECRET"
      sign_name: "$ALIYUN_SMS_SIGN_NAME"
      region: "cn-hangzhou"

  # Tencent Cloud SMS
  tencentsms:
    type: tencentsms
    enabled: false
    config:
      secret_id: "$TENCENT_SECRET_ID"
      secret_key: "$TENCENT_SECRET_KEY"
      app_id: "$TENCENT_SMS_APP_ID"
      region: "ap-guangzhou"

  # NetEase Yunxin SMS
  neteasesms:
    type: neteasesms
    enabled: false
    config:
      app_key: "$NETEASE_APP_KEY"
      app_secret: "$NETEASE_APP_SECRET"

  # WeChat personal push (ServerChan/PushPlus/WxPusher)
  wechat:
    type: wechat
    enabled: false
    config:
      send_key: "$WECHAT_SERVERCHAN_SEND_KEY"

# Routing configuration
routes:
  error:
    - log
  warning:
    - log
  info:
    - log

# Queue configuration
queue:
  type: memory          # memory | redis
  size: 10000           # queue capacity
  workers: 0            # number of local workers (0 = auto, default CPU cores*2+1)
  # redis:              # required when type=redis
  #   addr: "localhost:6379"
  #   password: ""      # optional
  #   db: 0             # optional
  #   stream: "herald:tasks"
  #   group: "herald-workers"

# Retry configuration
retry:
  max: 3
  backoff: exponential
  initial_delay: 1s
  max_delay: 1m

# Dedup configuration
dedup:
  enabled: true
  window: 5m

# Notification rule seed (optional)
# Rules are evaluated against every notification after dedup and before route resolution;
# when an active rule matches and the caller did not explicitly specify channels,
# the rule's channel path is taken instead; shadow rules only record, they do not deliver.
rules:
  - id: prod-fail-rate            # 1-64 characters: letters, digits, and . _ -
    match: 'params.fail_rate > 0.05 && params.env == "prod"'
    mode: shadow                  # shadow (default, observe) | active | off
    for: 3m                       # optional: sustained-condition debounce, see "for sustained-condition (P2)"
    group_by: [env, service]      # optional: fold same-group events with a count + summary, see "group_by notification aggregation (P2)"
    # group_interval: 10m         # optional: group silence period, default 5m, requires group_by
    # silence: {start: "22:00", end: "06:00"}   # optional: daily silence window, see "silence (P2)"
    route:                        # steps are evaluated in order, the first matching step wins
      - match: 'level == "error"'
        channels: [oncall]
      - channels: [devops]        # an empty match = an always-matching fallback step
  - id: leaf-service-unreachable
    match: 'type == "alert" && params.service == "api"'
    mode: active
    inhibit:                      # optional: suppress this rule while the root cause is present, see "inhibit (P2)"
      source: prod-fail-rate      #   the root-cause rule's id
      equal: [env]                #   suppress only when these field values are equal
      # ttl: 30m                  #   lifetime of a presence entry, default 30m
    route:
      - channels: [devops]
  - id: prod-disk-down            # a rule with an escalation chain, see "escalation chain (P3)"
    match: 'type == "alert" && params.check == "disk"'
    mode: active
    escalation:                   # no ack within ack_timeout → escalate to the to channels
      ack_timeout: 5m             #   optional, default 5m, capped at 24h
      to: [phone-bridge]          #   escalation channels (e.g. a phone gateway bridged via webhook)
    route:
      - channels: [oncall]

# Default policy (optional: allow | deny, default allow)
# Where notifications go when no active rule matches: allow keeps static routing, deny holds them back outright.
# rules_default_policy: allow

# Rule persistence file (optional)
# When set, rule additions/modifications/deletions made through the API are persisted to this JSON file and restored on restart;
# when unset, rules live only in memory (the rules seed still takes effect).
# rules_store: ./data/rules.json

# Rule state storage (optional, used by for sustained-condition and group_by aggregation)
# type: memory (default, single instance; in-flight windows restart counting after a process restart)
# type: redis (windows and group state shared across instances/restarts, connection parameters below)
# rules_state:
#   type: redis
#   addr: "localhost:6379"
#   # password: "..."
#   # db: 0

# Notification group seed (optional): named audiences, any channel slot may reference one as "group:<id>"
# groups:
#   - id: ops-oncall
#     description: On-call roster
#     members:
#       - channel: feishu            # member channel (provider name, channels-block name, or static route channel)
#         recipients: ["@zhang"]     # optional: pinned recipients overriding the channel's default target
#       - channel: sms-duty

# Group persistence file (optional)
# When set, group additions/modifications/deletions made through the API are persisted to this JSON file and restored on restart;
# when unset, groups live only in memory (the groups seed still takes effect).
# groups_store: ./data/groups.json

# Audience persistence file (optional)
# When set, audience additions/modifications/deletions made through the API are persisted to this JSON file and restored on restart;
# when unset, audiences live only in memory (the audiences seed still takes effect; recipients are always pure configuration).
# audiences_store: ./data/audiences.json

# Roster persistence file (optional)
# Rosters are pushed by an external scheduling system through the API (no YAML seed — it is pushed data, not configuration);
# when set, pushed on-call periods are persisted to this JSON file and restored on restart;
# when unset, rosters live only in memory (after a restart, wait for the scheduling system to push again).
# rosters_store: ./data/rosters.json

# Escalation pending-record persistence (optional)
# When set, pending escalations are persisted to this JSON file and restored on restart:
# expired ones re-send their escalation (acks that happened during downtime are still honored), unexpired ones rebuild timers with the remaining time.
# When unset, pending escalations live only in memory (lost on restart, i.e. the escalation is abandoned).
# escalation_store: ./data/escalations.json

# Incident ledger capacity (optional, default 1000)
# Rule-routed deliveries open incidents, ack/recovery closes them, see "Incident ledger and recovery summary (P3)".
# On overflow only closed old incidents are evicted; incidents in the open state never take part in capacity eviction.
# incident_limit: 1000

# Feishu card button callback (optional)
# The interactive card's "Acknowledge Alert" button calls back to POST /api/v1/callbacks/feishu,
# using the same identity and storage as the ack API (ack records, escalation cancellation, incident ledger).
# encrypt_key is the "Encrypt Key" generated when configuring the callback on the Feishu open platform:
# encrypted callbacks are decrypted with it; when unset, encrypted callbacks are rejected (plaintext callbacks and URL verification challenges are unaffected).
# card_callback:
#   encrypt_key: "$FEISHU_ENCRYPT_KEY"

# Provider rate limiting (optional, token bucket)
# A token is taken per provider before delivery; the rule engine fans one event out to multiple channels,
# and rate limiting is the last safety valve against channel storms. Unset = no rate limiting.
# providers:
#   oncall:
#     type: feishu
#     config: { webhook_url: "..." }
#     rate_limit:
#       rate: 10      # tokens replenished per second
#       burst: 100    # bucket capacity (the allowed burst)

# WebSocket configuration (remote worker management channel)
websocket:
  addr: ":8081"
  read_timeout: 60s
  write_timeout: 60s
  ping_interval: 20s
  # allowed_origins:       # list of allowed WebSocket origins
  #   - "https://your-domain.com"
  #   - "*"                 # allow all origins (development only)
  # When unset, localhost/127.0.0.1 are allowed by default

# Template definitions
templates:
  # Server alert template
  server_alert:
    name: "Server alert"
    title: "[Alert] {{.Level}} - {{.Service}}"
    level: "error"
    fields:
      - label: "Server"
        value: "{{.Server}}"
        type: "text"
      - label: "Error"
        value: "{{.Error}}"
        type: "text"
      - label: "Time"
        value: "{{.Timestamp}}"
        type: "text"

  # Deployment notification template
  deploy_notify:
    name: "Deployment notification"
    title: "Deployment complete: {{.Env}}"
    level: "info"
    fields:
      - label: "Environment"
        value: "{{.Env}}"
        type: "text"
      - label: "Version"
        value: "{{.Version}}"
        type: "text"
      - label: "Branch"
        value: "{{.Branch}}"
        type: "text"
      - label: "Duration"
        value: "{{.Duration}}"
        type: "text"
```

### Remote worker configuration (worker.yaml)

Remote workers consume tasks from the shared Queue and require a `redis`-type queue:

```yaml
# Queue configuration (must use the same Queue backend as the scheduler)
queue:
  type: redis
  workers: 0            # number of workers (0 = auto, default CPU cores*2+1)
  redis:
    addr: "localhost:6379"
    stream: "herald:tasks"
    group: "herald-workers"

# Worker control-plane connection (recommended: state the scheduler's WebSocket address explicitly)
worker:
  id: "worker-01"             # optional, auto-generated by default
  server_url: "ws://localhost:8081/worker"   # scheduler management channel (note the /worker path)

# Compatible form: without worker.server_url, falls back to websocket.addr (the /worker path is appended automatically)
websocket:
  addr: "localhost:8081"

# Worker-local providers (optional)
providers:
  wechatmp:
    type: wechatmp
    enabled: true
    config:
      app_id: "$WECHAT_APP_ID"
      app_secret: "$WECHAT_APP_SECRET"
```

## Queue configuration notes

| Field | Type | Default | Notes |
|------|------|--------|------|
| `type` | string | `memory` | Queue type: `memory` (single machine) or `redis` (distributed) |
| `size` | int | `10000` | Queue capacity |
| `workers` | int | `CPU*2+1` | Local worker concurrency |

`queue.timeout` is a reserved key that no queue implementation currently reads (the Redis queue uses a fixed
internal command timeout); configuring it has no effect.

### Deployment mode comparison

| Scenario | queue.type | Notes |
|------|-----------|------|
| Single-machine development/small scale | `memory` | All workers in the same process |
| Distributed/high availability | `redis` | Scheduler and workers can be deployed independently |

### Redis queue requirements

- **Minimum version**: Redis 6.2+ (Streams and Consumer Groups need 5.0+, and delayed retry's `ZRANGE ... BYSCORE` needs 6.2+)
- The Redis Streams commands `XADD`, `XREADGROUP`, and `XACK` are core dependencies

## Environment variables

A string value starting with `$` inside a provider config is expanded at load time to the value of the environment variable with the same name:

```yaml
providers:
  telegram:
    config:
      token: "$TELEGRAM_BOT_TOKEN"
```

> Note: the braced form `"${VAR_NAME}"` is **not expanded**: Herald takes everything after the first `$` as the variable name to look up (that is, `{VAR_NAME}`) and keeps it as-is when not found. Use the `$VAR_NAME` form.

## Rule configuration notes

`rules` is optional; when unset, notification behavior is exactly that of static routing.

### Expression environment

| Identifier | Meaning |
|--------|------|
| `type` | Notification type (`Notification.Type`) |
| `level` | Notification level |
| `title` / `body` | Inline content (the direct content before template rendering; template render output does not participate in matching) |
| `params` | Template parameter map, e.g. `params.fail_rate` |

### Security boundaries

- Expressions are **compiled once** at load/save time; syntax or type errors are rejected outright (startup failure/save failure), and only the compiled artifact is executed at runtime
- Length ≤ 2048 characters, AST ≤ 256 nodes
- Built-in functions and the `range` operator are disabled. An expression is pure comparison/logic over the notification environment — no function calls, no loops

### Shadow mode

- New rules default to `mode: shadow`: every notification is evaluated as usual, but only recorded, never delivered
- A hit is written to the delivery log (status `shadow`, carrying `rule_id`, `would_fire`, and the channels it would have taken), sampled per rule (the first entry plus one per 100), and the total hit count feeds the log statistics
- The "shadow hits" column in the rule list is a cumulative count; `shadow_stats` from `GET /api/v1/rules/{id}` (the console's "details" drawer) additionally provides counts for the last 24 hours / last 7 days plus the 20 most recent hit samples: every shadow hit enters the sample ring (unaffected by log sampling), and the windows accumulate in whole-hour buckets; this too is in-process state, reset to zero on restart
- Once the observed real hit volume matches expectations, switch to `mode: active` to take effect

### Decision layer: actions, priority, and default policy

- A rule's `action` decides where a hit goes: `route` (default, reroute to the rule's route channels), `allow` (pass through, keep the original channels), `suppress` (suppress — no enqueue, no delivery)
- Non-route actions do not combine with stateful semantics (`for` / `group_by` / `inhibit` / `escalation` / `silence`). Sustained-condition and aggregation both work on rerouted deliveries, and combining them only creates ambiguity, so it is rejected outright at save time
- `priority` (optional, default 0, may be negative): a larger value is evaluated first; equal priorities go by creation order (stable sort); the first active hit is the verdict and later rules are not consulted
- Default policy `rules_default_policy`: when not a single active rule matches, `allow` (the default) keeps static routing and `deny` holds the notification back (recorded into the delivery log per sampling, status `suppressed`, no `rule_id`; "held back by the default policy" and "suppressed by a rule" are distinguishable in the log)
- Suppression (a rule's `suppress` or the default policy's `deny`) does not apply to notifications with explicit `channels` — explicit intent wins; consistent with the existing semantics of silence/for/inhibit

### for sustained-condition (P2)

- A rule may carry `for: <duration>` (e.g. `for: 3m`, Go duration syntax, capped at 24h): it only truly fires once the condition of the same logical alert has **held for that duration**, filtering single-blip noise
- Grouping granularity: by default the notification's full content (type/level/title/body/params) must be identical to count as the same group, and each group is timed independently; with `group_by` configured, grouping goes by field values instead (see the next section); the window starts at the group's first hit, and each later hit checks "has it been the full duration since the first one"; notifications whose caller explicitly specified `channels` are not held by for (explicit intent wins)
- After firing once the group goes silent (no repeated delivery); the state expires (`for` + 1 hour) or the rule being modified/deleted restarts the clock; note that event-driven evaluation cannot perceive a blip that has "no recovery event"; with `group_by` configured, later hits inside the silence period flow into the group fold count and the summary still holds
- While an active rule's window has not filled, the event is held (not enqueued, not delivered, similar to a dedup hit), and each hold is recorded into the delivery log per rule sampling (status `pending`); shadow rules only record hits whose "window has filled", so what you see during the shadow period is the real firing cadence
- Window state is kept in process memory by default (in-flight windows restart counting after a restart); `rules_state.type: redis` lets multiple instances share state (keys like `rule:{id}:state:{group}`, auto-reclaimed by TTL)
- On a state-store failure the rule is treated as "evaluation failed" (the rule is skipped) and routing is unaffected

### group_by notification aggregation (P2)

- A rule may carry `group_by: [env, service]` (field values are taken from `params`, 1-8 fields; identical values for the grouping fields mean the same group; omitting group_by falls back to full-content-hash grouping): **new events in the same group are no longer delivered one by one**, but folded into a count for the current round
- The first event delivers as usual and opens a new round; subsequent events inside the `group_interval` (default 5m, capped at 24h) silence period are all folded (not enqueued, not delivered, recorded into the delivery log per rule sampling, status `folded`)
- When the silence period ends, the previous round is settled **when the next event arrives**: that event delivers as usual and is accompanied by a synthetic summary notification (the title carries the group labels and the folded total, the body carries the start and end times); the summary takes the same channel path as the event; purely event-driven, with no background timer. If no further event ever arrives, the last round's summary is made up when the next event does
- The summary is a synthetic notification the rule engine produces itself: it does not go through rule evaluation again (so a broad-matching rule cannot fold the summary back into the group) nor through dedup
- Notifications whose caller explicitly specified `channels` are not folded (explicit intent wins); shadow rules do not simulate folding (the shadow only observes condition hits)
- Combined with `for`, the for check comes first: while the window is unfilled the event is held and no group round opens; events that go silent after firing once flow into the group fold count, and the summary count includes the triggering first event
- Group state lives in the same store as for windows (keys like `rule:{id}:group:{group}`, auto-reclaimed by TTL), and a store failure likewise fails open as "evaluation failed"

### inhibit (P2)

- A cluster going down drags out dozens of "service unreachable" child alerts — all real, but with no action value while the root cause is present. The suppressed rule configures `inhibit: {source: <root-cause rule id>, equal: [fields...]}`: **when the source rule actually delivers**, the notification's combination of equal-field values is recorded as "present" (key like `rule:{id}:inhibit:{value hash}`, stored in `rules_state`), and later hits of this rule whose equal-field values match are held (not enqueued, recorded into the delivery log per rule sampling, status `inhibited`)
- Presence entries expire per `ttl` (default 30m, capped at 24h), and every delivery of the source renews them. Event-driven with no timers: once the root cause stops delivering, entries expire naturally and suppression lifts; "re-send a summary after the root cause recovers" depends on P3 recovery awareness and is not part of this batch
- The check order is before for / group_by: a suppressed event opens no group round and starts no timer — sustained-condition and folding of child alerts are meaningless while the root cause is present
- Grouping granularity: exact match on the combination of equal-field values (a missing field counts as empty); different value combinations do not affect each other
- source may be forward-referenced (create the suppressed rule first and the root-cause rule later; the index wires up automatically); a rule suppressing itself is forbidden
- Notifications with explicit `channels` are not suppressed (explicit intent wins); shadow rules do not check inhibition (the shadow only observes condition hits)
- A presence-read failure fails open as "evaluation failed" (the rule is skipped); a write failure does not block the source's own delivery (better to over-deliver than to miss; errors feed observability)

### silence (P2: silence windows and rosters)

- A rule may carry `silence: {start: "22:00", end: "06:00"}` (HH:MM; an optional `tz` sets the timezone the window follows, defaulting to the process's local timezone): inside the window the rule is **frozen entirely**: events are held (not enqueued, recorded into the delivery log per rule sampling, status `silenced`), for windows do not tick, and group rounds do not open
- `end` is exclusive (22:00-06:00 silences up to exactly 06:00); `start < end` is a same-day window, and `start > end` is automatically understood as a window crossing midnight; a zero-length window (start == end) is rejected by validation
- The optional `tz` is an IANA timezone name (e.g. `silence: {start: "22:00", end: "06:00", tz: "Asia/Shanghai"}`); the window is computed in that timezone, so a deployment in a UTC container can still express "silence after 22:00 Beijing time" without leaning on the container's `TZ` as a fallback; a timezone name that cannot be parsed (`time.LoadLocation` does not know it) is rejected at rule-save time. The release image already ships `tzdata` (Dockerfile `apk add tzdata`), so keep the timezone database when building your own slim images
- An optional `match` expression scopes the silence, e.g. `silence: {start: "22:00", end: "06:00", match: 'level != "critical"'}` — inside the window only non-critical events are silenced and critical ones deliver as usual; a match that fails to compile is rejected at rule validation
- **Scheduling integration placeholder**: a silence's schedule source is one of two: a daily window (the start/end/tz above) or a roster (`silence: {roster: "ops-oncall"}`, not used together with start/end/tz — combining them is rejected at save). A roster is an **absolute period table** that an external scheduling system advances into herald through the API (see "rosters" below); rotation, shift changes, and who is on call all live outside herald — herald only stores and evaluates the pushed periods
- The roster source fails open: when the referenced roster was never pushed (forward references are legal), has been deleted, or the deployment never configured rosters at all, the silence gate **stays open**: missing schedule data never means "should be silent" — silence must be an explicit operator decision, not a side effect of a broken push pipeline
- Schedule-driven and stateless: nothing goes into `rules_state`, evaluation looks only at the current moment, and it does not depend on consistency across a process restart
- The check order is first (before inhibit / for / group_by): silence means "a whole stretch of schedule stays quiet", a different layer of semantics from root-cause presence or sustained-condition
- Notifications with explicit `channels` are not silenced (explicit intent wins); shadow rules do not check silence (the shadow only observes condition hits)

### Rule storage and API (hot reload)

- By default rules exist only in memory (from the `rules` seed); with `rules_store: <path>` set, additions/modifications/deletions made through the API are atomically persisted to that JSON file (tmp + rename) and restored on restart
- Rule changes hot-reload immediately: compiled at save time, the compiled artifact swaps into the live table right away, and the very next notification is evaluated with the new rule — no restart needed
- On a corrupted file (invalid JSON / unrecognized version) startup fails and the scene is preserved; rules are never silently dropped

**API:**

```bash
# List rules
curl http://localhost:8080/api/v1/rules

# Create a rule (a duplicate id returns 409; an expression that fails to compile returns 400)
curl -X POST http://localhost:8080/api/v1/rules \
  -H 'Content-Type: application/json' \
  -d '{"id":"p1","match":"level == \"error\"","mode":"active","route":[{"channels":["oncall"]}]}'

# View / update / delete (the id in the URL takes precedence over the body)
curl http://localhost:8080/api/v1/rules/p1
curl -X PUT http://localhost:8080/api/v1/rules/p1 -d '{...}'
curl -X DELETE http://localhost:8080/api/v1/rules/p1
```

**Implementation deviation note**: the design document originally planned to start persistence with SQLite; P1 actually uses JSON file storage (implementing the same `Store` interface). Rationale: fewer than 100 rules, a single-writer process, and no query needs make SQLite's 15MB cgo dependency disproportionate; SQLite will be introduced when P2 ACK state needs real query capability, at which point the interface stays the same and only the implementation changes.

### Notification groups (`group:` references and audience management)

- A group is a **named audience**: an `id` plus a member list; each member is one channel (a provider instance name, a `channels`-block name, or a static route channel) with optional `recipients` pinning (overriding the default target when delivering to that channel)
- Any channel slot may carry `group:<id>`: a notification's explicit `channels`, rule route channels, escalation chain targets, static route targets — all expanded uniformly into the member channel list at delivery planning time (see the complete example above)
- Expansion is **single-level and flat**: member channels may not carry `group:` again (no nesting; rejected at save); the dedup key keeps the `group:` reference itself, so roster changes do not affect in-flight dedup windows
- When no group manager is configured (neither `groups` nor `groups_store` set), referencing `group:` is an error — fail fast rather than silently drop targets; referencing a nonexistent group is treated as a channel-level failure (recorded in this delivery's `failed`, without blocking other targets)
- Groups are decoupled from rules: rules keep routing to `group:ops-oncall`, and changing who is on call only touches the group roster, not the rules

**API:**

```bash
# List groups
curl http://localhost:8080/api/v1/groups

# Create a group (a duplicate id returns 409; validation failure — empty members / nested references / field overflow — returns 400)
curl -X POST http://localhost:8080/api/v1/groups \
  -H 'Content-Type: application/json' \
  -d '{"id":"ops-oncall","members":[{"channel":"feishu"}],"description":"On-call roster"}'

# View / update (whole-roster replacement) / delete (the id in the URL takes precedence over the body)
curl http://localhost:8080/api/v1/groups/ops-oncall
curl -X PUT http://localhost:8080/api/v1/groups/ops-oncall \
  -H 'Content-Type: application/json' \
  -d '{"members":[{"channel":"feishu","recipients":["@zhang"]},{"channel":"sms-duty"}]}'
curl -X DELETE http://localhost:8080/api/v1/groups/ops-oncall
```

- Changes take effect immediately (live-table swap) and the very next notification uses the new roster; persistence and hot-reload semantics match rule storage (`groups_store` persistence, corrupted file fails startup)

### rosters (silence scheduling integration placeholder)

- A roster is a **named period table**: an `id`, an optional description, and a list of on-call periods (`periods`, each an absolute-time `start`/`end` in RFC3339, `end` exclusive). A rule's `silence: {roster: <id>}` is in effect during any of the periods; at every moment a period covers, the rule is frozen entirely, with semantics identical to a silence window
- herald **does not do scheduling**: rotation rules, shift changes, and who is on call are the external scheduling system's business; once that system computes the periods it pushes the whole table to herald (PUT, full replacement), and herald only stores and evaluates. Periods pushed out of order are sorted automatically (Normalize); overlapping periods, missing timestamps, and zero-length periods are rejected at save. Two pushes fighting over the same moment is the scheduling system's bug and deserves a loud failure, not a silent merge
- At most 512 periods per table (years of weekly maintenance windows pushed occurrence by occurrence still fit; an over-long list almost always means history was pushed as a schedule); clearing a schedule uses DELETE rather than pushing an empty table (saving an empty table is rejected)
- The silence gate reads the live table: pushes take effect immediately and the very next notification is evaluated against the new schedule; after a roster is deleted, silences referencing it fail open immediately (see above) — no alert is lost, delivery just resumes

**API:**

```bash
# List rosters
curl http://localhost:8080/api/v1/rosters

# Push / fully replace a roster (a duplicate id returns 409; period validation failure returns 400)
curl -X POST http://localhost:8080/api/v1/rosters \
  -H 'Content-Type: application/json' \
  -d '{"id":"ops-oncall","description":"Weekend on-call","periods":[
        {"start":"2026-10-10T09:00:00+08:00","end":"2026-10-10T18:00:00+08:00"},
        {"start":"2026-10-11T09:00:00+08:00","end":"2026-10-11T18:00:00+08:00"}]}'

# View / replace / delete (the id in the URL takes precedence over the body)
curl http://localhost:8080/api/v1/rosters/ops-oncall
curl -X PUT http://localhost:8080/api/v1/rosters/ops-oncall -d '{...}'
curl -X DELETE http://localhost:8080/api/v1/rosters/ops-oncall
```

- Persistence and hot-reload semantics match rule storage (`rosters_store` persistence, corrupted file fails startup); without `rosters_store` configured, rosters are kept in memory only

### ACK alert acknowledgment (P3)

`POST /api/v1/alerts/{id}/ack` records an alert acknowledgment (separating "the notification was delivered" from "a human owns the incident": the former is the delivery system's job, the latter the alerting system's):

```bash
# Acknowledge an alert (acked_by optional, records who acknowledged)
curl -X POST http://localhost:8080/api/v1/alerts/incident-123/ack \
  -H 'Content-Type: application/json' \
  -d '{"acked_by": "alice"}'

# Query acknowledgment status
curl http://localhost:8080/api/v1/alerts/incident-123
```

- `{id}` is the caller's alert identity: the business alert id the caller carries in the notification's params; several notifications of the same alert need only one ack under the same id
- Acknowledgment is idempotent: repeated acks of the same id keep the first record (the ack time is a fact, not a counter)

### Feishu card button callback (P3)

Once the Feishu provider is configured with `interactive_cards: true`, **deliveries carrying an alert identity** become interactive cards: a level-colored title + body + an "Acknowledge Alert" button (the button payload carries the alert_id from the delivery task). Clicking the button makes Feishu call the action back to `POST /api/v1/callbacks/feishu`:

- The callback endpoint is **not behind API-token authentication** (the caller is Feishu's servers and the credential is the callback encryption key); when no ack store is configured it returns 503
- The URL verification challenge (`url_verification`) from the first time you configure the callback URL on the Feishu open platform is answered automatically
- The callback body supports an encrypted mode (the `encrypt` field, decrypted with AES-256-CBC using the SHA-256 of `card_callback.encrypt_key`) and a plaintext mode; an encrypted callback returns 501 when no key is configured
- Button acknowledgments go through exactly the same path as the ack API: the same ack record (idempotent, first ack wins), cancellation of pending escalations, marking of the incident ledger; `acked_by` records the clicker's open_id, with the source marked `feishu_card`
- Deliveries without an alert_id (or with the interactive switch off) stay plain text messages — cards exist solely for the acknowledge button

### escalation chain (P3)

A rule may carry `escalation: {ack_timeout: 5m, to: [phone-bridge]}`: when nobody acknowledges within ack_timeout, the alert is re-delivered to the wider `to` channels (phone channels are implemented by bridging an external phone gateway through a webhook; herald ships no built-in carrier integration).

- The alert identity is taken from the notification's `params.alert_id` (the caller's business id, in the same identity space as the ack API); when it is absent it falls back to a content fingerprint (same-content alerts share an identity, but only an explicit alert_id makes acknowledging work well)
- Once a rule-routed delivery goes out, an ack_timeout window opens; re-delivering the same alert resets the window (escalation cares about "even the latest one went unseen"); an ack through the ack API within the window cancels the escalation
- Deadline reached without an ack → a synthetic escalation notification is delivered to the `to` channels (title `[Escalation]` + the original title, body containing the rule, the alert id, and the timeout duration); the escalation notification goes through neither rule evaluation nor dedup: escalation is by design a repeat of an alert that has already been sent
- **The timeout check is timer semantics** (every P2 semantic is event-driven; escalation alone must be able to act even when no further event arrives): pending escalations are persisted to `escalation_store` and restored on restart — expired ones re-send their escalation (acks during downtime are still honored), unexpired ones rebuild timers with the remaining time; without `escalation_store` configured, a restart abandons pending escalations
- Double insurance: if an ack request races the escalation firing at the same instant, then even with the escalation timer already fired, the ack re-check at firing time still makes it abandon delivery
- A call with explicit `channels` does not get an escalation chain (a rule's escalation applies only to the rule's own routing); events held back by for/group folding/inhibition/silence were never delivered and naturally get none either

### Incident ledger and recovery summary (P3)

Every rule-routed delivery opens an incident in the in-memory ledger: `GET /api/v1/incidents` lists all incidents (new→old), and `GET /api/v1/incidents/{id}` returns a single full timeline.

```bash
# All incidents (filterable by status=open|acked|resolved, rule_id, alert_id; limit 1-1000, default 100)
curl 'http://localhost:8080/api/v1/incidents?status=open&limit=50'

# A single incident (including the ack, escalation, and recovery timeline)
curl http://localhost:8080/api/v1/incidents/6f1c...
```

- **Identity**: the ledger records both `rule_id` + the group key (how the engine locates the incident group) and `alert_id` (the business alert id, in the same identity space as the ack API); when `alert_id` is absent it falls back to a content fingerprint
- **Lifecycle**: opened on rule-routed delivery (repeat deliveries of the same alert refresh the context without reopening; a recurrence after recovery is a **new incident**) → marked when the ack API acknowledges it (first ack wins) → closed when the group recovers (recording the event count and duration). Incidents in the open state never take part in capacity eviction; the capacity cap `incident_limit` (default 1000, 0=default) evicts only closed old incidents
- **Recovery is event-driven**: for group_by rules, when a later event of the same group falls to a level that no longer matches the rule (e.g. prod going from error to info), the engine treats the group as recovered. The group key is computed from the group_by field values, so as long as the resource keeps reporting there is a recovery signal; **for a rule without group_by the group key is a content fingerprint, recovery events can never group up, and after its for window expires the group is silently reclaimed by TTL with no recovery summary**
- The recovery summary is delivered to the incident's original channels (title `[Resolved]` + the original title, body containing the alert id, the event count over the period, and the duration); a failed summary delivery is recorded in the timeline (`resolve_delivery_failed`) but does not prevent the incident from closing
- **The recovery event itself is still an ordinary notification**: an event that matches no rule goes through static routing, so the caller must have routes configured for that type; escalation firing and delivery failures are also recorded in the timeline (`escalation_fired` / `escalation_failed`)
- Ordering versus dedup: rule evaluation (including recovery observation) comes first, and dedup is the last gate before delivery. Repeat deliveries are deduped, but an alert's for window, group counts, and recovery signal are never interrupted by dedup


### Delivery rate limiting and retry

**Rate limiting** (`providers.<name>.rate_limit`, optional): before delivery a token is taken per provider (token bucket), and the wait happens before the actual call; if the wait is cancelled (shutdown), that delivery is marked failed. Rate limiting is the last gate on rule fan-out: one bad rule must not turn into a channel storm.

**Retry**: whether a failed delivery is retried depends on the error type:

| Error type | Retryable | Notes |
|----------|--------|------|
| Network transport failure (connection refused, timeout) | ✅ | Marked uniformly by the HTTP client |
| HTTP 408 / 429 / 5xx | ✅ | Upstream overload or transient fault |
| Other HTTP 4xx (400/401/403/404) | ❌ | Deterministic client error; retrying is pointless |
| Channel business failure (e.g. an SMS body error code) | provider-dependent | Each channel marks its own |

Retries back off per the `retry` configuration (exponential backoff by default, at most 3 attempts); a 429 carrying `Retry-After` is honored for its wait, capped at `max_delay`. The backoff wait happens on the queue side — the task is re-enqueued with `next_retry_at` and delivered when due, with no worker held in the meantime. Note: for SMS-type channels the "HTTP 200 but business code failed" error is returned by the provider after it inspects the body; some channels mark it retryable (e.g. Tencent Cloud API errors) and some do not (e.g. a carrier rejection in the send status).

## Dynamic configuration

### Enable/disable a provider

**Via the API:**

```bash
# Enable
curl -X POST http://localhost:8080/api/v1/providers/telegram/enable

# Disable
curl -X POST http://localhost:8080/api/v1/providers/telegram/disable
```

**Via the Dashboard:**

On the Dashboard's Providers page, click the enable/disable button on each provider card.

### Configuration precedence

1. API calls (runtime modification)
2. Configuration file (loaded at startup)
3. Defaults

### Domain model and configuration blocks

Configuration maps one-to-one onto the domain model (for the relations and boundaries see the [Audience Domain Model](/design-audience-model)):

| Configuration block | Domain role | Status |
|--------|---------|------|
| `server` / `queue` / `retry` / `dedup` | Platform foundation | ✅ implemented |
| `providers` | Channel delivery implementations (Providers) | ✅ implemented |
| `routes` / `level_routes` | Channel → Provider routing map | ✅ implemented |
| `groups` | The `group:` form of an Audience (named audience) | ✅ implemented |
| `channels` | Standalone Channel configuration block (`channels: {ci: {providers: [...]}}`) | ✅ implemented |
| `audiences` / `recipients` | `user:`-level audiences and multiple Endpoints (`audiences: {ops: {recipients: [alice]}}` + `recipients: {alice: {endpoints: [...]}}`) | ✅ implemented |
| `digest` | Digest time-window aggregation (window/timezone/optional redis-lease leader election), bypassing the main delivery pipeline | ✅ implemented |
| `feeds` | RSS pull channel (§9): the `/feeds/<category>.xml` public feed and the `/feeds/private/<token>.xml` private feed, a pull-style projection of delivery records with visibility decided at read time | ✅ implemented |
| `sources` | Source adapters (relation detail design §8): three subscription entry points — bot / official account / in-app — plus periodic reconciliation against external state; the credentials double as per-entry switches | ✅ implemented |
| `delivery` | Delivery policy component (§6): the three override tables — category default urgency / channel intensity / category mode — plus phone-gate config-side consent; app overrides stack on top | ✅ implemented |
| `apps` | Integrator namespaces (§13.1): app name → startup token seed (secret + scopes), with no runtime minting API | ✅ implemented |

`audiences` / `recipients`, like `groups`, are the local configuration form of audiences (the `user:` tier; audiences has a runtime write surface, the recipient/endpoint tables still do not — changing recipients = editing YAML + restarting): a `user:<id>` reference in any channel slot resolves against the `audiences` table first and falls back to the `recipients` table on a miss; the expanded endpoints are **merged per provider**: multiple endpoints of the same provider are bundled into one delivery task (if, for example, two recipients both have Feishu configured, only one Feishu task with two targets is produced). Invalid configuration (an audience referencing an unknown recipient, a recipient with no endpoints, an endpoint with an empty type/target) makes startup **fail and refuse to boot**, rather than blowing up at delivery time. This static table is also the static seed of the runtime **ContactSurface** — binding, rebinding, invalidation, and other runtime actions go through the audience-layer registry, and the static table's semantics are unchanged; see the [Audience Domain Model §5](/design-audience-model).

```yaml
recipients:
  alice:
    endpoints:
      - type: feishu
        target: "@alice"
      - type: email
        target: alice@example.com
  bob:
    endpoints:
      - type: feishu
        target: "@bob"
      - type: sms-duty
        target: "13900000000"
audiences:
  ops:
    recipients: [alice, bob]
```

For example, `channels: ["user:ops"]` expands into 3 delivery tasks: feishu (@alice and @bob bundled), email, and sms-duty.

audiences supports a **runtime write surface** (semantics like groups: the `audiences:` YAML is a bootstrap seed, loaded at startup and coexisting with the store; on an id collision the configuration seed overrides the stored entry):

```bash
# List audiences (the live table after merging store and seed)
curl http://localhost:8080/api/v1/audiences

# Create an audience (a duplicate id returns 409; validation failure — invalid id / empty recipients / unknown recipient reference — returns 400)
curl -X POST http://localhost:8080/api/v1/audiences \
  -H 'Content-Type: application/json' \
  -d '{"id":"ops","recipients":["alice","bob"]}'

# View / wholesale replace / delete (the id in the URL takes precedence over the body)
curl http://localhost:8080/api/v1/audiences/ops
curl -X PUT http://localhost:8080/api/v1/audiences/ops \
  -H 'Content-Type: application/json' \
  -d '{"recipients":["alice"]}'
curl -X DELETE http://localhost:8080/api/v1/audiences/ops
```

- Changes take effect immediately (live-table swap) and the very next notification uses the new audience; persistence and hot-reload semantics match the rule/group stores (`audiences_store` persistence, corrupted file fails startup)
- The write surface covers only the audiences entity; recipients (the recipient/endpoint tables) and the channels block remain pure configuration, with the write surface deferred
- A `user:<id>` reference to a deleted audience fails explicitly (unknown) at expansion time and never silently resolves to empty

The `channels` block declares the delivery providers of a named channel (design §27). A **bare name in a channel slot resolves by precedence**: an explicit provider instance > the `channels` block > the `routes` table (`routes` routes by type/level only when the notification specified no channel at all). When a name is neither a provider instance nor present in any configuration block, the original "fails at delivery time as a missing provider" semantics is kept. Group member channel names go through the same resolution chain — a member naming a block fans out to the block's providers:

```yaml
channels:
  ci:
    providers: [telegram, feishu]   # channels: [ci] → two delivery tasks

groups:
  - id: ops
    members:
      - channel: ci                # member channel as a block name → likewise two delivery tasks
```

Invalid configuration (a channel without providers, a reference to an unconfigured provider name) makes startup **fail and refuse to boot**. The `channels` block expands one level only, and its entries must be provider instances rather than other channels.

Configuration parsing is lenient: unknown keys are silently ignored without error, so a typo in a configuration key is not surfaced at startup (structural errors **inside** the blocks already landed in the table above are caught by startup validation, which refuses to boot).

## Source adapter configuration (sources)

Subscription entry points ([relation detail design §8](/design-audience-relations)): the bot webhook, the official-account server callback, and the in-app checkbox converge platform-side actions into registry changes. `enabled` is the master switch, and **the credentials double as per-entry switches** — when a secret/token is empty the corresponding endpoint returns 404 even with `enabled: true`:

```yaml
sources:
  enabled: true
  bot:
    secret: "$TELEGRAM_WEBHOOK_SECRET"   # comparison value for the Telegram webhook secret header; empty = that endpoint 404s
    default_categories: ["notices"]      # the default subscription group after a successful /start rebind; empty = bind the ContactSurface only
  wechat_mp:
    token: "$WECHAT_MP_VERIFY_TOKEN"     # token for signature and console verification; empty = that endpoint 404s
    default_categories: ["notices"]      # the default subscription group the follow event lands in; empty = register the ContactSurface only
  reconcile:
    enabled: true                        # periodic reconciliation against external state (rule 3: trust only the relations we registered ourselves)
    interval: 1h                         # scan interval; a zero value defaults to 1h
    lease_ttl: 1m                        # redis leader-lease TTL; a zero value defaults to 1m; connection parameters reuse digest's
                                         # redis_addr / redis_password / redis_db; without redis configured it runs single-instance
```

| Endpoint | Gate | Purpose |
|------|------|------|
| `POST /api/v1/callbacks/bot` | webhook secret header (constant-time comparison) | one-shot `/start <token>` rebind; `/stop` unsubscribes and stops all flow-back |
| `POST /api/v1/callbacks/wechat-mp` | sha1 signature; a GET with `echostr` is the console verification challenge | converge `subscribe` / `unsubscribe` follow events |
| `POST` / `DELETE /api/v1/audiences/{id}/subscriptions` | API token (the same tier as the operations surface) | in-app checkbox on/off; turning off must-deliver returns 409 |

The reconciliation probes are built from the `providers:` block by `type: telegram|wechatmp`: providers with `enabled: false` are not probed, and missing credentials (an incomplete configuration) **refuse to boot**; targets whose probe errors are skipped precisely — reconciliation does not guess platform state. Across multiple instances, a redis lease makes each round scan only once.

## Delivery policy configuration (delivery)

The operator's global delivery policy tables (the intensity and mode vocabulary of the [Events and Alerts model](/guide/events)). All three tables are optional overrides — a missing table falls back to the category default tier (system=once frequency control, alerts=throttle, the rest window-throttled); **unparseable values refuse to boot**:

```yaml
delivery:
  category_urgency:
    incidents: critical        # category → routine/normal/urgent/critical
  channel_intensity:
    sms: L4                    # channel name → L0..L5
  category_mode:
    incidents: escalation      # category → fixed/escalation/parallel
  allow_phone: true            # phone-gate config-side consent (audience-side consent = a bound phone ContactSurface)
```

| Field | Type | Default | Notes |
|------|------|------|------|
| `category_urgency` | map | empty | category → urgency tier; decides which intensity window a trigger without `urgency` matches |
| `channel_intensity` | map | empty | channel name → intensity tier L0–L5 (channel defaults are on the events model page) |
| `category_mode` | map | empty | category → delivery mode fixed/escalation/parallel |
| `allow_phone` | bool | false | the config-side switch for phones being disabled by default: once on, an L5 channel still requires the audience to have bound a phone ContactSurface (double consent, neither is optional) |

The app namespace's policy overrides (`/api/v1/apps/{app}/policies/*`) stack on top of these three tables; **the phone gate cannot be overridden by an app**, and the channel × relation matrix is the safety floor.

## Digest aggregation configuration (digest)

Time-window aggregation ([Audience Domain Model §10](/design-audience-model)): events inside the window are not delivered directly; when the window opens they are merged into one summary that goes through the full delivery chain. Turning `enabled` off stops the aggregator from running:

```yaml
digest:
  enabled: true
  interval: 1m                  # flip-loop tick (how often due windows are checked); a zero value defaults to 1m
  daily: "09:00"                # daily digest window-open time; a zero value defaults to "09:00"
  weekly: "Mon 09:00"           # weekly digest window-open time; a zero value defaults to "Mon 09:00"
  location: Asia/Shanghai       # IANA timezone; a zero value defaults to Asia/Shanghai
  redis_addr: ""                # when non-empty, leader election via a redis lease (multiple instances flush only once per round)
  redis_password: ""
  redis_db: 0
  lease_ttl: 1m                 # leader lease TTL; a zero value defaults to 60s; renewed on every tick while held
```

| Field | Type | Default | Notes |
|------|------|------|------|
| `enabled` | bool | false | master switch |
| `interval` | duration | 1m | flip-loop tick |
| `daily` / `weekly` | string | `09:00` / `Mon 09:00` | window-open time; `daily` is `HH:MM` and `weekly` is `Weekday HH:MM` |
| `location` | string | `Asia/Shanghai` | IANA timezone name; an invalid timezone **refuses to boot** |
| `redis_addr` / `redis_password` / `redis_db` | | empty | leader-lease election; an empty redis_addr = a single instance running lock-free |
| `lease_ttl` | duration | 60s | lease TTL |

An invalid window-open time or timezone, or an unreachable redis, all **refuse to boot** (`cmd/heraldd/main.go` digest wiring) — no running while sick.

## RSS pull configuration (feeds)

Pull projection ([Audience Domain Model §9](/design-audience-model)): delivering to an rss-type channel projects the delivery in place into a feed entry that readers pull by category or by audience. Turning `enabled` off closes the feed endpoints and treats rss channels as unknown providers (delivery fails as a missing provider):

```yaml
feeds:
  enabled: true
  title: "Herald Notifications" # channel title; a zero value defaults to "Herald Notifications" (the renderer rejects an empty title)
  link: "https://example.com"   # homepage link; when empty the <link> element is omitted
  description: ""               # channel description; when empty it is omitted
  max_items: 500                 # per-category FIFO cap on entries; a zero value defaults to 500
```

| Field | Type | Default | Notes |
|------|------|------|------|
| `enabled` | bool | false | master switch; off = the endpoints close + rss channels count as unknown providers |
| `title` | string | `Herald Notifications` | channel title |
| `link` / `description` | string | empty | when empty the corresponding RSS element is omitted |
| `max_items` | int | 500 | per-category FIFO cap on entries, oldest dropped first |

The public feed URL is `/feeds/<category>.xml` (the category name must match `^[a-zA-Z0-9._-]{1,64}$`, otherwise 404); the private feed is `/feeds/private/<rss_token>.xml`, with the token issued stably per audience and disappearing from the next pull after unsubscription. For the endpoint semantics see the [REST API](/api/rest#feeds-public).

## Integrator namespace configuration (apps)

The seed for integrator onboarding (the [Integration Guide](/guide/integration) has a full seeding walkthrough). **There is no API to mint tokens at runtime** — apps and tokens can only be seeded here, and heraldd validates them at startup (a scope spelling outside the three-tier vocabulary, an invalid app name, or a duplicate or empty secret all **refuse to boot**):

```yaml
apps:
  demo-app:
    tokens:
      - secret: "demo-config-token-0123456789abcdef"
        scopes: [config]          # config changes configuration / trigger triggers delivery / query reads state and audit
```

| Field | Type | Required | Notes |
|------|------|------|------|
| `tokens` | list | at least one | one token per element |
| `tokens[].secret` | string | yes | globally unique, merely non-empty (no length floor) |
| `tokens[].scopes` | list | yes | the three tiers `config` / `trigger` / `query`, with no inheritance |

## Provider types

| Type | Notes | Configuration example |
|------|------|---------|
| `log` | Log output | `type: log` |
| `telegram` | Telegram Bot | `type: telegram` |
| `feishu` | Feishu bot | `type: feishu` |
| `wecom` | WeCom bot | `type: wecom` |
| `dingtalk` | DingTalk bot | `type: dingtalk` |
| `slack` | Slack | `type: slack` |
| `discord` | Discord | `type: discord` |
| `email` | SMTP email | `type: email` |
| `webhook` | Generic webhook | `type: webhook` |
| `aliyunsms` | Aliyun SMS | `type: aliyunsms` |
| `tencentsms` | Tencent Cloud SMS | `type: tencentsms` |
| `neteasesms` | NetEase Yunxin SMS | `type: neteasesms` |
| `fcm` | Firebase push (Android/Web) | `type: fcm` |
| `apns` | Apple push (iOS/macOS) | `type: apns` |
| `jpush` | JPush (Android/iOS/HarmonyOS) | `type: jpush` |
| `getui` | Getui (Android/iOS/HarmonyOS) | `type: getui` |
| `wechat` | WeChat personal push | `type: wechat` |
| `wechatmp` | WeChat official account template message | `type: wechatmp` |
| `worker` | Remote worker local placeholder (forwards to a worker node) | `type: worker` |

For per-channel credential applications and config details see the [Provider handbook](/providers/overview).

## Authentication configuration (auth)

Once API authentication is enabled, every endpoint except login/refresh and the Feishu card callback requires credentials (a Bearer
token or an API Key). With `enabled: true`, `api_keys` must have at least one entry configured, otherwise startup is refused:
listening on a public address (e.g. `:8080`, `0.0.0.0:8080`) requires authentication to be enabled with at least one API key;
with authentication disabled, only `127.0.0.1`, `[::1]` or `localhost` binds are allowed, otherwise the service refuses to start. For public deployments set:

```yaml
auth:
  enabled: true
  api_keys:                 # key -> description (used by Dashboard/API callers)
    "hk-xxxxxxxx": "CI pipeline"
  secret_key: "$HERALD_JWT_SECRET"   # optional: JWT signing key (a built-in default key is used when unset; production must configure it explicitly)
  admin_user:               # optional: Dashboard login account (username -> password)
    admin: "$HERALD_ADMIN_PASSWORD"
```

## Worker configuration (worker)

Remote worker nodes (`heraldd worker`) connect to the scheduler's management channel over WebSocket:

| Field | Default | Notes |
|------|--------|------|
| `id` | auto-generated | Worker identity (the `worker_id` in `/api/v1/workers`) |
| `server_url` | — | Scheduler management channel address, shaped like `ws://host:8081/worker`; when unset it falls back to `websocket.addr` (with the `/worker` path appended automatically), and with neither it does not report to the control plane |
| `heartbeat_interval` | `20s` | Heartbeat interval |
| `reconnect_delay` | `5s` | Reconnect interval after a disconnect |
| `capabilities` | `["*"]` | Capability tags, by which the scheduler selects workers |

### Template configuration

See the [template system](/guide/templates).
