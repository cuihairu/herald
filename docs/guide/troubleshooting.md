# Troubleshooting Guide

Triage by "which step is stuck": check startup first, then sending, and finally delivery.

## Startup Fails Immediately

| Symptom / log | Cause and fix |
|-------------|-----------|
| `Unknown command: --config` | The start command is missing a subcommand. Correct form: `heraldd serve --config config.yaml` (or `heraldd worker --config ...`); `serve` cannot be omitted |
| `factory not found: builtin` | The provider's `type` is set to `builtin`. `type` must be a concrete type name: `type: telegram`, `type: aliyunsms`… For the full list see the [Provider Manual](/providers/overview) |
| `fcm: project_id is required` / `telegram: token is required`, etc. | Required configuration is missing; the error prefix is the provider name. Check the configuration item table in that provider's documentation |
| `unknown provider type` / `no-such` | `type` is misspelled; check the Provider Manual |
| `unknown queue type: banana` | `queue.type` only supports `memory` (default) / `redis` (remote workers do not accept memory) |
| `auth enabled but no api_keys configured` | `auth.enabled` is on but `auth.api_keys` is not configured |
| `address already in use` | `server.addr` (default `:8080`) or `websocket.addr` (default `:8081`) is already in use; change the port or stop the process holding it |
| `invalid config` | YAML syntax error; the message includes the line number / field |

Successful startup is indicated by `herald scheduler started` appearing in the logs.

## Environment Variables Not Taking Effect

You wrote `$VAR` in the config but the provider still receives the literal text:

- **Only the `$VAR` form is supported** (the value is replaced in whole when it starts with `$`); `${VAR}` is **not** expanded and is kept as-is
- The environment variable must exist in the **process environment that starts heraldd** (injected via `export` or `.env` by the deployment method), not in the frontend or curl's environment
- The config file is **read at startup**; restart the service after changing it

```yaml
# Correct
token: "$TELEGRAM_BOT_TOKEN"
# Wrong (will not be expanded)
token: "${TELEGRAM_BOT_TOKEN}"
```

## Send Failure (the notify call itself errors)

| Symptom | Cause and fix |
|------|-----------|
| `no route found for type=xxx level=xxx` | The request carries no `channels`, and neither `routes` (by type) nor `level_routes` (by level) matched. Add route table entries, or explicitly include `channels` in the request |
| The response body's `data.failed` lists the channels | The notification was accepted but delivery to some channel failed; error details are in `failed[].error` (**HTTP status is always 200**: on partial failure the body `code` is still 0, and only when everything fails is the body `code` set to 422). Deterministic errors are not retried; retriable errors are automatically re-sent with the backoff strategy |
| `provider not found` | `channels` contains an instance name that is not registered (no provider with that name in the config, or it is `enabled: false`) |
| Request 401 | `auth.enabled` is on; the `notify` endpoint requires an API Key (see the Authentication section of [Configuration Reference](/guide/configuration)) |

**Acceptance and delivery are two different things**: `{"code":0,...}` only means the notification entered the delivery pipeline; each channel's final outcome is shown by `data.accepted` / `data.failed` and the delivery logs:

```bash
curl "http://127.0.0.1:8080/api/v1/logs?limit=20"   # status field: success / failed / pending / shadow
```

`pending` also covers tasks waiting for a retry (in `/api/v1/logs` such a task has a single row, and `pending` flips to `success`/`failed` only at the terminal state); retry progress and `next_retry_at` are on the delivery status surface (`/api/v1/deliveries`, see [REST API](/api/rest#deliveries-list)), and the distinction between retry exhaustion (`dead`) and permanent failure (`failed`) is only visible there as well.

## Message Never Arrived (accepted but not received)

1. **Check the delivery logs**: `curl "http://127.0.0.1:8080/api/v1/logs?limit=20"` — look at the task's `status` and error message
2. **Check the worker**: is `queue.workers` 0, and did the worker start (the startup logs contain `worker pool started`)
3. **Check routing**: are the `channels` in the request really the instance names in the config (`curl /api/v1/providers` lists them)
4. **Check dedup**: duplicate notifications inside the `dedup.enabled` window are folded away (this is expected behavior); switch the `type` or disable dedup to verify
5. **Check channel-side errors**: look up each provider's common-errors table:

| Channel | Common errors table |
|------|-----------|
| Telegram | [telegram](/providers/telegram) |
| Feishu | [feishu](/providers/feishu) |
| WeCom | [wecom](/providers/wecom) |
| DingTalk | [dingtalk](/providers/dingtalk) |
| Slack / Discord | [slack](/providers/slack) · [discord](/providers/discord) |
| WeChat family | [wechat](/providers/wechat) · [wechatmp](/providers/wechatmp) |
| Email | [email](/providers/email) |
| SMS | [aliyunsms](/providers/aliyunsms) · [tencentsms](/providers/tencentsms) · [neteasesms](/providers/neteasesms) |
| Webhook / Log | [webhook](/providers/webhook) · [log](/providers/log) |

## Retry Not Taking Effect

- Only **retriable errors** are retried: request timeouts, HTTP 408/429/5xx, network unreachable. Credential errors, invalid tokens, nonexistent numbers — these 4xx are deterministic errors that a retry can never fix, so they are recorded as failed directly
- Retry count and intervals are in the `retry` configuration (`max` / `backoff` / `initial_delay` / `max_delay`); see [Configuration Reference](/guide/configuration)
- Email (direct SMTP) does not follow HTTP retry semantics; it returns on failure

## Remote Worker Issues

- `heraldd worker` rejects `queue.type: memory`: a remote worker must use a shared queue (redis)
- Worker cannot connect to the scheduler: check whether `worker.server_url` (the WebSocket address) and the scheduler's `websocket.addr` can reach each other
- Worker is up but delivery does not go through it: is the provider configured on the **scheduler** or **on the worker itself** — see [Worker Runtime](/runtime/worker)

## Still Not Working

1. Bypass verification: set the suspicious provider to `enabled: false` and it will not be selected; then use the `log` provider to verify the link itself (see step 1 of [Quick Start](/guide/getting-started))
2. Check logs and runtime status: `heraldd`'s structured logs + `curl /api/v1/status`
3. File an issue: include the config (redacted), the relevant `/api/v1/logs` entries, and the heraldd version

## Quick Reference for API Errors

First distinguish two response shapes: **business-result failure** (HTTP 200, the error code lives in the body's `code`; notify accepting with partial/total failure, and app-surface suppressed/refused, all fall in this class) and **transport-layer errors** (the HTTP status code is the body's `code`).

| Symptom (observed) | Cause | Fix |
| --- | --- | --- |
| HTTP 401 `{"code": 401, "message": "unauthorized"}` (note the space; hand-written envelope) | No credentials carried, or credentials invalid | On the management surface add `Authorization: Bearer` / `X-API-Key`; on the app surface check the seeded token |
| HTTP 401 `invalid username or password` | Wrong login password | Check the `auth.admin_user` configuration |
| HTTP 401 `invalid app credentials` | Wrong app token, wrong app name, or header missing | Self-check with `GET /api/v1/apps/{app}` to rule out these two first |
| HTTP 403 `token lacks trigger scope` | The token's permission set is insufficient | Use a token with the corresponding permissions, or add the scope when seeding |
| HTTP 404 plain text `404 page not found` (app surface) | The app was not seeded in the config | Have someone with configuration permissions seed the app (closes the whole surface) |
| HTTP 400 `invalid request` | The request body is not valid JSON | Check the JSON syntax and Content-Type |
| HTTP 404 `log not found` / `provider not found: x` / `rule not found: x` | The resource does not exist | Use the list endpoints (`/logs`, `/providers`, `/rules`) to confirm the name |
| HTTP 405 `method not allowed` (plain text) | The endpoint does not support that method (`templates/{id}` is the exception and returns JSON) | Check each endpoint's allowed-methods table |
| HTTP 422 `no route: no route found for type=... level=...` | `channels` was not carried and the route table did not match | Include `channels` in the request, or add `routes`/`level_routes` |
| HTTP 422 (body `code:422`) `all channels failed: [...]` | All channels failed delivery | Locate it channel by channel via `data.failed[].Error` (the key starts with a capital letter) |
| HTTP 200 `code:0` but `data.accepted: null` + `task_ids: null` | The notification was folded by the dedup gate (same content repeated within the window) | Expected behavior; switch the type or wait out the 5-minute window |
| HTTP 200 `suppressed: true` | App dispatch repeated within the same dedup_key window | Expected behavior; if a resend is genuinely required, change the dedup_key |
| HTTP 200 with a `refused` list | The audience has no outlet (relation / Intensity / ContactSurface is missing one, or the audience string has no landing point) | Map by `reason`: `intensity_exceeded` — switch to a category with higher Urgency; `phone_disabled` — check the phone gate; `no_channels` — check the ContactSurface (when there is no bound surface and the audience string is neither a channel instance name nor a channels block entry, first check the id spelling); `filtered` — check the channel × relation matrix |
| HTTP 409 `rule already exists: x` / `group already exists: x` / `category already registered with a different default urgency` | The resource already exists (rule / group / category default Urgency conflict) | Rules go through PUT to replace; groups go through PUT; for a category, rename it or change back to the original default value |
| HTTP 503 `rules engine is not configured` / `ack store is not configured` / `audit trail not configured`, etc. | The corresponding component is not enabled in the config | Add the corresponding configuration block and restart |
| App callback not received | The callback surface is not configured / non-2xx / secret mismatch | Check url/has_secret with `GET /apps/{app}/callback`; for signature verification recompute the HMAC using the secret from PUT |
