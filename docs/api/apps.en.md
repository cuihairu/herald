> English · [中文](./apps.md)

# App Integration API

`/api/v1/apps/{app}/**` is the integrator namespace: categories, templates, policies, triggers, and receipts are all isolated per app, and the credential is an app token seeded in the configuration file. For a walkthrough of the onboarding actions see the [Integration Guide](/guide/integration.en); this page is the endpoint-by-endpoint reference.

## Authentication and common conventions

- **Credentials**: `Authorization: Bearer <secret>` or `X-API-Key: <secret>`. Read from request headers only; there is no query-parameter fallback (URLs end up in logs).
- **401**: an unknown app and a wrong secret both return `{"code":401,"message":"invalid app credentials"}` — the two cases are not distinguished, to prevent namespace probing (hitting the API with an app name that was never seeded gets the same response).
- **403**: valid credentials but insufficient scope: `{"code":403,"message":"token lacks trigger scope"}`. There are three scope tiers, `config | trigger | query`, with no inheritance: config does **not** imply query.
- **Request body limit is 1 MiB**; anything larger is treated as an invalid request.
- **Envelope**: on success `{"code":0,"message":"...","data":{...}}`; on error `code` equals the HTTP status code and the `data` key is absent entirely. All errors on the app surface (including 405) are JSON envelopes.
- **Rate limiting**: there is no per-client rate limiting at the HTTP layer; the delivery layer has per-channel token buckets (`providers.<name>.rate_limit`), invisible to the trigger endpoints.

The demo credentials in the response examples match the [Integration Guide](/guide/integration.en#seed-app).

## GET /api/v1/apps/{app}

Self-check endpoint: confirms the credentials are valid and echoes the token's permission set. No request parameters, GET only; other methods get 405.

```bash
curl -H "X-API-Key: demo-query-token-0123456789abcdef" \
  http://127.0.0.1:8080/api/v1/apps/demo-app
```

```json
{"code":0,"message":"ok","data":{"name":"demo-app","scopes":["query"]}}
```

`scopes` is emitted in the fixed contract order (config, trigger, query).

## Categories: POST / GET /api/v1/apps/{app}/categories

Categories are the classification key of the trigger surface. POST requires `config`, GET requires `query`.

**POST request body**:

| Field | Type | Required | Values | Notes |
| --- | --- | --- | --- | --- |
| name | string | yes | `^[a-zA-Z0-9._-]{1,64}$` | Category name, unique within the namespace |
| default_urgency | string | yes | `routine`/`normal`/`urgent`/`critical` (Chinese aliases accepted) | Triggers that carry no `urgency` match the intensity window at this tier |

Idempotent: registering the same name with the same value again returns 200; the same name with a different value returns 409 `category already registered with a different default urgency` — once a category is registered its default urgency cannot drift; to change it, use a new category name.

**GET response** (sorted by name; a fresh namespace is `[]`):

```json
{"code":0,"message":"ok","data":{"categories":[{"name":"alerts","default_urgency":"urgent"}]}}
```

## Policies: GET /policies and PUT /policies/{family} {#policies}

The app domain's override set for delivery policies. GET requires `query`; PUT requires `config` for all four families and does a **whole-family replacement** (keys that do not appear in the body are cleared); the families do not affect one another.

**GET /api/v1/apps/{app}/policies** (aggregate read-back; families that were never set do not appear; when everything is empty `data` is `{}`):

```json
{"code":0,"message":"ok","data":{
  "channel_intensity": {"email": "L3"},
  "mode_by_category": {"alerts": "escalation"},
  "ack_timeout": "3m0s",
  "dedup_tiers": {"alerts": "always"},
  "dedup_windows": {"alerts": "10m0s"}
}}
```

**The four PUT sub-endpoints**:

| Endpoint | Request body shape | Valid values |
| --- | --- | --- |
| PUT /policies/intensity | `{"<channel>": "<intensity>", ...}` | `L0`–`L5` (RSS/Mail/Inbox/IM/SMS/Phone aliases are also accepted) |
| PUT /policies/delivery-mode | `{"<category>": "<mode>", ...}` | `fixed` / `escalation` / `parallel` (Chinese aliases accepted) |
| PUT /policies/escalation | `{"ack_timeout": "<duration>"}` | Go duration (`"3m"`, `"1h30m"`); required, non-negative; `"0s"` clears the override |
| PUT /policies/dedup | `{"tiers": {"<category>": "<tier>"}, "windows": {"<category>": "<duration>"}}` | tier ∈ `once`/`throttle`/`always`; window must be a positive duration |

Success returns 200 with `data` being the complete aggregated override set. A bad value returns 422 naming the key, e.g. `tiers[alerts]: dedup: unknown frequency tier "hourly" (want once|throttle|always)`.

Semantic boundaries: app overrides only stack on top of the operator's global tables; **the phone gate (must-deliver double consent) cannot be overridden by an app**; the channel × relation matrix is the safety floor, and there is no application-level relaxation endpoint.

## Templates: POST / GET /api/v1/apps/{app}/templates

A template table private to the namespace: rendering happens only inside the namespace and global templates are invisible. POST (upsert) requires `config`, GET requires `query`.

**POST /templates request body**:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| id | string | yes | Unique key within the namespace |
| name | string | yes | Display name |
| title | string | yes | Title template, Go text/template syntax <code v-pre>{{.field}}</code>; missing → 422 |
| level | string | no | Conventionally `error`/`warning`/`info` |
| fields | array | no | Elements are `{"label","value","type"}`; label/value must be non-empty |
| bindings | object | no | Channel name → `{format, template_code, template_id, params, param_order}` |

Idempotent upsert: POSTing the same id again returns 200 and replaces the whole record. Success returns 200 and `data` echoes the template object (with created_at/updated_at).

**GET /templates**: `data` is directly an array of template objects (not a wrapper map); a fresh namespace is `[]`.
**GET /templates/{id}**: an unknown id returns 404 `template <id>: template not found: <id>` (with the example id ghost, that is `template ghost: template not found: ghost`).
**DELETE /templates/{id}**: success returns `{"code":0,"message":"deleted"}`; an unknown id returns 404 `template not found: <id>` (deleting an unknown id is an error, not an idempotent no-op).

## Callback configuration: GET / PUT / DELETE /api/v1/apps/{app}/callback

Registers "where herald pushes delivery results and unsubscribe events". GET requires `query`; PUT/DELETE require `config`.

**PUT request body**:

| Field | Type | Required | Validation |
| --- | --- | --- | --- |
| url | string | yes | Absolute http(s) URL, otherwise 422 |
| secret | string | yes | 16–128 characters, otherwise 422 |

Success returns 200. Whole-record replacement semantics. **The GET read path never echoes the secret**:

```json
{"code":0,"message":"ok","data":{"url":"http://127.0.0.1:29090/callback","has_secret":true}}
```

When nothing is configured, `{"url":"","has_secret":false}`. DELETE also returns 200 `deleted` when nothing is configured.

**Outbound callback payload** (herald → your url, POST application/json):

```json
{
  "event_id": "751c5b97-0191-40a9-9291-153944909201",
  "app": "demo-app",
  "kind": "delivery_result",
  "at": "2026-10-10T12:54:45+08:00",
  "delivery": {
    "task_id": "944f0c97-...",
    "event_id": "evt-docs-001",
    "audience_id": "log",
    "category": "alerts",
    "channel": "log",
    "status": "success",
    "error": ""
  }
}
```

| Field | Notes |
| --- | --- |
| event_id | UUID of this callback event, the **receiver-side idempotency key** (at-least-once delivery, duplicates possible) |
| kind | `delivery_result` (delivery settled) or `unsubscribe` (unsubscribe flowing back) |
| delivery.event_id | **Echoes the event_id you declared in dispatch/events**; receipts are matched back to the original event by it; `status` ∈ success/failed |
| unsubscribe | `{audience_id, category, channel, relation_type, actor?}` |

Request headers: `X-Herald-Signature: sha256=<hex>` (HMAC-SHA256 over the exact request body bytes, keyed by the secret from the PUT) and `X-Herald-Event-ID` (equal to the body's event_id). For signature-verification code and replay protection see the [Integration Guide](/guide/integration.en#callback). There is no timestamp header and no time-window check; a non-2xx response goes through the same retry pipeline as delivery (3 attempts with exponential backoff by default). An app with no callback configured is silently skipped, and a callback failure does not affect the original delivery.

## Trigger: POST /api/v1/apps/{app}/dispatch

Requires `trigger`. The category-semantics trigger surface: **the category must already be registered in this namespace** (the operator's `delivery.categories` configuration block does not count); if it is not registered, 422.

**Request body**:

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| category | string | yes | — | A registered category; unregistered → 422 |
| urgency | string | no | category default | `routine`/`normal`/`urgent`/`critical`; invalid → 422, not clamped |
| relation_type | string | no | `subscription` | `subscription` (subscription relation) or `enrollment` (enrollment relation, parallel delivery by default); other values → 422 |
| audiences | string[] | yes | — | Audience ids, ref forms (`user:5`/`group:oncall`), or bare channel names; an empty array → 422 |
| dedup_key | string | no | content-derived | Dedup key; repeated triggers under the same key within the window return `suppressed:true` |
| event_id | string | no | — | Your own event identity; it enters the dedup idempotency layer and is echoed back verbatim on the delivery result callback |
| state | string | no | — | State-machine dimension (`down`/`ok`); the same key with the same state collapses, a flip resends |
| template | string | no | — | Namespace template id; render failure or unknown → 422 |
| params | object | no | — | Template variables |
| title / body | string | no | — | Direct content; use either this or template |

**Successful 200 response** (an acceptance result, not a delivery receipt):

```json
{
  "code": 0, "message": "ok",
  "data": {
    "notification_id": "a7c6f2bc-4d70-4374-8d89-e01a39714275",
    "category": "alerts",
    "urgency": "urgent",
    "mode": "escalation",
    "plan": [{"channels": ["email"], "ack_timeout": "15m0s"}],
    "dispatched": [{"audience": "log", "channels": ["log"]}],
    "refused": [{"audience": "bob", "channel": "sms", "reason": "phone_disabled"}],
    "task_ids": ["61d35a0f-6196-46c9-87da-76b81715a37f"],
    "accepted": ["log"]
  }
}
```

| Key | Notes |
| --- | --- |
| plan | Shows the full escalation chain; the first stage delivers first and later stages do not advance automatically — no ack source is wired, see the design §6.3 landing note for the shelved decision; `mode` ∈ fixed/escalation/parallel |
| dispatched | Audiences that matched and already had deliveries generated |
| refused | Refused audiences with reasons: `filtered` / `phone_disabled` / `intensity_exceeded` / `no_channels` |
| suppressed | `true` when dedup collapsed the trigger; in that case, apart from `dispatched` always being an empty array, only notification_id/category/urgency/suppressed are present, with no task_ids |

Refusal is the answer: a response with refused entries is still 200; only when every channel delivery fails is the body `code` written as 422. A deployment without the delivery policy component returns 404 `delivery policy not configured`.

## Event adaptation: POST /api/v1/apps/{app}/events

Requires `trigger`. For alerting sources whose event semantics live in their own vocabulary; it maps onto exactly the same pipeline as dispatch, and the response is identical to dispatch.

**Request body**:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| kind | string | yes | Mapped to a category, which must be registered; empty → 422 |
| severity | string | yes | `critical`→critical, `warning`→urgent, `info`→normal; other values → 422 |
| target | string | yes | A single audience; the ref form `user:1` maps to audience id `user.1` |
| id | number | no | Your event primary key (e.g. an outbox id); converted to `event_id` and echoed back on callbacks |
| title / body | string | no | Direct content |
| dedup_key | string | no | Same as dispatch |
| meta | object | no | Template parameters; non-object → 400 |
| occurred_at | string | no | RFC3339; herald only parses it, the timestamp on the herald side is authoritative |

dispatch's `relation_type`/`urgency`/`template`/`state` are not supported — it always uses the subscription relation and the severity→urgency mapping.

## Queries: GET /deliveries and GET /audit

Both require `query`, and the namespace boundary is enforced: only rows with `source=app:<name>` are visible.

**GET /deliveries**: reads the delivery state machine (lifecycle/retry evolution), not a log fact stream. `status` uses the state-machine vocabulary `accepted/queued/delivering/retrying/delivered/failed/dead` — the distinction between `failed` (terminal with an error) and `dead` (retries exhausted) exists only here; the log-side `success/failed` event outcomes do not pass through this endpoint.

| Parameter | Default | Notes |
| --- | --- | --- |
| offset | 0 | Negative values are treated as 0 |
| limit | 50 | `<=0` becomes 50, `>500` is truncated to 500 |
| status | all | Exact match against the seven lifecycle values |
| category | all | Exact category match |
| audience | all | Exact match against the audience string passed at trigger time (dispatch persists it as-is; for events, `target` goes through the `user:5`→`user.5` mapping before being persisted, so query by the mapped id) |

```json
{"code":0,"message":"ok","data":{"total":1,"offset":0,"limit":50,"deliveries":[{
  "id": "6af218e5-...", "provider": "log", "level": "warning", "status": "delivered",
  "retry_count": 0, "created_at": "2026-10-10T12:55:04+08:00", "updated_at": "2026-10-10T12:55:04+08:00",
  "relation_type": "subscription", "source": "app:demo-app",
  "audience_id": "user.1", "category": "alerts"
}]}}
```

Rows appear only after the task has actually executed; triggers collapsed by dedup do not show up here (look in audit for `delivery.deduped`).

**GET /audit**: the audit stream, ascending by time. Parameter `since` (optional RFC3339; invalid → 400). When audit storage is not configured it returns 404 `audit trail not configured` **before authentication**.

```json
{"code":0,"message":"ok","data":{"events":[{
  "kind": "delivery.deduped", "category": "alerts",
  "relation_type": "subscription", "source": "app:demo-app",
  "detail": "throttled: node-17-down (×1)", "at": "2026-10-10T12:57:58+08:00"
}]}}
```

`kind` vocabulary: `relation.subscribe` / `relation.enroll` / `relation.terminate` / `surface.bind` / `surface.rebind` / `surface.invalidate` / `delivery.deduped`.

## OpenAPI specification

The integration protocol is REST + signed webhook only, with no language restrictions. Specification file: **`docs/api/openapi.yaml`** (OpenAPI 3.1, covering all endpoints of `/notify` and `/apps/{app}/**`). Generate a client for yourself with openapi-generator:

```bash
npx @openapitools/openapi-generator-cli generate \
  -i docs/api/openapi.yaml \
  -g python \        # or typescript-axios / java / go, etc.
  -o ./herald-client
```

In the specification, `servers.url` holds your deployment address; the security scheme for app tokens is named `AppToken` (apiKey, header `X-API-Key`), and admin-surface tokens use `BearerAuth`. Go users can use the official `apps-sdk/go` directly (`apps-sdk/go/example` ships a runnable self-check that walks the five surfaces — self-check, categories, events, delivery records, audit) and do not need to generate anything.

## Error code summary

| HTTP | When it happens | Example message |
| --- | --- | --- |
| 400 | Invalid JSON body / meta not an object | `invalid JSON body` (callback PUT is the exception, reporting `invalid request body`) |
| 401 | Wrong token, wrong app name, missing credentials (including accessing with an app name that was never seeded) | `invalid app credentials` |
| 403 | Insufficient scope | `token lacks trigger scope` |
| 404 | Audit storage not configured; no delivery policy component | `audit trail not configured` / `delivery policy not configured` |
| 405 | Method not supported by the endpoint (every endpoint has an explicit check — the four policy families accept only PUT, dispatch/events only POST, show/policies/deliveries/audit only GET) | `method not allowed` |
| 409 | Same category name with a different default urgency | `category already registered with a different default urgency` |
| 422 | Field validation failure (unregistered category / invalid severity / invalid urgency / invalid relation_type / template render failure / bad policy value) | `severity must be critical/warning/info` |

Every trigger and query success path returns 200; `suppressed`, `refused`, and `failed` are all fields of the acceptance result, not HTTP errors.
