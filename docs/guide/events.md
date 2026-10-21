# Events and Alert Model

The four questions integrators ask most often: how are levels determined, why is the same alert not resent, is delivery guaranteed, and is ordering guaranteed. This page states all of those rules in one place.

## Three Vocabularies for Levels

Herald has one vocabulary at each of three layers, with fixed mappings between them:

| Layer | Field | Values | Who uses it |
| --- | --- | --- | --- |
| Notification layer | `level` | Conventionally `info` / `warning` / `error` (`critical` is also valid; the dashboard colors by the four levels); **no enum validation — any string passes through** | `POST /notify`, rule expressions, `level_routes` routing |
| Urgency | `urgency` | `routine` / `normal` / `urgent` / `critical` | app dispatch, category default values, delivery strategy |
| Event adaptation surface | `severity` | `critical` / `warning` / `info` — **only these three values are recognized** | `POST /apps/{app}/events` |

Fixed mapping for the event adaptation surface (any other value is a 422; nothing is guessed or clamped):

| severity | urgency |
| --- | --- |
| `critical` | `critical` |
| `warning` | `urgent` |
| `info` | `normal` |

Urgency sets the ceiling for channel matching — a channel's Intensity (L0–L5, from RSS pull-style up to a phone hard interruption) must not exceed the Urgency cap:

| urgency | Intensity cap |
| --- | --- |
| routine | L1 (email) |
| normal | L2 (webhook / in-app) |
| urgent | L4 (SMS) |
| critical | L5 (phone) |

Default channel Intensity: `rss`/`feed` → L0, the `email` family → L1, `webhook` → L2, the `sms` family → L4, `phone`/`call` → L5, and **channels not listed here (including `log`) default to L3**. A channel that exceeds the cap is marked `refused` in the dispatch result, with reason `intensity_exceeded`.

## Dedup and Idempotency

The dedup gate acts at the "should we send, or not" layer, with a fixed latch order: **event idempotency → state machine → frequency control**.

| Layer | Trigger | Effect |
| --- | --- | --- |
| Event idempotency | The same `event_id` appears again (content drift is caught too) | Suppressed, reason `idempotent` |
| State machine | The same `dedup_key` + the same `state` reported repeatedly | Suppressed, reason `state-repeat`; a state flip resends and opens a new round |
| Frequency control | The same dedup key appears again within the window | Suppressed, reason `once` or `throttled` |

The three frequency control tiers are configured per category: `once` (once per process lifetime), `throttle` (once per window; the window defaults to 5 minutes and can be overridden per category), `always` (let everything through). Default tiers: `system` → once, `alerts` → throttle, everything else → throttle.

Dedup key: an explicit `dedup_key` takes priority; if none is supplied, a normalized hash is computed over the content (type/level/template/channels/recipients/params/title/body). **Note: a change in level is a different key** — the same fault escalating from warning to error will resend. This is a feature, not a defect.

A suppressed notification returns `suppressed: true` with HTTP 200 and does not appear in the delivery records; the expanded detail (`×N`) is on the `delivery.deduped` event in the audit stream.

Both the idempotency table and the dedup table are **in-memory process tables** (capacity 4096–1000, FIFO eviction), and are cleared on restart — the semantics are "resend once after a restart", not silent drop. Callers that need idempotency across restarts must persist and dedup on `event_id` themselves.

`POST /notify` additionally has a request-body-level `idempotency_key`: repeated requests with the same key return the first accepted result (the same notification_id) and produce no new deliveries. Only successfully accepted results enter the table; a request that failed entirely can be corrected and retried with the same key.

## Delivery Guarantees

**At least once, best effort; no ordering guarantee.**

- Each notification fans out into multiple delivery tasks, each retrying independently. Retriable errors (timeouts, HTTP 408/429/5xx, network failures) are re-sent per the backoff strategy, default `max: 3` (4 attempts in total), exponential backoff 1s → 1m; an upstream 429's `Retry-After` takes precedence over the backoff curve, capped at `max_delay`.
- Deterministic errors (invalid credentials, nonexistent numbers, rejected requests, and other 4xx) are **not retried** and are recorded as failed directly.
- A task that ultimately fails lands in `failed` state in the delivery logs; while retrying it is `pending`, one row per task.
- Concurrent consumption by multiple workers, requeueing, and fan-out all scramble ordering; herald does not promise delivery order and provides no ordering compensation. Callers that need ordering should carry a sequence number in the content and handle it themselves.
- Queue implementations: `memory` (default; tasks still in the queue are lost on restart) and `redis` (Streams; tasks survive restarts, but a crash within the delayed-resend window may lose one due task).
- App callbacks are likewise at least once: events go through the same queue and retry pipeline as delivery, non-2xx is retried, duplicates are possible, and receivers dedup on `event_id`.

Acceptance and delivery are two different things: `code:0` / `accepted` only means it entered the delivery pipeline; each channel's final outcome is shown by the delivery logs (`/logs` or `/apps/{app}/deliveries`) and the callbacks.

**RSS channel exception**: deliveries routed to `rss` produce no delivery task; they are projected in place as feed entries that subscribers pull — an empty `accepted` is expected behavior.

## The Lifecycle of an Event

A rule-routed delivery opens an event ledger (incident) entry: `open` → acknowledged (`POST /alerts/{id}/ack` or the Feishu card button) → resolved (when the rule expression no longer matches, it resolves automatically and sends a recovery summary). Acknowledgment is idempotent: the first one takes effect, repeated acknowledgments return the same record, and acknowledging in time cancels a pending escalation chain.

For rules with `escalation` configured, if nobody acknowledges within `ack_timeout` the notification is resent to the escalation channels (bypassing rule evaluation and dedup). The alert id comes from the notification's `params.alert_id`, falling back to the content dedup key when absent.
