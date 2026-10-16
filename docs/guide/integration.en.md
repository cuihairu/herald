> English · [中文](./integration.md)

# Integration Guide

This page answers "**how does my project use Herald as a delivery hub**": from getting the service address to sending your first alert, copy the examples and you'll be up and running — no need to come ask. Field-level reference per endpoint is in the [App Integration API](/api/apps.en); the rules for notification levels, dedup, and delivery guarantees are in the [Event and Alert Model](/guide/events.en).

## Two integration paths

| | Path A: `/notify` | Path B: app namespace |
| --- | --- | --- |
| Suits | scripts, CI, one-off alerts, small internal tools | business systems with namespaces, alert sources, platforms that need receipts and audit |
| Credentials | none (or an API Key when `auth.enabled` is on) | an app token seeded per application, carrying config/trigger/query scopes |
| Content and routing | give `title`/`body` + `channels` directly, or go through the routing table by `type`+`level` | category + audience; channels are decided by audience relations and delivery policies, **the app does not configure channels** |
| Status lookup | `/logs` (global view, needs admin-surface permission) | `/apps/{app}/deliveries` (only your own, the token is the permission) |
| Delivery result callback | none | yes (signed webhook, at least once) |

If you're unsure, take Path A first to get the pipeline working, then decide whether you need a namespace. The two can coexist.

## Prerequisite: service address and authentication

The service address is provided by the deployer, of the form `http://herald.example.com:8080`; this page uniformly writes `http://127.0.0.1:8080`.

For deployments with `auth.enabled` on, admin-surface endpoints require credentials, and these three forms are equivalent (pick one):

```http
Authorization: Bearer <api-key-or-jwt>
X-API-Key: <api-key>
GET /api/v1/logs?api_key=<api-key>
```

`/api/v1/status` and `/feeds/**` are available anonymously; the app surface (`/api/v1/apps/**`) is authenticated separately and requires an app token even when `auth.enabled` is off. The app token is only read from request headers — **there is no query-parameter fallback**.

## Path A: Send your first alert in a minute

`channels` names the channel instance directly and the server delivers as ordered:

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

Acceptance result (`code:0` only means accepted; whether it was delivered is in `logs`):

```json
{"code":0,"message":"ok","data":{"accepted":["log"],"notification_id":"8e1ea3c3-...","task_ids":["21a2a819-..."]}}
```

When you don't name `channels`, it looks up the routing table (`routes`/`level_routes`) by `type`+`level`; if neither matches it returns 422 `no route: no route found for type=...`. For the full field table see [the notify section of the REST API](/api/rest.en).

For network-retry scenarios, give the same `idempotency_key`: within the process lifetime, a repeated request with the same key returns the first result and does not deliver again.

## Path B: app namespace

### 1. Seed the app (done once by whoever holds herald configuration rights) {#seed-app}

The app and token are seeded in the heraldd config file; **there is no API to mint a token at runtime**. There are three permission tiers: `config` changes categories/templates/policies/callbacks, `trigger` triggers deliveries, `query` reads delivery status and the audit stream. One token carries one permission set; seed as many as needed.

```yaml
apps:
  demo-app:
    tokens:
      - secret: "demo-config-token-0123456789abcdef"
        scopes: [config]
      - secret: "demo-trigger-token-0123456789abcdef"
        scopes: [trigger]
      - secret: "demo-query-token-0123456789abcdef"
        scopes: [query]
```

The examples on this page uniformly use this demo credential set, so you can copy them and get through; replace them with your own values in production (token secrets must be globally unique, non-empty, with no minimum length; only callback-surface secrets have the 16–128 character requirement). If the configuration is wrong (a scope spelling outside the three-tier vocabulary, an illegal app name, a duplicate secret) heraldd refuses to start. Category names and app names share `^[a-zA-Z0-9._-]{1,64}$`.

The app token is used the same way as an API Key: `Authorization: Bearer <secret>` or `X-API-Key: <secret>`.

### 2. Self-check

```bash
curl http://127.0.0.1:8080/api/v1/apps/demo-app \
  -H "X-API-Key: demo-query-token-0123456789abcdef"
```

```json
{"code":0,"message":"ok","data":{"name":"demo-app","scopes":["query"]}}
```

The echoed `scopes` are exactly this token's permissions. Bad credentials and unknown apps uniformly return 401 `invalid app credentials` (not distinguished, to avoid probing).

### 3. Register a category

A category is the classification key on the trigger surface; its default urgency decides which intensity window's channels a trigger without `urgency` is matched against:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/apps/demo-app/categories \
  -H "X-API-Key: demo-config-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{"name":"alerts","default_urgency":"urgent"}'
```

Re-registering the same name with the same default is idempotent; changing to a different default is a 409 conflict (once a category is registered its default urgency cannot drift). Category names are isolated per app.

### 4. Bind audiences

The trigger surface only delivers to **audiences that have a way out**: relation × urgency intensity range × ContactSurface binding — missing any one of these and it cannot deliver. There are three binding entry points:

| Entry point | Endpoint | Who operates it |
| --- | --- | --- |
| Audience self-service | `POST /api/v1/audiences/{id}/subscriptions` | the preference center; the audience checks the boxes itself |
| Platform convergence | bot `/start`, official-account follow | the source adapter writes the table automatically |
| Admin-side binding on behalf | `POST /api/v1/audiences/{id}/surfaces` | scenarios where the contact info lives in the integrator's database |

Binding on behalf also writes a default subscription:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/audiences/user.5/surfaces \
  -H "Authorization: Bearer <operator-token>" \
  -H "Content-Type: application/json" \
  -d '{"channel":"email","target":"dev@example.com","categories":["alerts"]}'
```

Audience ids do not contain `:` (that is reserved for the `user:`/`group:` reference prefixes); the integrator's ref-form target `user:5` corresponds to audience id `user.5`. To look up an audience's existing relations: `GET /api/v1/audiences/{id}/relations` — this is an admin-surface read endpoint (the audience registry is globally shared and deliberately has no app-token surface); credentials go through the operator's Bearer/admin API key, and when `auth.enabled` is on an app token gets a 401; the SDK deliberately omits it, so for the query surface use the SDK's `Deliveries`/`Audit` (which carry their own `app:<name>` namespace isolation).

### 5. Trigger a delivery

```bash
curl -X POST http://127.0.0.1:8080/api/v1/apps/demo-app/dispatch \
  -H "X-API-Key: demo-trigger-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{
    "category": "alerts",
    "audiences": ["user.5", "group:oncall"],
    "dedup_key": "node-17-down",
    "event_id": "evt-20261010-001",
    "title": "node-17 down",
    "body": "cpu 98%, ping lost"
  }'
```

The response is the **acceptance result**, not a delivery receipt:

```json
{
  "code": 0, "message": "ok",
  "data": {
    "notification_id": "a7c6f2bc-...",
    "category": "alerts", "urgency": "urgent", "mode": "escalation",
    "plan": [{"channels": ["email"], "ack_timeout": "15m0s"}],
    "dispatched": [{"audience": "user.5", "channels": ["email"]}],
    "refused": [{"audience": "bob", "channel": "sms", "reason": "phone_disabled"}],
    "task_ids": ["61d35a0f-..."], "accepted": ["email"]
  }
}
```

Key points:

- `urgency` defaults to the category default; if you supply it, yours is used; a misspelled urgency is a 422 rather than a silent downgrade;
- `dedup_key`/`state`/`event_id` go into the dedup gate: a repeat trigger with the same key returns `suppressed: true` and produces no delivery; a state flip (a change in `state`) resends;
- content goes through `template`+`params` (the namespace's own template table, which cannot see global templates) or directly through `title`+`body`; the template variable syntax is Go text/template's <code v-pre>{{.field}}</code>;
- `refused[].reason` vocabulary: `filtered` / `phone_disabled` / `intensity_exceeded` / `no_channels` — a refusal is an answer, and HTTP is still 200;
- only when all channel deliveries fail is the body `code` written as 422.

### 6. Alert sources direct-connect: the event adaptation surface

Integrators whose event semantics live in their own vocabulary (alert sources: kind/severity/target + their own event primary key) can skip dispatch and use `POST /api/v1/apps/{app}/events`; the mapping is done on the herald side:

| Your field | Herald-side mapping |
| --- | --- |
| `kind` | → category (must already be registered) |
| `severity` | `critical`→critical, `warning`→urgent, `info`→normal; any other value is 422 |
| `target` | → audience (ref-form `user:5` maps to `user.5`) |
| `id` | → `event_id`, goes into dedup idempotency, and is carried back as-is in the delivery result callback |

```bash
curl -X POST http://127.0.0.1:8080/api/v1/apps/demo-app/events \
  -H "X-API-Key: demo-trigger-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{"id": 42, "kind": "alerts", "severity": "critical", "title": "db down", "target": "user:5", "meta": {"host": "db-1"}}'
```

The response is exactly the same as dispatch (`DispatchOutcome`). `meta` must be a JSON object and is mapped to template parameters. Go integrators can go straight through the SDK's `c.Events(ctx, sdk.EventsRequest{...})`, with fields mapping one-to-one, so there's no need to hand-assemble this request.

### 7. Check status and audit

```bash
# delivery records (automatically isolated by namespace; you only see your own triggers)
curl -H "X-API-Key: demo-query-token-0123456789abcdef" \
  "http://127.0.0.1:8080/api/v1/apps/demo-app/deliveries?status=failed"

# audit stream: dedup folding, relation changes, in ascending time order
curl -H "X-API-Key: demo-query-token-0123456789abcdef" \
  "http://127.0.0.1:8080/api/v1/apps/demo-app/audit"
```

`deliveries` supports filtering by `status`/`category`/`audience` (50 by default, up to 500). Notifications folded by dedup do not appear in deliveries; the answer is on the `delivery.deduped` event in the audit stream:

```json
{"kind":"delivery.deduped","category":"alerts","source":"app:demo-app","detail":"throttled: node-17-down (×1)","at":"2026-10-10T12:57:58+08:00"}
```

### 8. Callbacks: delivery results and unsubscribe feedback {#callback}

Configure the callback surface (config permission) and herald pushes delivery results and unsubscribe events to you:

```bash
curl -X PUT http://127.0.0.1:8080/api/v1/apps/demo-app/callback \
  -H "X-API-Key: demo-config-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://app.example.com/herald/callback","secret":"demo-callback-secret-0123456789"}'
```

Constraints: url must be an absolute http(s) address; secret is 16–128 characters; the read endpoint never echoes the secret back. Two kinds of events:

- `delivery_result`: the task's final state (success/failed + provider/audience/category), **retries are an internal pipeline affair; the application only sees the final state**;
- `unsubscribe`: audience unsubscribe feedback (audience × category × channel × relation type).

Delivery guarantee and signature verification:

- **At least once**: callback events go through the same queue and retry pipeline as deliveries; a non-2xx is treated as a failure and retried per the backoff policy, so duplicates are possible;
- **Idempotent**: each event carries an `event_id` (a UUID, the same ID for duplicate deliveries); your endpoint dedups on it;
- **Signature**: the request header `X-Herald-Signature: sha256=<hex>` is the HMAC-SHA256 of the exact bytes of the request body (the key is the secret you configured); `X-Herald-Event-ID` is the ID of the same event;
- **There is no timestamp header**; replay protection relies on the receiver deduplicating by `event_id`, which is part of the at-least-once contract.

Signature verification code (receiver-side skeleton, two steps: compute the HMAC, compare in constant time):

```go
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "net/http"
)

func verifyHerald(w http.ResponseWriter, r *http.Request, secret []byte) bool {
    body := mustReadAll(r.Body) // unparsed raw bytes; verify the signature before parsing
    mac := hmac.New(sha256.New, secret)
    mac.Write(body)
    want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    if !hmac.Equal([]byte(r.Header.Get("X-Herald-Signature")), []byte(want)) {
        http.Error(w, "bad signature", http.StatusUnauthorized)
        return false
    }
    return true // a 2xx is the acknowledgment; a non-2xx will be retried
}
```

The Python version of the same algorithm:

```python
import hmac, hashlib

def verify(secret: str, body: bytes, signature: str) -> bool:
    want = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(signature, want)
```

The signing flow above has been verified with a real delivery round trip: configure the callback → trigger a dispatch → the receiver recomputes the HMAC with the shared secret and the comparison matches.

### 9. Policy override (optional)

The app domain can override intensity, delivery mode, escalation-chain parameters, and dedup/frequency control per category or channel; the four endpoint families are replaced as whole families:

```bash
curl -X PUT http://127.0.0.1:8080/api/v1/apps/demo-app/policies/intensity \
  -H "X-API-Key: demo-config-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{"email": "L3"}'
```

Intensity vocabulary `L0..L5` (RSS→phone), modes `fixed|escalation|parallel`, frequency-control tiers `once|throttle|always`. App overrides only layer on top of the operator's global table; **the phone gate cannot be overridden by an app**, the channel × relation matrix is the safety floor, and there is no application-level relaxation endpoint. For field-by-field reference see the [App Integration API](/api/apps.en).

## Integrator list

| Integrator | Role | Integration point | Status |
| --- | --- | --- | --- |
| croupier | status source / alert outlet: capture alerts, healthprobe fault windows, build failures | app namespace `croupier`, trigger token, via the event adaptation surface `POST /api/v1/apps/croupier/events`; its own event primary key (outbox id) goes into `event_id`; when herald is unreachable it only logs and counts failures, and does not block alert persistence | Integrated |
| cockpit | detection orchestration, alert outlet reserved | the `Alerter` interface is reserved, push-channel integration deferred | Reserved, not connected |
| other external systems | general business integration | Path A (`/notify`) or seed an independent app namespace | As needed |

## SDK

The officially maintained Go SDK is at `apps-sdk/go`; one token is one Client, and permissions are decided by the token:

```go
import sdk "github.com/cuihairu/herald/apps-sdk/go"

c := sdk.New("http://herald:8080", "demo-app", os.Getenv("DEMO_CONFIG_TOKEN"))
_ = c.RegisterCategory(ctx, "alerts", "urgent")
_ = c.SetCallback(ctx, "https://app.example.com/herald/callback", secret)
```

Errors fall into two kinds: `*sdk.Error` (a rejection from herald, carrying the HTTP status and message, where retrying is meaningless) and transport-layer failures (`sdk.IsTransport` discriminates them; decide for yourself whether to retry).

`apps-sdk/go/example` is a runnable minimal example: point `HERALD_URL`/`HERALD_APP`/`HERALD_TOKEN` at a deployment and run `go run ./apps-sdk/go/example`; it uses that token to walk through each of the five surfaces show → categories → events → deliveries → audit and print the results — when the category is absent and the token carries both the `config` and `trigger` scopes it self-registers `alerts` first (missing either scope skips self-registration, leaving the rejection to be explained by the trigger step itself), which serves nicely as an integration self-check.

Other languages get no hand-written SDK: the integration protocol is just REST + signed webhooks, so any HTTP client can connect. When you need code generation, feed the [OpenAPI spec of the App Integration API](/api/apps.en) to openapi-generator; no need to wait for official support.

## Common errors quick reference

| Symptom | Cause | Handling |
| --- | --- | --- |
| 401 `invalid app credentials` | wrong token, wrong app name, or forgot the header | check the secret in the seeding config; self-check with `GET /apps/{app}` |
| 403 `token lacks trigger scope` | insufficient token permission | switch to a token with the right permission, or add the scope when seeding |
| 404 `category X is not registered` | the category used by dispatch is not registered | `POST categories` first; the `delivery.categories` config block does not count as registration |
| 422 `severity must be critical/warning/info` | misspelled severity on the event adaptation surface | only the three lowercase full words are accepted; `warn` won't do |
| 422 `function "node" not defined` (template rendering) | the template used <code v-pre>{{node}}</code> | the Go text/template syntax is <code v-pre>{{.node}}</code> |
| 200 + `suppressed: true` | folded by the dedup gate (a repeat with the same dedup_key within the window) | expected behavior; if you really need to resend, change the dedup_key or wait for the window to pass |
| 200 + `refused` | the audience has no way out (missing one of relation/intensity/ContactSurface) | look at the `reason` field and bind accordingly; see step 4 above |
| 200 `code:422` + `all channels failed` | all deliveries failed | check `deliveries` and the audit stream to locate the per-channel error |

For locating more transport-layer errors, see the [Troubleshooting Guide](/guide/troubleshooting.en).
