> English · [中文](./design-audience-deferrals.md)

# Deferred-Item Unfreeze Plan (3 items; ①③ unfrozen and landed, ② fully closed)

> Status: **① unfrozen and landed (option B), ② fully closed (audiences write path landed + recipients/channels write paths kept by proxy decision), ③ landed**. This document carries forward the 3 deferred items that the
> [Audience refactor audit (archived)](./design-audience-audit.en.md)
> decided on 2026-10-05 to leave undone, listing for each the current facts, the optional paths, and the consequences. Audit round eleven
> confirmed: apart from these 3 items in todo.md there are no other unfinished items, the repo has no open issue/PR, and ten re-sweeps of the
> doc surface found no new deltas — the audit had entered pure re-sweep territory, so per the patrol instruction work moved to this document. Every item's current state carries
> file:line; options are A/B/C, and the recommended baseline for all is A (keep the status quo, i.e. the 2026-10-05 decision).

## Overview

| # | Item | Where it is stuck | What unlocking needs |
|---|------|--------|----------|
| ① | Logs vs Delivery state separation | **unfrozen (2026-10-14, user named it for work, option B)** | not needed — see the unfreeze record below |
| ② | audiences/recipients/channels runtime API | **fully closed** — the audiences write path has landed (`8193172`); the recipients/channels write paths were decided by proxy (patrol R1): keep pure config | not needed — the proxy-decision record is in section ② below |
| ③ | group member channels-block expansion | **unfrozen (2026-10-13, patrol assigned the work, option B)** | not needed — landed in `2a857ff`, docs synced in `9b741ef` |

---

## ① Logs vs Delivery state separation

**Current facts (one copy of data, two read surfaces)**

- `core/logstore/logstore.go:19-48` the `TaskLog` struct: the sole carrier of task execution on the API. `Status`
  is the 4-word wire vocabulary `success/failed/pending/shadow` (`:24`); shadow is rule-engine dry-run
  observation (`:30-38`).
- `core/types.go:65-77` the `DeliveryStatus` 7-value state machine
  (accepted/queued/delivering/retrying/delivered/failed/dead), living on
  `DeliveryTask.Status` (`:94`) — **not exposed via the API**: the `/api/v1/logs` family
  (`api/server.go:252-254`) reads only `TaskLog`, and the queue endpoint reports only size (Phase 9 record,
  audit archive :212).
- Both are driven by the same write path: `core/runtime/manager.go:103-220` writes
  `TaskLog` at seven task transition points, and `retry_count` evolves with the task's in-memory state.

**Decision record**: 2026-10-05, keep it undone — the highest-cost form of "log/state separation" (would
require introducing an event stream or splitting storage); one copy of data read two ways already meets the need (audit archive :216).

**Options and consequences**

| Option | Approach | Consequence |
|------|------|------|
| A Keep the status quo (recommended baseline) | do nothing | wire has only 4 vocabulary words; the `delivering/retrying` intermediate states and the `dead` terminal state are visible only in memory and as derived log views; report wording is constrained by the wire vocabulary (troubleshooting.md was already fixed to the wire vocabulary) |
| B Split storage + an independent query surface | new `DeliveryStore` (task ID key, independent retention) + `/api/v1/deliveries`; logs keep historical-event semantics | new store, new API, dashboard rework; highest cost — exactly the reason it was deferred |
| C Event stream | reuse the `core/audit` 7 event family (`delivery.deduped` etc.), extend to per-attempt events, logs become a projection | doubles the write path but yields full state history; medium-high cost |

**Unlock**: ~~the user picks A/B/C; picking B/C requires wording on whether to add an API surface.~~ → **Unfrozen** (2026-10-14, the user named it for work; implemented as **B**).

> 2026-10-14 unfreeze record: the patrol work order said "the user now names ① for work"; after weighing the plan, **B (split storage + an independent query surface)** was selected. Rationale: A keeps the status quo (the user explicitly wants it done, so it is ruled out); C (event stream) demotes logs to a derived projection of audit events — the state machine has no home of its own, it couples ops log records to the audit event schema, doubles the write path, and does not match the work order's shape ("the two places no longer write each other's state + Delivery owns the state machine and retries"); B means a new store + new API + dashboard rework, but its incremental cost is bounded (Phase 3 already enumerated the 7-value state machine, logstore already has a retention model, and the API/dashboard patterns are established), and it is the only option matching the work order's shape. Implementation: a new `core/deliverystore` (task ID key, independent retention ring) owns the state machine and retry evolution; `core/runtime/manager.go` writes to the delivery store at every state transition point (delivering / defer-retrying / settle terminal); `core/logstore` narrows to fact events (send / result / elapsed / error code), the `status` field's semantics become the event outcome, and the dead/failed distinction lives only in the delivery store; a new `GET /api/v1/deliveries` (list/stats/single) reads the delivery store, and the §13.4 app deliveries query surface switched from logstore to the delivery store; the dashboard gained a "delivery status" page; the apps-sdk §13.4 read port was synced to the `deliveries` key and lifecycle vocabulary (`6534269`). Landed as `0effddf`+`6534269`; evidence lines in todo and [REST](/api/rest#deliveries-list).

## ② audiences/recipients/channels runtime API

**Current facts (entities are pure config; the runtime write surface covers only the relation side)**

- audiences/recipients/channels are all config, with startup validation refusing to start (audit archive :64, :99, Phase 7
  :164-173); there is no runtime create/update/delete — changing an entity = editing YAML + restarting.
- The runtime API covers only **relations and contact surfaces**: `/api/v1/audiences/{id}/relations` read
  (`api/server.go:311`, GET), `/subscriptions` subscription toggle (`:318`, POST/DELETE
  `api/handler_sources.go:214`), `/surfaces` contact-surface binding (`:322`,
  `api/handler_surfaces.go:32`, one-time token). The audiences/recipients/channels entities
  themselves have no write path.

**Decision record**: 2026-10-05, the phase-one wording was pure config (audit archive :217).

**Options and consequences**

| Option | Approach | Consequence |
|------|------|------|
| A Keep the status quo (recommended baseline) | do nothing | ops cadence = file + restart; an integrator adding audiences/recipients/named channels programmatically must go through the config change process |
| B Minimal write path (audiences) | `POST /api/v1/audiences` writes into the runtime registry, YAML stays the bootstrap seed | the registry behaves as in-memory state (loaded from YAML); runtime additions need persistence; validation must line up with config `Validate()`; medium cost |
| C Full CRUD + persistence + write-back | full runtime create/update/delete on audiences/recipients/channels, with config write-back | the dual-source-of-truth problem: file vs runtime priority and the write-back format need an extra ruling; highest cost |

**Unlock**: ~~the user picks the scope (audiences only / all three entities) + the persistence wording (whether to write back to the config file)~~ → **partially unfrozen** (2026-10-14 the user decided to unfreeze, minimal write path for audiences).

> 2026-10-14 unfreeze record: a patrol AskUserQuestion decided "unfreeze ② audiences/recipients runtime
> API"; implemented as **option B (minimal write path, audiences) + file persistence** (same semantics as the
> group store): a `Store` interface + a JSON `FileStore` (the `audiences_store` config, atomic writes) + a Manager
> live table (Reload/Put/Delete; a seed with the same id overwrites the stored entry) + `GET/POST /api/v1/audiences`
> and `GET/PUT/DELETE /api/v1/audiences/{id}`. The write paths for recipients (the recipient/Endpoint
> tables) and the channels block **remain deferred** (that is the only part of the write surface left, awaiting
> another decision). Landed in `8193172`; see todo,
> [configuration](/guide/configuration), and [REST](/api/rest#audiences-list).

> 2026-10-14 decided by proxy (patrol R1): the recipients/channels write paths **stay pure config** (no new
> write paths), closing ② entirely. Rationale: the design boundaries already assign "changing addresses at
> runtime" to the ContactSurface API — `SurfaceRegistry`'s
> bind / rebind / invalidate (`core/audience/surfaces.go`, audienceID → channel → ContactSurface
> as an independent runtime store), with the static `recipients/endpoints` as only its seed
> ([boundaries](./design-audience-boundaries.en.md)); adding CRUD write paths would create a second
> runtime path for the same concept (dual source of truth), conflicting with the established
> boundaries, with no consumer waiting.

## ③ group member channels-block expansion

**Current facts (member channel name = provider instance name, resolved literally)**

- `core/service/notification.go:675-683`: when a `group:` reference is expanded, members become
  `deliveryTarget{channel: m.Channel}` directly; the channels-block lookup
  (`:696-704`, explicit provider > channels block > routes table) applies only to top-level
  references, **not to member channels**.
- `core/groups/groups.go:32-39` the `Member.Channel` comment is exactly "provider instance name (never
  another group reference)". If a member channel name collides with a channels-block key, it is delivered as
  the literal provider, and a miss fails at delivery time (a familiar failure mode).

**Decision record**: settled as not doing it — member semantics are the provider instance name (audit archive :218, Phase 7 deferral :173).

**Options and consequences**

| Option | Approach | Consequence |
|------|------|------|
| A Keep the status quo (recommended baseline) | do nothing | groups cannot reuse the channels block; when a block is renamed, members do not follow — the YAML member list must be edited |
| B Member channels go through the same resolution chain | reuse the `:696-704` channel arm to expand member channels | when a member name collides with a block key it **silently fans out** to multiple providers, changing existing delivery semantics; existing configs need a migration note; small cost (code reuse + tests) |
| C Explicit prefix opt-in | new syntax such as `channel: "channel:ci"` to opt into block expansion | no silent behavior change; the new syntax must go into docs and validation; small-to-medium cost |

**Unlock**: ~~the user picks A/B/C~~ → **Unfrozen** (2026-10-13, patrol assigned the work; implemented as B).

> 2026-10-13 unfreeze record: the patrol assigned "start the most valuable item", and this one was chosen as
> the smallest self-contained change; the option was implemented as **B** (chosen at implementation time;
> rationale: the grounds for "existing behavior unchanged" — an explicit provider instance wins over the block at
> both ends of the resolution chain (`resolveChannel` checks the provider first), and fan-out happens when a
> member name collides with a block key; such a collision previously "failed at delivery as a missing
> provider", and fan-out only turns the currently-failing reference into a reachable one, changing no
> configuration that ever worked). Landed; see todo and [configuration](/guide/configuration).

---

> After a decision: the corresponding section of this document marks the chosen option and the executing commit, the three todo.md lines become `[x]` evidence lines; unchosen
> options stay `[ ]`. No code is touched before a decision.
