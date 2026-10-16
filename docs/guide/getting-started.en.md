> English · [中文](./getting-started.md)

# Getting Started

**Your first genuinely delivered notification** can take one of two paths:

- **Step 1 (about 1 minute, zero credentials)**: run the full "API accepts the order → queue → delivery" pipeline locally with the `log` channel and watch the message get delivered with your own eyes;
- **Step 2 (about 5 minutes)**: wire up a real channel (Telegram or a group-bot Webhook) and send the notification to your phone or group.

After that, head to [Use Cases and Integration](/guide/use-cases.en) to pick the scenario that matches your business and copy the configuration.

> **Have a ready-made deployment address?** You don't need to install anything — a single curl gets you through (deployments with `auth.enabled` on just add `-H "Authorization: Bearer <token>"` or `-H "X-API-Key: <key>"`):
>
> ```bash
> curl -X POST http://<herald-host>:8080/api/v1/notify \
>   -H "Content-Type: application/json" \
>   -d '{"type":"demo","level":"error","title":"hello","body":"from curl","channels":["log"]}'
> ```
>
> External projects that need a formal integration (namespaces, receipts, signed callbacks) should follow the [Integration Guide](/guide/integration.en); for the rules on levels, dedup, and delivery guarantees, see the [Event and Alert Model](/guide/events.en).

## What Herald Is

> Herald receives notifications from applications and reliably delivers them to one or more notification providers.

Chinese positioning:

> **Herald is a lightweight, Provider-agnostic unified subscription and delivery hub** that provides business systems, operations, CI/CD, Agents, and automated tasks with unified notification routing, audience management, and reliable multi-channel delivery.

The business side only describes "what happened, what to notify about, and whom to notify"; which channel, at what frequency, and for which category each recipient receives things is decided by the audience's own relations; which channel the message ultimately goes through, which Worker delivers it, and when and how it is retried is all handled by Herald (for the model, see the [Audience Domain Model Overview](/design-audience-model.en)).

## Core Concepts

| Concept | In one sentence | Example |
|------|--------|------|
| **Notification** | A notification that needs to be processed and delivered by Herald | An alert with `type=server.alert, level=error` |
| **Audience** | The set of audiences a notification targets (whom to notify) | `group:ops` |
| **Recipient** | A specific recipient within an Audience | `user:alice` |
| **Endpoint** | A Recipient's specific delivery address (static configuration) | `telegram:123456` |
| **ContactSurface** | An audience's ContactSurface: runtime-bound, stateful channel credentials | the telegram surface bound to `alice` (`active`) |
| **Relation** | An audience's send/receive relation for a category, the sole legitimate basis for delivery | `alice × alerts × telegram` (subscription) |
| **Channel** | A notification's logical channel as defined by the business (which kind of notification) | `ci` / `ops` / `security` |
| **Template** | How the notification content is generated | the `server_alert` template renders the title and fields |
| **Routing** | Decides which deliveries a notification expands into | audience resolution + channel routing → Delivery Task |
| **Delivery Task** | A single concrete channel delivery task | `T001 → Alice → Telegram` |
| **Queue / Worker** | When to execute / who executes | memory queue + local worker pool |
| **Provider** | The delivery implementation that actually calls an external service | Telegram / Feishu / Email / Log |

## Architecture

```text
Client
 ↓ (HTTP POST /api/v1/notify)
Notification API
 ↓
Routing (audience resolution / channel routing / rule engine)
 ↓
Delivery Task
 ↓
Queue
 ↓
Worker
 ↓
Provider
 ↓
Telegram / Feishu / Email / Log ...
```

Notification and Delivery are separated: a single Notification may expand into multiple Deliveries (multi-channel), and each Delivery has its own independent state and retries. `accepted` only means Herald has accepted it, not that it has been delivered.

> Implementation status: the Phase 0-9 pipeline rework has landed in full — `Notification / Template / Channel (routes + channels block) / Delivery (enumerated state machine + MaxAttempts/LastError + NextRetryAt) / Queue / Worker / Provider` are all in place; `user:`-level Recipient and multiple Endpoints have landed in a configuration-driven form (the `audiences` / `recipients` config blocks; see "Domain Model and Config Blocks" in the [Configuration Reference](/guide/configuration.en)); Provider errors are classified into six vocabulary categories (retryable ones come back to the queue for redelivery with `next_retry_at`, deterministic ones fail immediately); the notify API supports the `channel` / `audience` / `data` / `idempotency_key` domain fields (see [REST API](/api/rest.en)). For the batch-by-batch execution record, see the [Status Audit](/design-audience-audit.en); the separation of Logs from Delivery state has landed (the `/api/v1/deliveries` state-machine read surface); the audiences write endpoint in the runtime API has landed (see the [Configuration Reference](/guide/configuration.en)), while the recipients/channels write endpoints remain deferred (see the [Deferral Plan](/design-audience-deferrals.en) for the decision record). The audience layer is being extended into a unified subscription and delivery hub according to the [Relations Design](/design-audience-relations.en): the relation model, ContactSurface binding, preference center, channel × relation matrix and delivery filtering, delivery audit completion, Digest time-window aggregation, RSS pull-based channel, source adapters, integrator API, and the Go SDK (`apps-sdk/go`) have all landed (for landing status see [Overview §13](/design-audience-model.en)).

## Installation

### Build from source (recommended, requires Go 1.26+)

```bash
git clone https://github.com/cuihairu/herald
cd herald
make build          # produces bin/heraldd
```

### Docker

```bash
git clone https://github.com/cuihairu/herald
cd herald
cp .env.example .env   # fill in channel credentials as needed
make docker-up
```

## Step 1: Run the full pipeline in 60 seconds (zero credentials)

The `log` Provider sends no network requests — it prints the notification straight to stdout. Without applying for any account, you can verify the entire delivery pipeline.

**1. Write a minimal config** `config.yaml`:

```yaml
server:
  addr: "127.0.0.1:8080"
  timeout: 30s

queue:
  type: memory       # single-machine embedded queue, no Redis needed
  workers: 2

providers:
  log:
    type: log
    enabled: true
    config:
      name: "log"
```

**2. Start the service**:

```bash
./bin/heraldd serve --config config.yaml
```

The startup log looks roughly like this (structured JSON output):

```json
{"time":"...","level":"INFO","msg":"provider registered","name":"log","type":"log","enabled":true}
{"time":"...","level":"INFO","msg":"herald scheduler started","addr":"127.0.0.1:8080","workers":2}
{"time":"...","level":"INFO","msg":"worker pool started","local_workers":2}
```

**3. Send a notification** (open another terminal):

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "server.alert",
    "level": "error",
    "title": "Node Offline",
    "body": "node-17 is offline",
    "channels": ["log"]
  }'
```

The API immediately returns the acceptance result:

```json
{"code":0,"message":"ok","data":{"accepted":["log"],"notification_id":"a2c000ea-77b2-45d8-ac50-52f83d8dd0ae","task_ids":["73e15ce1-6434-4f87-9520-9d5e990f15c9"]}}
```

A **delivery line** then appears in the server log — the message arrived:

```
[error] Node Offline: node-17 is offline
```

What happened during that minute: the `notify` API accepts the order → finds the Provider by `channels` → generates a delivery task and enqueues it → the worker pool picks it up → calls the Provider's `Deliver` → prints the delivery line. Swap in any real channel and the flow is exactly the same, except the last step becomes sending Telegram / SMS / email.

> ⚠️ The start command is `heraldd serve --config ...` (serve is a subcommand). Also, the WebSocket admin endpoint takes `:8081` by default; if it is occupied, change `websocket.addr` in the config.

## Step 2: Send to a real channel

### Path A: Telegram (lands on your personal phone, about 5 minutes)

**Apply for credentials** (no approval needed at any point):

1. In Telegram, find [@BotFather](https://t.me/BotFather) → send `/newbot` → pick a name as prompted and get a **Bot Token** (of the form `123456:ABC-DEF...`)
2. Send your bot any message (in a private chat the user must initiate first, otherwise the bot cannot message you proactively)
3. Get the **chat_id**: open `https://api.telegram.org/bot<your token>/getUpdates` in a browser and find `chat.id` in the returned JSON

**Configure and start**:

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
```

```bash
export TELEGRAM_BOT_TOKEN=123456:ABC-DEF...
export TELEGRAM_CHAT_ID=your-numeric-id
./bin/heraldd serve --config config.yaml
```

**Send**:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{"type":"demo","level":"error","title":"Herald first push","body":"from curl","channels":["telegram"]}'
```

Telegram on your phone receives `🔴 Herald first push` immediately. See [Telegram Provider](/providers/telegram.en) for details.

### Path B: Group-bot Webhook (Feishu/WeCom/DingTalk, about 2 minutes)

All three vendors' custom bots need no approval — add one in the group settings and you get a Webhook URL. Taking Feishu as an example:

1. Feishu group → Settings → Group bots → Add custom bot, copy the Webhook URL
2. Configure:

```yaml
providers:
  feishu:
    type: feishu
    enabled: true
    config:
      webhook_url: "$FEISHU_WEBHOOK_URL"
```

3. After restarting, change `"channels"` in the curl above to `["feishu"]` and the group receives `[error] Herald first push`.

WeCom and DingTalk work the same way: [WeCom](/providers/wecom.en) · [DingTalk](/providers/dingtalk.en) · for all channels see the [Provider Handbook](/providers/overview.en).

## Step 3: Connect your business

Just replace the curl with a single HTTP call in your business code — Herald is HTTP First, so any language, script, or monitoring system webhook can connect:

- **Route by business type**: `type` + `routes`/`level_routes` decide which channels a notification takes; see [Use Cases and Integration](/guide/use-cases.en)
- **Template reuse**: define `templates` once and reuse them across channels; see the [Template System](/guide/templates.en)
- **Named audiences**: one `group:ops` reference expands to the whole on-call group; see [Notification Groups](/design-notification-groups.en); `user:alice` pins to a specific person (multi-endpoint merged delivery); see the [Configuration Reference](/guide/configuration.en)
- **Named channels**: `channels: {ci: {providers: [...]}}` defines a group of providers as a referencable channel; see the [Configuration Reference](/guide/configuration.en)
- **RSS pull-based outlet**: with `feeds.enabled: true`, each category gets a public feed (`/feeds/<category>.xml`); deliveries routed to the `rss` channel are projected into feed entries in place, and subscribers pull them themselves; see the [Configuration Reference](/guide/configuration.en)
- **Idempotent resend**: when retrying over the network, carry the same `idempotency_key` and Herald accepts it only once; see [REST API](/api/rest.en)

## Common Queries

```bash
curl http://127.0.0.1:8080/api/v1/status      # service status
curl http://127.0.0.1:8080/api/v1/providers   # Provider list and status
curl "http://127.0.0.1:8080/api/v1/logs?limit=10"  # delivery logs (delivered/failed/retried)
```

For the full set of fields on the send endpoint (`recipients`, `template`, `params`, etc.), see [REST API](/api/rest.en). If something goes wrong, check the [Troubleshooting Guide](/guide/troubleshooting.en) first.

## Next Steps

- [Use Cases and Integration](/guide/use-cases.en) - complete configurations for alert push / operational outreach / verification-code SMS / multi-channel fallback
- [Provider Handbook](/providers/overview.en) - credential application, configuration, and first message for every channel
- [Configuration Reference](/guide/configuration.en) - the full set of configuration options
