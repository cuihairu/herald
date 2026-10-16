> English · [中文](./design-audience-boundaries.md)

# Conceptual Boundaries and Layering Audit

> Status: **current ruling** (2026-10-07, adjudicated item by item against an external architecture review). This document answers one question: what each word in the audience layer governs, and who may not intrude on whom. Term meanings are defined in the [general model §2](./design-audience-model.en.md) (the single source of truth); this document is the **boundary ruling** — the conclusions of the overlap sweep, the layering map, and the red lines that later batches must check against before landing. Where the two documents conflict, general model §2 prevails and this document is amended to follow it.

## 1. Positioning statement

Herald is **lightweight notification orchestration and delivery infrastructure**.

The word "orchestration" is what this review nailed into the positioning: Herald was never just `send()` — it decides **who, when, through which channel, at what intensity, whether to aggregate, whether to filter, whether to escalate, and whether to dedup**. The delivery pipeline (landed through Phase 0-9) is the foundation; the audience layer (the unified subscription and delivery hub) stands up "the notified party decides"; orchestration is the behavior description on top of the two. The positioning sentence has been synchronized in three places: README, general model §1, and the architecture overview.

## 2. Five-layer map

The system's layering boundaries — any feature in a later batch must first answer "which layer does this land in" before work starts:

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ Integration layer             API (/notify /audience /feeds) · Go SDK ·      │
│                               webhook callbacks · Source adapters            │
│                               (bot / official account / in-app checkbox)     │
├──────────────────────────────────────────────────────────────────────────────┤
│ Audience & preference layer   Registry · SurfaceRegistry ·                   │
│                               PreferenceRegistry · Relation                  │
│                               (subscription/enrollment) · Filter/matrix ·    │
│                               audit trail                                    │
├──────────────────────────────────────────────────────────────────────────────┤
│ Orchestration layer           rule engine · route expansion ·                │
│                               dedup/frequency control · Digest aggregation   │
│                               · Urgency×Intensity matching · delivery mode   │
│                               execution (escalation chain / single channel / │
│                               parallel)                                      │
├──────────────────────────────────────────────────────────────────────────────┤
│ Delivery runtime layer        Planner→Task · Queue · Worker · Retry · ack ·  │
│                               state machine                                  │
├──────────────────────────────────────────────────────────────────────────────┤
│ Provider layer                telegram / email / webhook / wechatmp / …      │
│                               (swappable implementations)                    │
└──────────────────────────────────────────────────────────────────────────────┘
```

Code placement (dual-resident components note their primary layer):

| Layer | Packages/modules |
| --- | --- |
| Integration layer | `api/` (HTTP surface), `worker-sdk/go/`, Source adapters and webhook callbacks (batch 8/11) |
| Audience & preference layer | `core/audience/` (registry / contact surfaces / preferences / filters), `core/audit/` |
| Orchestration layer | `core/rules/`, `core/route/` (routing table), `core/dedup/`, `core/digest/`, `core/escalation/`; Intensity×Urgency matching and the three-mode executors (batch 9) |
| Delivery runtime layer | `core/service/` (planner/pool — orchestration decisions become tasks here), `core/queue/`, worker, `core/retry/`, `core/ack/` |
| Provider layer | `providers/builtin/`, `core/errclass/` (the error vocabulary belongs to the provider contract) |

## 3. Ruling on the eight terms: no overlap

The concepts the review called out for an overlap sweep — audience / channel / ContactSurface / provider / Preference / delivery mode / Urgency / Intensity — are adjudicated word by word below. Conclusion: **each answers a different question; no merging, no renaming**.

| Term | Owning side | Question it answers | Boundary red line |
| --- | --- | --- | --- |
| `audience` | identity | "who gets notified" | the single system-wide ID; stores no channel credentials; no further peer identity entities are derived (§5) |
| `channel` | reach mechanism | "which class of conduit" | carries three attributes — Intensity, timeliness, and **delivery direction** (§4); channel ≠ provider |
| `ContactSurface` | credential instance | "this audience's concrete address on this channel" | the channel's runtime binding state; the recipients/endpoints in static config are only its seed |
| provider | delivery implementation | "which implementation carries the delivery" | a channel is expanded into provider instances through the channels block; the same channel can swap implementations (email→smtp or resend); the Intensity ladder is calibrated per channel class and the provider inherits its level |
| `Preference` | user choice | "what the audience itself wants" | can only narrow, never widen (same for the silence floor and frequency control); stored on the subscription relation |
| `Urgency` | **message side** | "how urgent this matter is" | determines the allowed Intensity range (detailed design §6.2); category-level default mapping, overridable per event |
| `Intensity` | **channel side** | "how intrusive this channel is" | the L0–L5 ladder; linked to Urgency by the §6.2 matrix — one lives on the message, the other on the channel, and they never meet elsewhere |
| delivery mode | **orchestration side** | "how to deliver: escalation chain / fixed single channel / parallel" | one policy component entry point with different executors (batch 9); orthogonal to rule-engine escalation (rule escalation = who to call; mode escalation = how to call louder) |

The review's worry that "`Intensity=high` / `Urgency=high`" are indistinguishable does not hold in this repo, because the two **never share a side**: Urgency is an attribute of the message, Intensity is an attribute of the channel, and the two meet only in the matching matrix (detailed design §6.2). The axis the review proposed — "Intensity = how strong the delivery policy is" — **already exists**, and its name is delivery mode (escalation/fixed/parallel); no third "strength" word will be coined.

## 4. Delivery direction: Push / Pull

The review pointed out that RSS changes the delivery model (push vs pull). Correct. Ruling:

- Add **delivery direction** (push / pull) as a channel attribute: push channels are delivered proactively by Herald (telegram/email/SMS/phone/in-app message); pull channels are polled by the reader (RSS).
- **RSS is a pull channel, not a kind of provider**: an RSS feed is a pull projection of delivery records (detailed design §9) and goes through the same "relation permits × ContactSurface bound" validation.
- **Naming avoidance**: the name "Push/Pull Delivery Mode" is not adopted — "delivery mode" is already taken by the three-way choice of escalation chain / fixed single channel / parallel (general model §2 glossary), and one word cannot carry two meanings. The direction axis is named "delivery direction" and written into the channel attributes.

Batch 7 (RSS) has landed under this wording: `core/feeds` stores the pull projection, `/feeds/**` is the integration layer's pull-side outlet, rss-class channels are projected in place in the delivery pipeline without producing provider tasks, visibility validation reuses the audience layer (token ownership via SurfaceRegistry, category admission via the relation registry + matrix recheck), and no new delivery state machine branch is added; multi-address disaster tolerance belongs to the reader side and is not codified.

## 5. Idempotency / dedup / frequency control / aggregation: four matters, four gates

The review's four-way split lines up with this repo's current state (detailed design §10/§11). The wording is tightened as follows; the four concepts are never conflated:

| Concept | What it solves | Gate position | Current state |
| --- | --- | --- | --- |
| Idempotency `idempotency_key`/`event_id` | **duplicate submission** of the same event (retry storms, upstream resends) | very front of the pipeline | landed (Phase 0) |
| Folding / state-machine dedup | **duplicate delivery** of equivalent content (×N counting, send only on state flip) | orchestration layer, before delivery and before the escalation chain | folding landed; state machine in batch 10 |
| Frequency control once/throttle/always | the **rate** cap within a time window | after dedup | batch 10 |
| Aggregation Digest | **folding multiple distinct messages into one summary** (audience × category × time window) | independent bypass; the summary goes through the full delivery chain | landed (batch 6) |

Order: idempotency → folding/state → frequency control → (bypass) aggregation → delivery. The audit has independent event vocabulary for every gate (`delivery.deduped` etc.), answering "why was this one not delivered".

## 6. Point-by-point receipt for the review's seven items

| # | Review point | Disposition |
| --- | --- | --- |
| ① | Positioning upgraded to "notification orchestration and delivery" | **changed**: the positioning sentence in general model §1, README, and the architecture overview now includes "orchestration" (§1) |
| ② | Surface established as a core concept | **already the status quo**: the ContactSurface chain audience → recipient → ContactSurface → channel → provider (detailed design §2 key constraints); this round adds the explicit sentence "channel ≠ provider, implementations are swappable" to general model §2 |
| ③ | Push/Pull established as a delivery-model axis | **adopted with a rename**: lands as the channel attribute "delivery direction", avoiding the word "delivery mode" (§4) |
| ④ | Urgency/Intensity redefined or merged | **verified as already layered**: message side vs channel side, connected by the §6.2 matrix; the "policy strength" axis is already carried by delivery mode (§3) |
| ⑤ | Idempotency/dedup/frequency control/aggregation split four ways | **already split four ways**, wording tightened (§5) |
| ⑥ | Redis is not a hard dependency of Digest | **batch 6 landed already compliant**: `digest.LeaderLock` is constructor-injected; with no `redis_addr` in config it is nil and a single-machine in-process timer runs directly; the lease is configured only for multiple instances. The lock implementation is swappable (the interface depends only on SET NX / renew / release semantics) |
| ⑦ | Stop adding audience sub-concepts | **established as a red line** (§7) |

## 7. Red line: the audience layer entity cap

The audience layer entity list is **capped** at seven: **audience, recipient, Endpoint, ContactSurface, relation (subscription/enrollment), Preference, aggregation rule**. The peer entities the review named — `Subscriber / Member / Contact / Identity / Profile / Target / Destination` and the like — **will not be added**: audience is identity, subscriber/recipient are relation roles, and this layering (detailed design §1) already covers every sense of the words above. New requirements always land as **attributes or relations of existing entities**; if an eighth entity is genuinely needed, first amend the general model §2 term contract and run the eight-term sweep of §3 of this document, then touch code.

## 8. Batch receipts and follow-on discipline

The review's graded judgments on batches 1-11 against landing status (landing details in [general model §13](./design-audience-model.en.md)):

| Batch | Review judgment | This repo's disposition |
| --- | --- | --- |
| 1 Relation model | very reasonable | ✅ landed |
| 2 ContactSurface and binding | reasonable, a core capability | ✅ landed |
| 3 Preference center | very reasonable | ✅ landed |
| 4 Relation filtering | very important | ✅ landed |
| 5 Audit | should be kept | ✅ landed |
| 6 Digest | reasonable but postponed | ✅ landed (redis-optional compliant); the review's "postponed" meant it was not landed at the time, and is not retroactive |
| 7 RSS | should be defined as a pull channel | ✅ landed per §4 (`core/feeds` + `/feeds/**`; the pull projection produces no provider tasks) |
| 8 Source adapters | reasonable | ✅ landed as "subscription entry point adapters" (`SourceAdapter` action normalization + bot / official account / in-app endpoints + `Reconciler` periodic reconciliation; detailed design §8 landing section) |
| 9 Intensity and modes | valuable, boundaries need pinning | ◑ landed per the §3 eight-term ruling — the three axes Urgency/Intensity/delivery mode are not merged |
| 10 Dedup and frequency control | must be done | ◑ landed per the §5 four-way split |
| 11 Integrator API/SDK | very reasonable | ◑ token permissions start minimal per the review: notify/query first; config-class permissions are tightened and stay closed until the admin-surface review |

**Follow-on discipline**: before starting each of batches 7-11, first check against the §2 five-layer map to answer "which layer does this land in", and against §3/§5 to answer "which existing word does this use"; implementations that conflict with this document do not pass the gate.
