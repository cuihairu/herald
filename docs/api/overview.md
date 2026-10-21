# API Overview

## Protocol

Herald uses **HTTP REST + JSON** as its only public protocol.

## Base URL

```
http://your-host:8080/api/v1
```

## Auth

Endpoints are divided into four authentication domains (routing and middleware
are in the registry in `api/server.go`):

1. **Dashboard/API token** (requires `Authorization: Bearer <token>` when
   `auth.enabled` is on): notify, providers, workers, queue, logs, config,
   templates, rules, groups, rosters, alerts, incidents, and audiences (entity
   CRUD plus relations/subscriptions/surfaces). The token comes from
   `/auth/login`; `/auth/refresh` exchanges an old token for a new one; `/auth/me`
   does not go through this middleware and validates the token inside the
   handler. After logging in, the Dashboard stores the token in `localStorage`
   and an axios request interceptor attaches it uniformly.
2. **App token** (the `/api/v1/apps/{app}/**` integrator surface): authenticated
   by the token seeded per app, via `Authorization: Bearer <app token>` or
   `X-API-Key`; each token carries a config/trigger/query permission set (see
   [App integration](/guide/integration)). An unseeded app name and a wrong
   secret both return 401, with no distinction between the two cases.
3. **Self-authenticating callbacks** (no Bearer): `/callbacks/feishu` (Feishu's
   server authenticates with the callback encryption key), `/callbacks/bot` and
   `/callbacks/wechat-mp` (platform callbacks, self-authenticating with their
   respective shared secrets).
4. **Public read surface**: `/api/v1/status` and `/feeds/**` (RSS pull model) are
   anonymously usable.

When authentication is not enabled, domain 1 is entirely anonymously usable
(domain 2 still requires an app token — app authentication is independent of
`auth.enabled`).

## API list

| Method | Path | Description |
| ---- | ---------- | ----- |
| POST | /auth/login | Log in; exchange for a JWT token and user info |
| POST | /auth/refresh | Exchange a Bearer token for a new token |
| GET  | /auth/me | Query the current logged-in user |
| POST | /notify    | Send a notification |
| GET  | /status    | Query status (public read surface) |
| GET  | /providers | Query Providers |
| POST | /providers/{name}/enable | Enable a Provider |
| POST | /providers/{name}/disable | Disable a Provider |
| GET  | /config/{name} | Query Provider configuration (sensitive values masked) |
| PUT  | /config/{name} | Update Provider configuration |
| GET  | /workers | Query connected Workers |
| GET  | /queue | Query queue depth |
| GET  | /logs | Query delivery logs (fact events: result/duration/error code) |
| GET  | /logs/stats | Delivery log statistics |
| GET  | /logs/{id} | Query a single delivery log |
| GET  | /deliveries | Query the delivery state machine (lifecycle/retry evolution) |
| GET  | /deliveries/stats | Delivery state statistics |
| GET  | /deliveries/{id} | Query a single delivery state |
| GET  | /rules     | Query the rule list |
| POST | /rules     | Create a rule (the expression compiles on save) |
| GET  | /rules/{id} | Query a single rule |
| PUT  | /rules/{id} | Replace a rule wholesale (the enable/disable switch goes through this) |
| DELETE | /rules/{id} | Delete a rule |
| GET  | /groups    | Query the notification group list |
| POST | /groups    | Create a group |
| GET  | /groups/{id} | Query a single group |
| PUT  | /groups/{id} | Replace a group wholesale |
| DELETE | /groups/{id} | Delete a group |
| GET  | /rosters    | Query the roster list |
| POST | /rosters    | Push a roster (the whole table in full) |
| GET  | /rosters/{id} | Query a single roster |
| PUT  | /rosters/{id} | Replace a roster's periods wholesale |
| DELETE | /rosters/{id} | Delete a roster |
| GET  | /alerts/{id} | Query an alert's acknowledgment status |
| POST | /alerts/{id}/ack | Acknowledge an alert (a timely acknowledgment cancels the pending escalation) |
| GET  | /incidents | Query the event ledger |
| GET  | /incidents/{id} | Query a single event (with full timeline) |
| GET  | /templates | Get the template list |
| POST | /templates/create | Create a template |
| GET  | /templates/{id} | Get a single template |
| PUT  | /templates/{id} | Update a template |
| DELETE | /templates/{id} | Delete a template |
| POST | /callbacks/feishu | Feishu card callback (ack button / URL verification; no Bearer) |
| POST | /callbacks/bot | Bot platform callback (/start /stop and other source entries; shared-secret self-authentication) |
| POST | /callbacks/wechat-mp | Official Account callback (subscribe/unsubscribe source entry; shared-secret self-authentication) |
| GET / POST | /audiences | Audience list / create (runtime write surface for `user:`-level entities; storage see `audiences_store`) |
| GET / PUT / DELETE | /audiences/{id} | Query / replace wholesale / delete a single audience (URL id takes precedence) |
| GET  | /audiences/{id}/relations | Query audience relations (subscription/enrollment) |
| POST / DELETE | /audiences/{id}/subscriptions | Preference center subscribe/unsubscribe (source adapter entry) |
| GET  | /audiences/{id}/surfaces | Bound surface list + private feed `rss_token` (issued on first read, stable at the audience level) |
| POST | /audiences/{id}/surfaces | Admin-side delegated ContactSurface binding (may also write default subscriptions) |
| DELETE | /audiences/{id}/surfaces | Unbind a ContactSurface (`?channel=`) |

**Integrator app surface** (`/api/v1/apps/{app}/**`, app-token authenticated,
namespace isolated; an unseeded app name uniformly returns 401):

| Method | Path | Permission | Description |
| --- | --- | --- | --- |
| GET | /apps/{app} | query | Self-check: echoes the token's permission set |
| GET / POST | /apps/{app}/categories | query / config | Category list / register a category (with default Urgency) |
| GET | /apps/{app}/policies | query | Read policy overrides (reads back an empty object when unset) |
| PUT | /apps/{app}/policies/intensity | config | Channel Intensity override |
| PUT | /apps/{app}/policies/delivery-mode | config | Delivery mode override |
| PUT | /apps/{app}/policies/escalation | config | Escalation chain parameter override |
| PUT | /apps/{app}/policies/dedup | config | Dedup frequency control override per category |
| GET / POST | /apps/{app}/templates | query / config | Namespace template list / register |
| GET / DELETE | /apps/{app}/templates/{id} | query / config | Read / delete a namespace template |
| GET / PUT / DELETE | /apps/{app}/callback | query / config | Callback surface configuration (URL + secret; the read surface does not echo the secret) |
| POST | /apps/{app}/dispatch | trigger | Trigger a delivery (three-way intersection match; returns the acceptance result and delivery plan) |
| POST | /apps/{app}/events | trigger | Event ingestion adapter surface (kind/severity/target vocabulary mapping) |
| GET | /apps/{app}/deliveries | query | Delivery state query (reads the state machine store; default 50 entries, max 500) |
| GET | /apps/{app}/audit | query | Audit stream (relation changes, dedup folding) |

**RSS pull model** (mounted at `/feeds/**`, not under `/api/v1`, public read
surface):

| Method | Path | Description |
| --- | --- | --- |
| GET | /feeds/{name} | Public feed (one per category; contains only public content with no audience references) |
| GET | /feeds/private/{name} | Private feed (addressed by the audience's rss_token; invalidated on token reset; the token read path is in the [surfaces read surface](/api/rest#audience-surfaces)) |

The rules, groups, rosters, alerts, and incidents endpoints depend on their
corresponding components being configured (e.g. `rules:`, `groups_store`,
`rosters_store`); when not configured, these endpoints return 503.

Complete request/response fields are in the [REST API reference](./rest.md).

## Response format

Success response:

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

Error response:

```json
{
  "code": 400,
  "message": "invalid request"
}
```

Error responses **omit** the `data` field (omitempty) rather than emitting `null`.
Business-layer failures (e.g. all channels of a notification failing to deliver)
return HTTP 200, with the error code written in the body's `code`;
transport-layer errors (invalid parameters, nonexistent resources, etc.) are the
corresponding HTTP status codes.
