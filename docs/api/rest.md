# REST API

## Auth {#auth}

The endpoints on this page require `Authorization: Bearer <token>` (or an API Key
carried in the `X-API-Key` header; GET requests may also use the `?api_key=`
query parameter) once `auth.enabled` is turned on; when it is not, anonymous
access is allowed. When an API Key and a JWT are mixed, the API Key is checked
first. JWTs are issued by `/auth/login` with a **fixed 24-hour validity period**;
use `/auth/refresh` to exchange for a new one after expiry. The four exception
domains are described in [API Overview](./overview.md): `/auth/*` itself, the
integrator app surface (authenticated by app token with a three-level scope,
independent of `auth.enabled`), platform self-authentication callbacks (bot /
WeChat Official Account / Feishu), and the public read surface
(`/api/v1/status`, `/feeds/**`).

## POST /api/v1/auth/login {#auth-login}

Log in to exchange for a JWT token. A wrong username or password returns 401. The
token may also be replaced by an API Key carried in the `X-API-Key` header (see
[Auth](#auth)).

### login-request {#login-request}

```json
{
  "username": "admin",
  "password": "admin"
}
```

### login-response {#login-response}

```json
{
  "token": "eyJ...",
  "user": {
    "id": "u-1",
    "username": "admin",
    "role": "admin"
  }
}
```

## POST /api/v1/auth/refresh {#auth-refresh}

Refresh the JWT: exchange the Bearer token (or the `herald_token` cookie / the
`?token=` query parameter) for a new token. A missing, invalid, or expired token
returns 401.

### refresh-response {#refresh-response}

```json
{ "token": "eyJ..." }
```

## GET /api/v1/auth/me {#auth-me}

Query the currently logged-in user. A missing, invalid, or expired token returns
401; a nonexistent user returns 404.

### me-response {#me-response}

```json
{
  "id": "u-1",
  "username": "admin",
  "role": "admin"
}
```

## POST /api/v1/notify

Send a notification.

Model semantics: what is accepted here is the **Notification**; each channel
delivery expanded from it is a **Delivery**. `accepted` only means Herald has
accepted it, not that all Providers have delivered (see
[GET /api/v1/logs](#get-logs) for delivery results). Requests are routed by
`type` + `level`, and recipients can also be named explicitly (`channels` /
`channel` / `audience`); `template` + `params`/`data` goes through template
rendering, while `title`/`body` goes through direct content. Idempotency
semantics are in [Recipients and idempotency](#notify-receivers); field evolution
follows the [Audience domain model charter](/design-audience-model).

### notify-request {#notify-request}

```http
POST /api/v1/notify
Content-Type: application/json
```

**With a template:**

```json
{
  "type": "server.alert",
  "level": "error",
  "template": "server_alert",
  "params": {
    "host": "node-17",
    "status": "offline"
  },
  "channels": ["telegram", "aliyunsms"]
}
```

**Direct content:**

```json
{
  "type": "server.alert",
  "level": "error",
  "title": "Node Offline",
  "body": "node-17 is offline",
  "channels": ["telegram"]
}
```

### notify-params {#notify-params}

| Field | Type | Required | Description |
| -------- | ------ | ---- | ----- |
| type | string | Yes | Notification type (used for routing) |
| level | string | No | Level. No enum validation (any string passes through); conventionally `info` / `warning` / `error`, `critical` is also usable; the mapping to Urgency and event severity is in [Events and alerting model](/guide/events) |
| channels | array | No | Explicitly specify channels; if omitted, routing is by type/level |
| channel | string | No | Single-channel spelling, unioned with `channels` (see [Recipients and idempotency](#notify-receivers)) |
| audience | array | No | Audience references (`group:` / `user:` / bare channel name), each expanded per channel |
| recipients | map | No | Specify recipients per channel `{ "telegram": ["chat_id_1"] }` |
| template | string | No | Template ID |
| params | map | No | Template parameters |
| data | map | No | Template data, merged with `params`; `params` takes precedence on name conflicts |
| idempotency_key | string | No | Idempotency key: repeated requests with the same key within a process lifetime return the first result and do not deliver again |
| title | string | No | Direct title (used when there is no template) |
| body | string | No | Direct content (used when there is no template) |

### notify-response {#notify-response}

For all three outcomes (success / partial failure / total failure) the **HTTP
status code is 200**: on partial failure the body `code` is still 0; only on
total failure is the body `code` written as 422. The HTTP status code is reserved
for transport-layer errors (e.g. an invalid request body returns 400).

**Success:**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "notification_id": "550e8400-e29b-41d4-a716-446655440000",
    "task_ids": ["task-001", "task-002"],
    "accepted": ["telegram", "email"]
  }
}
```

The `failed` key only appears when non-empty (entirely absent on full success).

**Partial failure:**

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "notification_id": "550e8400-e29b-41d4-a716-446655440000",
    "task_ids": ["task-001"],
    "accepted": ["telegram"],
    "failed": [
      { "Channel": "sms-x", "Error": "provider not found: sms-x" }
    ]
  }
}
```

The keys of `failed` entries are the capitalized `Channel` / `Error` (historical
shape, no lowercase alias).

**Total failure:**

```json
{
  "code": 422,
  "message": "all channels failed: [sms-x: provider not found: sms-x]",
  "data": {
    "notification_id": "550e8400-e29b-41d4-a716-446655440000",
    "accepted": null,
    "failed": [
      { "Channel": "sms-x", "Error": "provider not found: sms-x" }
    ]
  }
}
```

On this path `accepted` is `null` (the success list is empty and uninitialized,
without `omitempty`), the same class of detail as `failed` being entirely absent
on full success.

### Recipients and idempotency {#notify-receivers}

**Recipient union**: `channels`, `channel`, and `audience` sit side by side and
can be mixed freely; the final recipient set is the union of the three. Each
item of `audience` is expanded as an audience reference: `group:` resolves to
group members, `user:` resolves to a [configured
recipient](/guide/configuration), and a bare name is treated as a channel
name. `data` and `params` are both template data and take effect merged; on name
conflicts `params` wins.

```json
{
  "type": "server.alert",
  "level": "error",
  "channel": "telegram",
  "audience": ["user:alice", "group:ops"],
  "template": "server_alert",
  "data": { "host": "node-17", "status": "offline" },
  "idempotency_key": "alert:node-17:4"
}
```

**Idempotency**: for requests carrying `idempotency_key`, repeated same-key
requests **within the process lifetime** return the recorded result of the first
request (the same `notification_id` / `task_ids`) and **produce no new
delivery**. Only successfully accepted requests (`code` 0) are recorded; totally
failed (422) requests are not recorded and may be retried with the same key after
correction. The idempotency table is an in-memory implementation (about 1000
entries FIFO eviction, cleared on service restart). Scenarios needing
cross-restart idempotency should deduplicate on the caller side.

## GET /api/v1/status {#get-status}

Query service status.

### status-response {#status-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "running",
    "providers": [
      {
        "name": "telegram",
        "type": "builtin",
        "status": "available",
        "enabled": true,
        "since": "2026-05-18T08:00:00Z"
      }
    ]
  }
}
```

## GET /api/v1/providers {#get-providers}

Query the Provider list.

### providers-response {#providers-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "providers": [
      {
        "name": "telegram",
        "type": "builtin",
        "status": "available",
        "enabled": true,
        "since": "2026-05-18T08:00:00Z"
      },
      {
        "name": "aliyunsms",
        "type": "builtin",
        "status": "available",
        "enabled": false,
        "since": "2026-05-18T08:00:00Z"
      }
    ]
  }
}
```

## POST /api/v1/providers/{name}/enable {#provider-enable}

Enable the specified Provider.

### enable-request {#enable-request}

```http
POST /api/v1/providers/telegram/enable
```

### enable-response {#enable-response}

```json
{
  "code": 0,
  "message": "provider enabled"
}
```

## POST /api/v1/providers/{name}/disable {#provider-disable}

Disable the specified Provider.

### disable-request {#disable-request}

```http
POST /api/v1/providers/telegram/disable
```

### disable-response {#disable-response}

```json
{
  "code": 0,
  "message": "provider disabled"
}
```

## GET /api/v1/workers {#get-workers}

Query connected Workers.

### workers-response {#workers-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "count": 1,
    "workers": [
      {
        "worker_id": "wechat-worker-01",
        "mode": "remote",
        "capabilities": ["wechat", "sms"],
        "connected_at": "2026-05-18T08:00:00Z",
        "last_heartbeat": "2026-05-18T08:05:00Z",
        "status": "online"
      }
    ]
  }
}
```

`mode` is `local` / `remote`; `status` is the string `online` / `offline`.
Deployments without a remote Worker return an empty list (`count` 0).

## GET /api/v1/queue {#get-queue}

Query queue status.

### queue-response {#queue-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "size": 5
  }
}
```

## GET /api/v1/logs {#get-logs}

Query delivery logs — the **fact event stream**: the result, duration, and error
code of each delivery attempt. The delivery lifecycle state machine (`queued` /
`delivering` / `retrying` / `delivered` / `failed` / `dead`) is not here; see
[Delivery state](#deliveries-list).

### logs-params {#logs-params}

| Param | Type | Description |
| -------- | ------ | ----- |
| offset | int | Offset |
| limit | int | Page size (default 50, max 500) |
| status | string | Filter by event outcome (`success` / `failed` / `pending` / `shadow`; event-outcome vocabulary, not lifecycle) |
| provider | string | Filter by Provider |
| level | string | Filter by level |
| since | string | Start time (RFC3339) |
| until | string | End time (RFC3339) |

### logs-response {#logs-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "total": 100,
    "offset": 0,
    "limit": 50,
    "logs": [
      {
        "id": "21a2a819-9392-4b4d-b9a7-e868a658f6a9",
        "provider": "log",
        "payload_kind": "content",
        "level": "error",
        "status": "success",
        "error": "",
        "created_at": "2026-10-10T12:53:09+08:00",
        "completed_at": "2026-10-10T12:53:09+08:00",
        "matched_at": "0001-01-01T00:00:00Z",
        "category": "server.alert"
      }
    ]
  }
}
```

One row per delivery task, sorted by `created_at` descending. `status` ∈
`success` / `failed` / `pending` / `shadow` — this is the **event outcome**
vocabulary, not the lifecycle: `pending` means the attempt has not finished
(tasks stay `pending` during retries and only flip to `success` / `failed` at a
terminal state). Logs do not distinguish `dead` (retries exhausted) from `failed`
(permanent failure) — that distinction exists only in the delivery state store.
`payload_kind` ∈ `content` / `provider_template` / `raw`. `category` is the
notification's `type` (app-surface deliveries use the registered category).
`matched_at` is the rule-match time; tasks that never went through the rule
engine show the zero value `0001-01-01T00:00:00Z`. `duration` (milliseconds) and
`error` appear only when nonzero/non-empty; shadow rows additionally carry
`would_fire` and `channels`.

## GET /api/v1/logs/stats {#logs-stats}

Query log statistics (grouped by event outcome `by_status`, `by_provider`,
`by_level`).

## GET /api/v1/logs/{id} {#log-by-id}

Query a single log entry.

## GET /api/v1/deliveries {#deliveries-list}

Query delivery state — the **state machine read surface**: the delivery lifecycle
(`accepted` / `queued` / `delivering` / `retrying` / `delivered` / `failed` /
`dead`) and retry evolution. Logs record fact events (result/duration/error
code); the state machine and retries live here; the two never write each other's
state. Keyed by task ID, with an independent retention ring (same capacity as
logs, each evicting on its own).

### deliveries-params {#deliveries-params}

| Param | Type | Description |
| -------- | ------ | ----- |
| offset | int | Offset |
| limit | int | Page size (default 50, max 500) |
| status | string | Filter by lifecycle (`accepted` / `queued` / `delivering` / `retrying` / `delivered` / `failed` / `dead`) |
| provider | string | Filter by Provider |
| level | string | Filter by level |
| since | string | Start time (RFC3339, by `created_at`) |
| until | string | End time (RFC3339, by `created_at`) |

### deliveries-response {#deliveries-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "total": 100,
    "offset": 0,
    "limit": 50,
    "deliveries": [
      {
        "id": "21a2a819-9392-4b4d-b9a7-e868a658f6a9",
        "provider": "log",
        "level": "error",
        "status": "retrying",
        "retry_count": 1,
        "max_attempts": 3,
        "last_error": "transient",
        "created_at": "2026-10-14T10:00:00+08:00",
        "updated_at": "2026-10-14T10:00:05+08:00",
        "next_retry_at": "2026-10-14T10:00:15+08:00",
        "category": "server.alert",
        "source": "app:demo-app"
      }
    ]
  }
}
```

One row per delivery task, sorted by `updated_at` descending. `status` is the
7-value lifecycle: `accepted` (exists, not yet enqueued) / `queued` (queued
waiting for a worker) / `delivering` (a worker is attempting) / `retrying`
(retryable failure, waiting for backoff) / `delivered` (delivered) / `failed`
(permanent failure, nothing retryable) / `dead` (retryable errors exhausted the
budget). `retry_count` is the number of backoff retries already executed,
`max_attempts` is the delivery budget (retry policy + the first attempt), and
`next_retry_at` appears only during `retrying`. The `failed` vs `dead`
distinction exists only in the state machine: logs report `failed` for both.

## GET /api/v1/deliveries/stats {#deliveries-stats}

Query delivery state statistics, grouped by lifecycle `by_status`, `by_provider`,
`by_level`.

## GET /api/v1/deliveries/{id} {#delivery-by-id}

Query a single delivery state (by task ID).

## GET /api/v1/config/{name} {#config-get}

Query Provider configuration.

## PUT /api/v1/config/{name} {#config-update}

Update Provider configuration.

## GET /api/v1/rules {#rules-list}

Query the rule list. Returns 503 when the rule engine is not configured.

### rules-response {#rules-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "rules": [
      {
        "id": "prod-payment-failure",
        "match": "level == \"error\" && params.fail_rate > 0.05",
        "mode": "active",
        "action": "route",
        "priority": 10,
        "shadow_hits": 42,
        "route": [{ "channels": ["feishu-oncall", "group:ops"] }]
      }
    ],
    "count": 1
  }
}
```

`mode` is the enable/disable carrier: `active` takes effect, `shadow` only
observes without acting, `off` disables (treated as `shadow` when omitted).
`action` is the decision-layer verb: `route` reroutes (default), `allow` permits,
`suppress` suppresses; the `route` step belongs only to `action=route` and is
rejected if carried otherwise. `priority` orders from high to low;
same-priority rules keep write order, and the first matching active rule decides
the result and stops evaluation.

`shadow_hits` is the cumulative hit count of this rule during its shadow period
(`GET /api/v1/rules/{id}` also carries this field): it counts only shadow hits
that *would have fired*; events held back by `for` suspension, group folding,
suppression, silence, or dropping are each counted separately and do not count
toward it. It is **in-process observational state**: reset on restart, not
persisted with the rule; after switching the rule to `active` the count stops
growing, and switching back to `shadow` resumes it. Check this number before
switching: it is the volume that would actually go out once activated. The list
only carries the cumulative count; the last-24h / last-7d windows and recent hit
samples live in `shadow_stats` on the **rule detail** (see
`GET /api/v1/rules/{id}`).

## POST /api/v1/rules {#rule-create}

Create a rule. **Save compiles**: the expression is compiled here; an invalid
expression is a direct 400 and never enters the evaluation path. An existing ID
returns 409.

### rule-create-request {#rule-create-request}

```json
{
  "id": "prod-payment-failure",
  "match": "level == \"error\" && params.fail_rate > 0.05",
  "mode": "active",
  "action": "route",
  "priority": 10,
  "route": [{ "channels": ["feishu-oncall", "group:ops"] }]
}
```

## GET /api/v1/rules/{id} {#rule-get}

Query the specified rule; a nonexistent rule returns 404. The response is the
rule document plus the same `shadow_hits` statistics as the list, and the
complete shadow statistics read surface `shadow_stats` (this is what the
console's "Details" drawer renders):

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": "prod-payment-failure",
    "match": "level == \"error\"",
    "shadow_hits": 42,
    "shadow_stats": {
      "total": 42,
      "last_24h": 9,
      "last_7d": 30,
      "samples": [
        { "at": "2026-10-03T08:00:00Z", "type": "deploy", "level": "error", "title": "Deployment complete", "channels": ["feishu-oncall"] }
      ]
    }
  }
}
```

`samples` is the recent hits (newest first, at most 20, omitted when there have
never been hits), coming from a sampling ring buffer independent of the delivery
logs; every shadow hit enters the ring. Windows are accumulated in whole-hour
buckets (`last_24h`/`last_7d`). Like `shadow_hits`, this is in-process
observational state: reset on restart. Deployments without Runtime attached omit
the `shadow_stats` field.

## PUT /api/v1/rules/{id} {#rule-update}

Replace the rule wholesale (PUT semantics, **the id in the URL takes precedence
over the request body**). The enable/disable switch goes through this endpoint:
carry the original fields and change only `mode`. The expression is likewise
compiled before writing.

## DELETE /api/v1/rules/{id} {#rule-delete}

Delete a rule; a nonexistent rule returns 404.

## GET /api/v1/groups {#groups-list}

Query the notification group list. Returns 503 when the group manager is not
configured.

### groups-response {#groups-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "groups": [
      {
        "id": "ops",
        "description": "On-call roster",
        "members": [
          { "channel": "feishu-oncall", "recipients": ["@zhang"] },
          { "channel": "sms-duty" }
        ]
      }
    ],
    "count": 1
  }
}
```

`recipients` is an optional per-channel recipient pin; omitting it means all
recipients of that channel. Groups can be referenced by a rule's routing channels
in the form `group:<id>` (see [rules-response](#rules-response)); referencing a
nonexistent group fails explicitly at dispatch time rather than being silently
dropped.

## POST /api/v1/groups {#group-create}

Create a group. An existing ID returns 409.

### group-create-request {#group-create-request}

```json
{
  "id": "ops",
  "description": "On-call roster",
  "members": [{ "channel": "feishu-oncall", "recipients": ["@zhang"] }]
}
```

## GET /api/v1/groups/{id} {#group-get}

Query the specified group; a nonexistent group returns 404.

## PUT /api/v1/groups/{id} {#group-update}

Replace the group wholesale (the id in the URL takes precedence over the request
body); the replacement takes effect immediately on the dispatch hot path.

## DELETE /api/v1/groups/{id} {#group-delete}

Delete a group; a nonexistent group returns 404. Deleting does **not** clean up
references to it in rules: rules still referencing it fail explicitly at dispatch
time due to the unknown group, which beats silently rewriting the user's rules.

## GET /api/v1/audiences {#audiences-list}

Query the audience list (the runtime live table of `user:`-level entities,
containing all entries after YAML seeds and storage are merged). Returns 503 when
the audience manager is not configured.

### audiences-response {#audiences-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "audiences": [
      { "id": "ops", "recipients": ["alice", "bob"] }
    ],
    "count": 1
  }
}
```

## POST /api/v1/audiences {#audience-create}

Create an audience. An existing ID returns 409; validation failure (illegal id,
empty recipients, referencing an unconfigured recipient) returns 400.

### audience-create-request {#audience-create-request}

```json
{
  "id": "ops",
  "recipients": ["alice", "bob"]
}
```

## GET /api/v1/audiences/{id} {#audience-get}

Query a single audience; a nonexistent audience returns 404.

## PUT /api/v1/audiences/{id} {#audience-update}

Replace the audience wholesale (the id in the URL takes precedence over the
request body); the replacement takes effect immediately for `user:` reference
expansion. Validation failure returns 400.

## DELETE /api/v1/audiences/{id} {#audience-delete}

Delete an audience; a nonexistent audience returns 404. Deleting does **not** clean
up its `user:<id>` references in notifications and rules: a still-referenced
audience fails explicitly as an unknown reference during expansion, and is never
silently resolved to empty.

## GET /api/v1/rosters {#rosters-list}

Query the roster list. Returns 503 when the roster manager is not configured.

### rosters-response {#rosters-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "rosters": [
      {
        "id": "ops-oncall",
        "description": "Weekend on-call",
        "periods": [
          { "start": "2026-10-10T09:00:00+08:00", "end": "2026-10-10T18:00:00+08:00" }
        ]
      }
    ],
    "count": 1
  }
}
```

A roster is the absolute time-period table that an external scheduling system
pushes into herald: a rule's `silence: {roster: "ops-oncall"}` silences that
rule at every moment covered by any period (see the silence field in
[rules-response](#rules-response)). Herald only stores and evaluates the pushed
periods; it does no scheduling itself.

## POST /api/v1/rosters {#roster-create}

Push a roster (the scheduling system pushes the whole table in full). An existing
ID returns 409; period validation failure (missing timestamps, zero-length or
inverted periods, overlapping periods, more than 512 periods, empty period table)
returns 400. Out-of-order periods are automatically sorted by start.

### roster-create-request {#roster-create-request}

```json
{
  "id": "ops-oncall",
  "description": "Weekend on-call",
  "periods": [{ "start": "2026-10-10T09:00:00+08:00", "end": "2026-10-10T18:00:00+08:00" }]
}
```

## GET /api/v1/rosters/{id} {#roster-get}

Query the specified roster; a nonexistent roster returns 404.

## PUT /api/v1/rosters/{id} {#roster-update}

Replace the roster's periods wholesale (the id in the URL takes precedence over
the request body); the replacement takes effect immediately for silence
evaluation.

## DELETE /api/v1/rosters/{id} {#roster-delete}

Delete a roster; a nonexistent roster returns 404. Deleting does not rewrite the
rules referencing it: the silence referencing it fails open immediately (missing
schedule data does not mean that silence applies), because restoring delivery is
safer than silently silencing.

## GET /api/v1/alerts/{id} {#alert-get}

Query an alert's acknowledgment status. Returns 503 when the ack store is not
configured.

### alert-get-response {#alert-get-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "alert_id": "alert-42",
    "acknowledged": true,
    "ack": {
      "alert_id": "alert-42",
      "acked_by": "alice",
      "source": "api",
      "acked_at": "2026-10-04T08:00:00Z"
    }
  }
}
```

When unacknowledged, `acknowledged` is `false` and the `ack` field is omitted.

## POST /api/v1/alerts/{id}/ack {#alert-ack}

Acknowledge an alert. **Idempotent**: for the same alert the first acknowledgment
takes effect and repeated acknowledgments return the same record. A timely
acknowledgment cancels that alert's pending `escalation` and marks the
corresponding event on the event ledger as acknowledged. Returns 503 when the ack
store is not configured.

### alert-ack-request {#alert-ack-request}

```json
{ "acked_by": "alice" }
```

`acked_by` may be omitted (the claimer is empty). The response is the
acknowledgment record itself (the `ack` field in the example above).

## GET /api/v1/incidents {#incidents-list}

Query the event ledger (events opened by rule-routed deliveries, closed by
acknowledgment and resolution), newest first. Returns 503 when the event store is
not configured.

### incidents-params {#incidents-params}

| Param | Type | Description |
| -------- | ------ | ----- |
| status | string | Filter by status: `open` / `acked` / `resolved` (omitted or `all` means everything; other values return 400) |
| rule_id | string | Filter by rule |
| alert_id | string | Filter by alert |
| limit | int | Number of entries returned (default 100, max 1000, out of range returns 400) |

Status is not a field in the response: `open` / `acked` / `resolved` are
**derived** from the existence of `resolved_at` / `acked_at`, and filtering runs
on the derived status.

### incidents-response {#incidents-response}

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "incidents": [
      {
        "id": "incident-1",
        "rule_id": "prod-payment-failure",
        "alert_id": "alert-42",
        "title": "Payment failure rate too high",
        "level": "error",
        "opened_at": "2026-10-04T08:00:00Z",
        "events": 7,
        "channels": ["feishu-oncall"]
      }
    ],
    "count": 1
  }
}
```

## GET /api/v1/incidents/{id} {#incident-get}

Query a single event and its full timeline (`timeline`); a nonexistent event
returns 404.

## POST /api/v1/callbacks/feishu {#feishu-callback}

Feishu interactive card callback entry (the acknowledge button on the card).
**Does not go through Bearer auth**; the caller is Feishu's server, authenticated
by `card_callback.encrypt_key`: answer Feishu's `url_verification` challenge;
encrypted callbacks (the `encrypt` field, AES-256-CBC) are decrypted and
processed when an encryption key is configured, and return 501 when it is not;
plaintext callbacks are always processable. The acknowledge action goes through
the same store as `POST /alerts/{id}/ack`: the first acknowledgment takes effect,
pending escalations are canceled, and the event ledger records it. Returns 503
when the ack store is not configured.

### feishu-callback-response {#feishu-callback-response}

Challenge answer:

```json
{ "code": 0, "message": "ok", "data": { "challenge": "..." } }
```

Acknowledge action:

```json
{
  "code": 0,
  "message": "ok",
  "data": { "alert_id": "alert-42", "acknowledged": true, "acked_by": "ou_xxx" }
}
```

## GET /api/v1/templates {#templates-list}

Query the template list.

## POST /api/v1/templates/create {#template-create}

Create a template.

### create-request {#create-request}

```json
{
  "id": "server_alert",
  "name": "Server alert",
  "title": "Server {{.host}} alert",
  "level": "error",
  "fields": [
    { "label": "Host", "value": "{{.host}}" },
    { "label": "Status", "value": "{{.status}}" }
  ]
}
```

## GET /api/v1/templates/{id} {#template-get}

Query the specified template.

## PUT /api/v1/templates/{id} {#template-update}

Update a template.

## DELETE /api/v1/templates/{id} {#template-delete}

Delete a template.

## POST /api/v1/callbacks/bot {#bot-callback}

Telegram platform callback entry (source adapter: `/start` exchanges a binding,
`/stop` stops all subscriptions). **Does not go through Bearer**; it
authenticates by constant-time comparison of the request header
`X-Telegram-Bot-Api-Secret-Token` against `sources.bot.secret`; mismatch returns
403, and when the secret is not configured the whole endpoint returns 404. The
payload is a subset of the Telegram update JSON (`message.chat.id` +
`message.text`). Every well-formed update always returns 200 (Telegram retries on
non-2xx).

## POST /api/v1/callbacks/wechat-mp {#wechat-mp-callback}

WeChat Official Account callback entry (subscribe/unsubscribe source adapter).
**Does not go through Bearer**; it self-authenticates via the query parameter
trio `signature` / `timestamp` / `nonce`:
`signature == hex(sha1(sort([timestamp, nonce, token]).join("")))` where `token`
comes from `sources.wechat_mp.token`, compared in constant time; a missing piece
returns 403, and when the token is not configured the whole endpoint returns 404.
`GET` is used for console URL verification (echoes `echostr` verbatim). The POST
body is XML (`FromUserName` + `Event` ∈ subscribe/unsubscribe). An unbound openid
gets a silent 200.

## GET /feeds/{name} {#feeds-public}

Public category feed (RSS pull model, anonymously readable, not under `/api/v1`).
`name` looks like `<category>.xml`, and the category name must match
`^[a-zA-Z0-9._-]{1,64}$` — no match returns 404; a legal but never-written
category returns an **empty channel 200** (no one has ever written to this
pull-model source; that is the honest answer). Contains only public content with
no audience references. Response `Content-Type: application/rss+xml;
charset=utf-8`. Non-GET methods return 405, render failure returns 500 (both plain
text, not the JSON envelope).

## GET /feeds/private/{name} {#feeds-private}

Audience private feed. `name` looks like `<rss_token>.xml`, and **the token is
the credential** (issued stably per audience, invalidated on reset); an unknown
token returns 404. Visibility is decided at read time: an entry appears only while
the audience's live subscription relation for that category allows the rss
channel — **unsubscribing removes it from the next pull onward**, with zero
bookkeeping on the projection side. Response format and error semantics are the
same as the public feed.

The token read path: `GET /api/v1/audiences/{id}/surfaces` returns the audience's
`rss_token` (issued on first read, stable at the audience level; see the
[surfaces read surface](#audience-surfaces)).

## GET /api/v1/audiences/{id}/relations {#audience-relations}

Query an audience's existing relations (subscription/enrollment). An unknown
audience returns a 200 empty list (the path id is not format-validated).

```json
{"code":0,"message":"ok","data":{"relations":[{
  "audience_id":"alice","category":"bills","channel":"email",
  "type":"subscription","source":"test",
  "policy":{"allow_unsubscribe":true,"must_deliver":false}
}]}}
```

`type` ∈ `subscription` (active subscription, always unsubscribable) /
`enrollment` (passive assignment; must-deliver or unsubscribable, pick one);
`source` ∈ `bot` / `wechat_mp` / `preference_center` / `admin` / `app:<name>`.

## POST / DELETE /api/v1/audiences/{id}/subscriptions {#audience-subscriptions}

Preference center check/unsubscribe entry. Returns 404 when the source adapter is
not configured.

**POST request body**:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| category | string | Yes | 1–64 characters |
| channel | string | Yes | 1–64 characters |

Writes a relation with `type=subscription`, `source=preference_center`. **The
response is the relation object itself, and the wire keys are Go field names
(capitalized)**:

```json
{"AudienceID":"alice","Category":"bills","Channel":"email","Type":"subscription",
 "Source":"preference_center","Policy":{"AllowUnsubscribe":true,"MustDeliver":false}}
```

**DELETE** uses query parameters `?category=&<>&channel=`: no relation in the
slot returns 404 `subscription not found`; a must-deliver relation cannot be
unsubscribed and returns 409.

## GET / POST / DELETE /api/v1/audiences/{id}/surfaces {#audience-surfaces}

Admin-side ContactSurface read/write surface. The write arms (POST/DELETE) are the
delegated-binding entry (for scenarios where user contact information lives in
the integrator's database); the operation is recorded as `admin` and enters the
audit stream; returns 404 when the source adapter is not configured. The read
arm (GET) depends only on the registry and returns the audience's bound surface
list plus the private RSS feed token.

**GET**: an unknown audience (no bound surfaces at all in the registry) returns
404; a legal audience returns the bound surface list + `rss_token` — the token is
**issued on first read and stable at the audience level** (unchanged for the
audience's lifetime; a reset invalidates the old address), and the reader uses it
to assemble the private feed address `/feeds/private/<rss_token>.xml` (see
[private feed](#feeds-private)):

```json
{"code":0,"message":"ok","data":{
  "surfaces":[{"audience_id":"user.1","channel":"email","target":"u1@example.com","status":"active"}],
  "rss_token":"3f9c2e..."
}}
```

The `token` is the credential, and this endpoint sits behind operation-surface
authentication — do not hand it out beyond surfaces the audience itself controls.

**POST request body**:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| channel | string | Yes | 1–64 characters |
| target | string | Yes | Contact target (≤256 characters) |
| categories | array | No | Default categories subscribed along with the binding |

```json
{"code":0,"message":"ok","data":{
  "surface":{"audience_id":"user.1","channel":"email","target":"u1@example.com","status":"active"},
  "bind_changed":true,
  "subscribed":1
}}
```

`status` ∈ `pending` / `active` / `invalid`; `bind_changed` indicates whether
the surface state transitioned; `subscribed` is the **count** of default
subscriptions successfully written. An active slot already holding a different
target is rejected (422 rebinding guard).

**DELETE** uses query parameter `?channel=`: the surface is marked `invalid`
(delivery stopped), all subscription-type relations on that channel terminate, and
**enrollment relations are kept** (the must-deliver baseline is not revocable by
the audience). An unknown or already-invalid surface returns 200
`{"surface_invalidated":false,"terminated":0}`.
