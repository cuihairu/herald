> English · [中文](./design-audience-model.md)

# Audience Domain Model (General Model)

> Status: **current model** (rewritten 2026-10-07). This document is the single general model of the Herald domain model: terminology, boundaries, and the full picture of the audience-layer extension. The item-by-item detailed design of relations (flow diagrams / matrix / parameter tables) lives in
> [Audience Subscription and Delivery Hub](./design-audience-relations.en.md) (hereafter "relation detail design");
> the rulings on conceptual layering and responsibility boundaries live in [Conceptual Boundaries and Layering Audit](./design-audience-boundaries.en.md);
> the execution history of the Phase 0-9 pipeline rework lives in [Status Audit](./design-audience-audit.en.md) (archived).
> Iron rule: if the plan changes, change this document first — when code and this document disagree, this document is the authority for filing a batch of corrections.

## 1. Positioning

Herald is **lightweight notification orchestration and delivery infrastructure**. It decides more than just "send it out": who, when, through which channel, at what intensity, whether to digest, whether to filter, whether to escalate, whether to dedup — these orchestration decisions are consolidated by Herald, and the delivery pipeline (Phase 0-9, already landed) is the foundation of this facet (see [Status Audit](./design-audience-audit.en.md)).

On top of this, Herald's positioning is a **unified subscription and delivery hub**: completing the shift from "the application actively pushes" to "**the notified party decides**". The application (the business-system integrator) only holds an audience ID; who receives which category, on which channel, at what frequency, is decided by the audience's own relations, not hardcoded by the caller.

```text
Old: application ──"send to telegram:123456"──> Herald ──> channel
New: application ──"alert category, notify user:alice"──> Herald ──> relation filter ──> ContactSurface ──> channel
                                                                    ▲
                              the audience's own subscription/preference/unsubscribe decides what is allowed through here
```

## 2. Terminology Contract (single authority)

Documentation and code terminology correspond one-to-one; a word has only one meaning. Site-wide documentation (guide / architecture / API / design) uses the terms of this table uniformly:

| Term | Code identifier | Meaning |
| --- | --- | --- |
| audience | `audience` (references `group:<id>` / `user:<id>`) | The identity subject, unique across the whole system; the application only holds the audience ID and never touches channel credentials |
| recipient | `recipient` | A named individual who is a member of an audience (the `recipients` static config entity). Note the distinction from the "recipient **role**" in an enrollment relation — a recipient is an identity, a recipient role is a relation role |
| Endpoint | `Endpoint` | A recipient's static delivery address (type + target) |
| ContactSurface | `ContactSurface` | The reachable channel credential an audience binds at runtime (TG chat_id, email, WeChat official-account openid, RSS private token), with pending/active/invalid states |
| subscription relation | `RelationSubscription` / `Subscribe()` | A relation the audience **actively** checks: category × channel × frequency, fully autonomous and unsubscribable; the audience role in the relation is called a **subscriber** |
| enrollment relation | `RelationEnrollment` / `Enroll()` | A relation that is **passively** included: admin group broadcast, operations outreach, system notification; the audience role in the relation is called a **recipient** (recipient role) |
| Policy | `Policy` (`AllowUnsubscribe` / `MustDeliver`) | The right bits hung on a relation: unsubscribe right, must-deliver marker |
| Source | `Source` (`bot` / `wechat_mp` / `preference_center` / `admin` / `app:<name>`) | Which entry-point adapter the relation and binding came in through; the audit records by this |
| category | category | The classification dimension of business notifications (alert / billing / domain / announcement / system / marketing), the classification axis of a relation |
| channel | channel | A means of reach (telegram / email / in-app message / RSS / SMS / phone), with three attributes: intensity, real-time-ness, and delivery direction (push/pull). channel ≠ provider: a channel is expanded through the channels block into provider instances, and the same channel can swap implementations (email→smtp or resend) |
| channels block | `channels` config block | The static expansion of named channel → set of provider instances (the existing delivery config; orthogonal to the category dimension, see §6) |
| Preference | `Preference` | The category × channel × frequency triple selection on a subscription relation |
| Digest | `Digest` | Collecting multiple events into one summary by audience + category + time window |
| Urgency | `Urgency` (routine / normal / urgent / critical) | The urgency level of a message, which decides the allowed channel intensity range |
| Intensity | `Intensity` (L0–L5) | The channel's disruption-intensity ladder: pull < email < in-app message < IM < SMS < phone |
| delivery mode | escalation / fixed / parallel | escalation chain / fixed single channel / multi-channel parallel, pick one of three, configurable |
| frequency control | once / throttle / always | Three dedup/frequency-control tiers: once only / window throttle / allow repeats |

An audience is an **identity**; a subscriber/recipient is the audience's role in some **relation**. There is no parallel "subscriber" identity entity — the application side only integrates with a single set of audience IDs.

## 3. Core Chain

```text
Event ──> Rule ──> Notification ──┬──> Audience (audience reference)
                                  └──> Template (render)
                                        │
                 audience layer (§5): relation filter (subscription/enrollment × channel matrix)
                              └──> ContactSurface (reachable credential, §5.2)
                                        │
                                        ▼
                    Routing (expand into delivery tasks)──> dedup/frequency control (§8)──> delivery mode (§7)
                                        │
                                        ▼
                    Delivery Task ──> Queue ──> Worker ──> Provider
                                        │
                    bypass: Digest time-window aggregation (§10)／RSS pull-mode exit (§10)
```

## 4. Conceptual Boundaries

```text
Notification ≠ Delivery Task        one notification ≠ one channel delivery
Audience ≠ Channel ≠ Provider       notify whom / means of reach / how to send, three things
recipient ≠ Endpoint                a named individual ≠ a static delivery address
Endpoint ≠ ContactSurface           Endpoint is a static config address; ContactSurface is a
                                    runtime-bound, stateful credential that can be invalidated and rebound
subscription ≠ enrollment           active checking and passive inclusion are the same table but different meanings: unsubscribe right,
                                    channel matrix, audit are all separate (relation detail design §4)
relation ≠ ContactSurface           willingness/obligation (can you receive) ≠ reachability (can it be reached)
Routing ≠ Delivery                  decision (who receives what) ≠ execution (send it out, retry)
rule escalation ≠ intensity escalation  rule-engine escalation answers "who to call"; intensity escalation chain answers
                                    "how to call louder", the two are orthogonal and stackable
```

## 5. Audience Layer

### 5.1 Identity: audience → recipient → Endpoint

An audience has two forms of reference: `group:<id>` (group, member expansion see [Notification Group Design](./design-notification-groups.en.md)) and `user:<id>` (a named individual). Static config model:

```yaml
audiences:            # audience → recipient
  ops:
    recipients: [alice, bob]
recipients:           # recipient → Endpoint
  alice:
    endpoints:
      - { type: telegram, target: "123456" }
      - { type: email, target: "alice@example.com" }
```

The config is validated at startup, and invalid input means refuse to start (a `user:` reference never silently falls through). This static table is the **seed** of ContactSurface (see next section); existing usage without the ContactSurface extension behaves completely unchanged.

### 5.2 ContactSurface

The runtime attachment point for channel credentials. The application side only handles audience IDs and one-time binding tokens and **never touches credential plaintext**; rebinding requires confirmation from the old channel, preventing one deep link from snatching someone else's notification channel.

| State | Meaning | Delivery behavior |
| --- | --- | --- |
| `pending` | Binding issued but not redeemed / rebind awaiting old-channel confirmation | do not deliver |
| `active` | Redeemed and usable | deliver per relation |
| `invalid` | External platform judged it invalid (reconciliation / chat unreachable / unfollow backflow) | do not deliver, awaiting rebind |

Binding goes through a bot deep-link one-time token (expires in 15 minutes, valid once); each audience automatically holds an RSS private token with its registration (zero binding cost). Flow diagrams and rule details are in relation detail design §3. **Landed**: `core/audience.SurfaceRegistry` (batch 2).

### 5.3 Relation

The only legitimate basis for delivery: at send time, validate the intersection of "**relation allows × ContactSurface bound**"; if the intersection is empty, do not deliver and audit. Even though the two kinds of relation share an implementation table, semantics, unsubscribe policy, and audit must be kept separate:

| | subscription | enrollment |
| --- | --- | --- |
| Initiator | the audience checks it themselves | admin / operations / system |
| Unsubscribe right | fully autonomous | bottom line preserved (see below) |
| Interface | `Subscribe()` | `Enroll()` |

The bottom line of an enrollment relation: marketing/operations types must be unsubscribable; system must-deliver types are explicitly marked and limited to legal/contractual/security-obligation content; the channel and frequency caps are always manageable by the user. A relation row carries `type` + `source` + `policy`, queries and audits are split by type, and there is no universal relation interface. **Landed**: `core/audience.Registry` (batch 1). Data model and flow diagrams are in relation detail design §4.

## 6. Channels and the channel × relation Matrix

channel (means of reach) and provider (sending implementation) are kept separate: channel is a word from the audience's perspective, provider is a word from the integration's perspective; the existing `channels` config block handles the static expansion of "named channel → set of providers", and the category dimension (alert/billing/…) is registered as the integrator API lands (relation detail design §13.2).

The available channels follow the relation type, and are not one set. At send time, validate the three-way intersection:

```text
delivery target = ContactSurfaces the audience bound ∩ channels the relation type allows ∩ intensity range the Urgency allows
```

| Channel class | subscription | enrollment·system must-deliver | enrollment·marketing/operations |
| --- | --- | --- | --- |
| Real-time (IM/SMS/push) | ✅ user's own choice | ✅ multi-channel parallel to guarantee must-deliver | ❌ disabled |
| email | ✅ user's own choice | ✅ fallback parallel | ✅ default channel, low-frequency and unsubscribable |
| application side (webhook→in-app message) | ✅ user's own choice | ✅ | ✅ default channel, low-frequency and unsubscribable |
| pull (RSS) | ✅ subscription type only | ❌ | ❌ |

The per-cell default policy, validation timing, and violation errors are in relation detail design §5. The matrix and filter **are landed** (batch 4: matrix into the Enroll gate + `Filter.Allow` re-check before sending; the expandRef pipeline hookup follows the batch 11 trigger surface — the current /notify has no category dimension to filter).

## 7. Intensity × Urgency and Delivery Mode

Channels are not flat. The Intensity ladder L0–L5 (RSS pull → email → in-app message → IM → SMS → phone) pairs with four message Urgency levels (routine/normal/urgent/critical); Urgency decides the allowed intensity range; the matching rule is its own policy component, not scattered inside providers.

User Preference **can go down but not up**: you can turn channels off downward within the urgency range, but not turn them on upward; critical level has a guaranteed channel (specified by config, made explicit in the preference center). Three delivery modes are configurable per category/relation type/single trigger, the same entry point for policy components but different executors:

| Mode | Behavior | Default |
| --- | --- | --- |
| escalation chain | on no answer, escalate intensity level by level; stop as soon as answered | urgent/critical |
| fixed single channel | only go through one specified channel | routine/normal |
| multi-channel parallel | send all at once to guarantee must-deliver | forced for system must-deliver types |

The ladder diagram, matching matrix, escalation-chain parameters, and the ordering of dedup folding are in relation detail design §6. **Landed** (batch 9, `core/audience.DeliveryPolicy` policy component + `BuildPlan`/`RunPlan` executors; trigger-surface wiring follows batch 11).

## 8. Dedup and Frequency Control

Division of labor with aggregation: **Digest = time-window aggregation of different messages; dedup = folding of identical messages**. Dedup happens before the delivery pipeline and before the escalation chain — 10 duplicate alerts are folded into 1 first, then answered and escalated.

Three layers: event idempotency (`event_id` duplicate trigger delivers only once), content folding (same category and same content folded within a window with a "×10" count, the original event list retained for audit), state-machine dedup (state-type alerts fire by flip message, down→recover→down again before re-sending). Three frequency-control tiers are configurable (once / throttle / always, per category/relation type), and user Preference **can narrow but not widen**.

Existing capabilities: content-fingerprint dedup and request idempotency key are already live; event idempotency/folding/state machine/three-tier frequency control **are landed** (batch 10, `core/dedup.Gate` gate sequence + `config.DedupConfig` category tiers and window override table). The Narrow cell for user-preference narrowing is ready, and the endpoint is wired with the batch 11 preference surface. Details are in relation detail design §11.

## 9. Preferences and Entry Points

The subscriber self-serves in the preference center, checking "what category → what channel → what frequency"; the default policy is given with the category (system must-receive, marketing defaults to weekly digest, alerts can be down-rated but not silenced). The preference-modification entry point is not unique — bot commands, official-account follow events, in-app checking, and the preference center are all **source adapters**; Herald is the **authoritative registry** of relations, all entry points converge into relation changes in the registry, unfollow must backflow (stopping all delivery), and external platform state is periodically reconciled. The default-policy table and adapter rules are in relation detail design §7-8. The preference model **is landed** (batch 3, `core/audience`'s Frequency/Preference/DefaultPolicy and read/write API); source adapters **are landed** (batch 8, `SourceAdapter` + the three entry-point endpoints for bot/official-account/in-app + external-state reconciliation).

## 10. Reach Forms: Digest and RSS

**Digest** (batch 6): by audience × category × time window (daily/weekly), collect scattered events into one summary and go through the existing delivery pipeline (the summary also enjoys retry/audit); real-time types are exempt and delivered directly, system must-deliver never aggregates. **Landed** (`core/digest`: strict late-arrival semantics for window flip, Resolve exemption ruling, `digest:<category>` template reskinning, FlipLoop timer + optional redis lease leader election — runs standalone without redis_addr).

**RSS pull-mode** (batch 7): the natural preference channel with zero binding and zero push. Public feeds, one per category (`/feeds/<category>.xml`, containing only public content with no audience reference); private feeds carry the audience's `rss_token` (`/feeds/private/<rss_token>.xml`, batch 2's reset-as-old-address-invalidation semantics take effect directly), personal content such as billing goes here; frequency = reader polling, Herald does not push, naturally no harassment; available only to subscription-type relations — enrolled content does not enter the feed. **Landed** (`core/feeds` pull-mode projection storage + RSS 2.0 rendering, `/feeds/**` endpoints, the delivery pipeline projects in place for rss-class channels without producing provider tasks; visibility is decided at read time — token ownership + relation registry × channel matrix re-check, unfollow makes it disappear from the next pull onward; multi-address disaster recovery is assigned to the reader side per boundary audit §4).

## 11. Delivery and Audit

The shape the delivery side has already landed: task state enumeration (delivered / failed / dead, retrying), six-class error vocabulary (temporary / permanent / rate_limited / authentication / invalid_request / timeout) deciding retry and terminal state, 429 with Retry-After backoff, retryable failures asynchronously re-enqueued (`next_retry_at` retained on the queue side, workers do not sleep for backoff). The audit side has been completed per relation detail design §12: relation type and Source snapshotted into the notification→task→delivery-log full chain, relation/ContactSurface change streams (`core/audit`, six event types: subscribe/enroll/unsubscribe and bind/rebind/invalidate, read split by type and audience), dedup-folding detail (`delivery.deduped`, with content fingerprint) — audit answers "which entry point this subscription came from, which relation this delivery was based on, why it failed, who changed what and when". **Audit completion is landed** (batch 5); the send-side relation context is filled with the batch 8/11 trigger surface.

## 12. Integrator API

All model capabilities come with a programmable config API: each integrated application has one app namespace (categories/templates/policies isolated from each other), app tokens are tiered by permission (config / trigger / query); the three endpoint groups — config, trigger, query — share the same vocabulary as policy components such as escalation chain, dedup, and channel matrix; the Go SDK is first out, and webhook callbacks bring back delivery results and unsubscribe events. The contract draft is in relation detail design §13, and the integration guide lands with batch 11 at `docs/guide/integration.md`. **Landed** (batch 11 increments one through eight; 2026-10-08 stage ③ integrator onboarding validation wrapped up — the event-ingestion adaptation surface and callback returning `event_id` are through).

## 13. Landing Status (honest accounting)

| Capability | Status | Evidence |
| --- | --- | --- |
| notification pipeline (route/queue/worker/retry/error classification/request idempotency) | ✅ landed | [Status Audit](./design-audience-audit.en.md) Phase 0-9 |
| relation model and audience registry | ✅ landed | batch 1 (`3fc1a82`), `core/audience.Registry` |
| ContactSurface and binding (token/rebind/RSS token) | ✅ landed | batch 2 (`19cf16c`), `core/audience.SurfaceRegistry` |
| preference center (category×channel×frequency, default-policy table) | ✅ landed | batch 3 (`4df837a`), `core/audience.PreferenceRegistry` |
| channel×relation matrix and delivery filter (four-family classification, Enroll gate enforcement, pre-send re-check) | ✅ landed | batch 4 (`4b18b4a`), `core/audience.Filter`; expandRef hookup is through with batch 11 |
| delivery audit completion (change stream, dedup-folding detail, relation snapshot into tasks and logs) | ✅ landed | batch 5 (`de05e94`), `core/audit` + `SetRecorder`/`SetFoldAudit` |
| Digest time-window aggregation (window flip, exemption ruling, summary template, flip loop and optional lease) | ✅ landed | batch 6 (`3204e90`), `core/digest` + `SetDigest`/`FlushDigest` |
| RSS pull-mode channel (public/private feed, read-time visibility, pull-mode projection) | ✅ landed | batch 7, `core/feeds` + `/feeds/**` endpoints + `SetFeeds` projection; token/relation registry wired in shared with batch 8 |
| source adapters (bot /start /stop, official-account follow events, in-app checking, unfollow backflow full-stop, external-state reconciliation) | ✅ landed | batch 8, `core/audience.SourceAdapter`/`Reconciler` + three entry-point endpoints in `api` + `telegram`/`wechatmp` probe |
| dedup and frequency control (event idempotency, folding ledger, state machine, three-tier frequency control with category default/override table) | ✅ landed | batch 10 (`3c6c395`+`ca6ee39`), `core/dedup.Gate` + `config.DedupConfig` override table |
| intensity and mode (intensity ladder, urgency range, phone double-consent gate, three-mode executors) | ✅ landed | batch 9 (`0c20459`), `core/audience.DeliveryPolicy`/`BuildPlan`/`RunPlan`; trigger-surface wiring is through with batch 11 |
| integrator API and Go SDK | ✅ landed | batch 11 increments one through eight, `api/handler_apps.go` (config/trigger/query three faces + events adaptation surface + callback) + `apps-sdk/go` + `docs/guide/integration.md`; stage ③ integrator onboarding validation (`0096be4`) closed the loop |

The config items and endpoints of in-flight capabilities do not yet exist — do not configure production based on this document; as a batch lands, this status table and the corresponding detail design are updated together. (As of 2026-10-08, §15 batches 1-11 and the three-stage validation have all landed, and this table has no in-flight rows.)

## 14. History

The original plan document of the Phase 0-9 pipeline rework ("Audience Domain Model: Architecture Improvement Plan") has wrapped up; the execution record and deferred-item rulings are archived in [Status Audit](./design-audience-audit.en.md), and the original plan text is preserved by git history. On 2026-10-05 the audience-layer extension (unified subscription and delivery hub) was initiated, and on 2026-10-07 this page was rewritten from the plan document into the current general model; the relation detail design carries the item-by-item design of all new capabilities.
