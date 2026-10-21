# Audience Subscription and Delivery Hub (Relations Detail Design)

Herald's positioning upgrade: from "a notification pipeline that applications push through proactively" to a unified subscription and delivery hub where **the notified party is in charge**. Applications (the integrating business systems) hold only the audience ID; who receives which category, on which channel, at what frequency, is decided by the audience's own relations.

This document is the relations detail design of the audience layer ([audience domain model, general model](./design-audience-model.md)). It covers: ContactSurface and binding, subscription/enrollment relations, the channel × relation matrix, channel intensity × message urgency and delivery modes, the preference center, source adapters, the RSS pull channel, Digest aggregation, dedup and frequency control, delivery audit, and the integrator API. It benchmarks against Novu's subscriber/preference/digest watershed capabilities but **introduces no separate "subscriber" entity** — everything extends the existing audience layer. Terminology follows the contract table in general model §2 as the single authoritative reference; this document does not re-define it.

## 1. Terminology

The terminology contract (audience / recipient / ContactSurface / subscription relation / enrollment relation / Policy / category / channel / Preference / Digest / Urgency / Intensity) is defined in [general model §2](./design-audience-model.md). This document adds two points specific to the relations layer:

An audience is an **identity**; subscriber and recipient are roles an audience plays within a given **relation**. There is no parallel "subscriber" identity entity — this guarantees that the application side integrates against a single set of audience IDs.

Real-time-ness is a channel property and does not intrude into the Intensity ladder: how fast a channel arrives (polled pull → second-level push) correlates with Intensity but is calibrated independently.

## 2. Domain Model: The Audience Layer at the Center

All new capabilities hang off two anchors of the existing audience layer: **ContactSurface** (reachability) and **relation** (willingness/obligation). The domain diagram puts the audience layer at the center:

```text
┌─────────────────────────────────────────────────────────────────────┐
│                 Audience Layer (existing, the sole center)          │
│                                                                     │
│   Audience(named audience) ──contains──> Recipient(named recipient) ──holds──> Endpoint │
│   user:alice ref resolution    identity: shared across apps          type+target │
└────────┬──────────────────────────────────────────┬─────────────────┘
         │ Extension ①: ContactSurface               │ Extension ②: Relation
┌────────▼─────────────────┐          ┌─────────────▼──────────────────┐
│  ContactSurface          │          │  Relation                      │
│  ─────────────────────   │          │  ────────────────────          │
│  type: telegram/email/   │  1:N     │  audience_id × category ×      │
│        wechat_mp/rss/... │◄─────────┤        channel                 │
│  credential              │          │  type: subscription│enrollment │
│  status: active│invalid   │          │  source: Source (bot/official  │
│  bound_via: binding entry│          │        account/preference      │
│  token: private RSS feed │          │        center/app/admin)       │
│  verified_at             │          │  policy: Policy (unsubscribe   │
│                          │          │        right/channel matrix/   │
│                          │          │        frequency cap)          │
└────────┬─────────────────┘          └─────────────┬──────────────────┘
         │                                          │
         │           ┌──────────────────────────────┤
         │           │ Extension ③: Preference   Extension ④: Digest
         │  ┌────────▼─────────┐      ┌─────────────▼─────────┐
         │  │ Preference        │      │ DigestRule             │
         │  │ category×channel× │      │ audience+category+     │
         │  │   frequency       │      │   time window          │
         │  │ (subscription-    │      │ daily/weekly, realtime │
         │  │  relation only)   │      │ exemption              │
         │  └──────────────────┘      └───────────────────────┘
         │
         │ Extension ⑤: pull-based outlet
┌────────▼─────────────────┐
│  RSS Feed (pull channel) │
│  public feed: one per    │
│    category              │
│  private feed: token URL │
│  multi-address failover  │
└──────────────────────────┘
```

Key constraints:

- **The audience ID is the sole cross-system identifier.** Applications store no email, no chat_id, no phone number — only `audience_id`; a ContactSurface change takes effect for all applications at once.
- **The ContactSurface is the sole attachment point for channel credentials** (extended from the existing `Endpoint`: Endpoint expresses "a delivery address", ContactSurface expresses "a reachable channel together with its credential and state"; the recipients table in static config becomes the static seed of contact surfaces).
- **A relation is the sole legal basis for delivery.** At send time the system validates the intersection of "relation allows × ContactSurface bound"; an empty intersection means no delivery.

## 3. ContactSurface and Binding

### 3.1 Binding Flow (bot deep-link one-time token)

Channel credentials are folded into audience registration: bot binding is exposed as Herald's **audience channel registration API**. Taking Telegram as an example:

```text
 User                     App (integrator)         Herald Binding API        TG Bot
  │                          │                        │                     │
  │ 1. click "Bind Telegram" │                        │                     │
  ├─────────────────────────>│ 2.POST /audiences/{id} │                     │
  │                          │   /bindings {channel:  │                     │
  │                          │    telegram}           │                     │
  │                          ├───────────────────────>│ 3. issue one-time   │
  │                          │   <── deep_link + token ┤   token (expires   │
  │                          │                        │    in 15 min,       │
  │                          │                        │    single use)      │
  │ 4. tap deep link to open │                        │                     │
  ├──────────────────────────┼────────────────────────┼────────────────────>│
  │                          │                        │  5./start <token>    │
  │                          │                        │<────────────────────┤
  │                          │                        │ 6. redeem: check    │
  │                          │                        │   unused/unexpired; │
  │                          │                        │   attach chat_id as │
  │                          │                        │   the audience's    │
  │                          │                        │   ContactSurface    │
  │                          │                        │   (status=active)   │
  │                          │  7. binding success    │                     │
  │                          │     notice (both ends) │                     │
  │<─────────────────────────┼────────────────────────┤<────────────────────┤
```

Rules:

- **The token is one-time + expiring** (default 15 minutes); it is invalidated upon redemption, and one token can attach to only one audience.
- **Rebinding requires confirmation from the old channel**: when the audience already has a contact surface of the same type, the new binding enters `pending` and a confirmation message is sent to the old channel; the switch happens only after the old channel confirms (or the silence window times out) — this prevents "one deep link from hijacking someone else's notification channel".
- **The RSS private token is issued automatically upon binding**: each audience gets one `rss_token`, which forms its private feed address (see §9) — zero binding cost.
- The application side only handles audience IDs and token flows, and **never touches channel credentials in plaintext**.

### 3.2 ContactSurface States

| State | Meaning | Delivery behavior |
| --- | --- | --- |
| `pending` | Binding issued but not redeemed / rebind awaiting old-channel confirmation | No delivery |
| `active` | Redeemed and usable | Deliver per relations |
| `invalid` | Declared invalid by the external platform (reconciliation / chat unreachable / unfollow) | No delivery; wait for re-binding |

## 4. Relation Semantics: Active Subscription vs. Passive Enrollment

Within the audience layer, two kinds of relations are distinguished. **Even if they share one table and one shape, semantics, unsubscribe policy, and audit must stay separate.**

| Dimension | subscription | enrollment |
| --- | --- | --- |
| Initiated by | The audience opts in itself | Admin / operations / system |
| Typical scenario | User subscribes to domain-expiry reminders, bills | Group broadcast, operations reach, system notifications |
| Unsubscribe right | Fully autonomous, unsubscribe anytime | Hard floor preserved (see below) |
| Category × channel × frequency | User's choice | Set by the sender; user may set caps |
| Audit | Recorded separately per subscription | Recorded separately per enrollment |
| Interface | `Subscribe()` | `Enroll()` |

**Hard floor (enrollment may not cross it)**:

1. Marketing/operations messages **must be unsubscribable** — after unsubscribing, no channel may deliver them again.
2. Must-deliver system messages are explicitly labeled ("system notification, cannot be unsubscribed") and are limited to statutory/contractual/security-obligation content; smuggling marketing in is not allowed.
3. Channel and frequency caps remain user-manageable: even if the user cannot exit the relation, they can cut channels to the minimum and frequency to the lowest.

**Data model**: relation rows carry `type` (subscription/enrollment) + `source` (Source) + `policy` (Policy). Queries and audit are always split by type — no "universal relation".

**Explicit interfaces**: subscription and enrollment are two named interfaces (`Subscribe` / `Enroll`), not a universal `AddAudience`. Policies hang off the type: unsubscribe right and the available channel matrix are valued per type.

### 4.1 Active Subscription Flow

```text
 User                Entry Adapter            Audience Registry       Delivery Pipeline
  │                     │                        │                    │
  │ picks category×     │                        │                    │
  │   channel×frequency │                        │                    │
  ├────────────────────>│ Subscribe()            │                    │
  │                     ├───────────────────────>│ write subscription │
  │                     │                        │ +Source+Policy     │
  │                     │                        │                    │
  │                     │        event arrives: category match ──────>│
  │                     │                        │ validate: relation │
  │                     │                        │   × ContactSurface │
  │                     │                        │   intersection     │
  │                     │                        ├───────────────────>│ deliver at the
  │                     │                        │                    │ preferred freq:
  │<── effective now, unsubscribe anytime ───────┤                    │ realtime→direct
  │                     │                        │                    │ daily/weekly→digest
```

### 4.2 Passive Enrollment Flow

```text
 Admin/Operations/System    Audience Registry               Delivery Pipeline
  │                          │                             │
  │ enroll: audience group×  │                             │
  │   category×channel      │                             │
  ├─────────────────────────>│ Enroll()                    │
  │                          │ write enrollment            │
  │                          │ +Source+Policy (hard floor  │
  │                          │    guaranteed)              │
  │                          │                             │
  │              event arrives ────┤                       │
  │                          │ validate: type channel matrix│
  │                          │   × ContactSurface          │
  │                          │   intersection             │
  │                          ├────────────────────────────>│ must-deliver system:
  │                          │                             │ multi-channel parallel
  │                          │ unsubscribe request         │ marketing/ops:
  │                          │   (marketing) ──> relation  │ low-intensity channels
  │                          │   terminated + audit record │ only
```

**The intersection of the two flows is the ContactSurface**: no matter where a relation comes from, whether and where anything can be delivered is decided by the intersection of "which contact surfaces this audience has bound × which channels this relation type allows".

## 5. Channel × Relation Matrix

Available channels follow the relation type — there is no single set. At send time the system validates the "relation allows × audience bound" intersection:

| Channel class (examples) | subscription | Enrollment · must-deliver system | Enrollment · marketing/operations |
| --- | --- | --- | --- |
| Instant (Telegram/WeCom/DingTalk/Feishu/SMS/push) | ✅ user's choice, on by default | ✅ **parallel multi-channel to guarantee delivery** (domain expiry etc.) | ❌ **forbidden** — no instant-channel bombing |
| Email (SMTP) | ✅ user's choice, on by default | ✅ fallback in parallel | ✅ **default channel**, low frequency, unsubscribable |
| Application side (webhook → in-app message) | ✅ user's choice | ✅ | ✅ default channel, low frequency, unsubscribable |
| Pull (RSS, see §9) | ✅ **subscription-type only** | ❌ enrollment cannot be pulled | ❌ |

Matrix rules:

- **The default policy of each cell** is written inside the cell; the Policy is valued per relation type, validated before sending, and violating combinations raise errors at config/call time.
- System "must-deliver" only means **best-effort parallel multi-channel**; it does not promise arrival (arrival depends on external channels); the audit records the result of every attempt truthfully.
- RSS is pure pull: only categories a user actively subscribed to (willing to pull) make sense as feeds; enrolled content cannot be "pulled up".

## 6. Channel Intensity and Message Urgency

Channels are not flat: interrupting a person and lying quietly waiting to be pulled are two entirely different behaviors. The channel intensity model gives every channel two calibrated values and every message one urgency; the matching rules stand alone as a policy component.

### 6.1 The Intensity Ladder

```text
 Intensity (low ──────────────────────────────> high)   Real-time-ness
 ┌──────┬──────┬────────┬──────┬──────┬──────┐
 │ RSS  │ Mail │ In-app │ IM   │ SMS  │ Phone│
 │ pull │ async│ passive│ push │ push │ hard │
 │      │      │ read   │      │      │ inter│
 ├──────┼──────┼────────┼──────┼──────┼──────┤
 │  L0  │  L1  │  L2    │  L3  │  L4  │  L5  │
 └──────┴──────┴────────┴──────┴──────┴──────┘
   minute-  hour-   next      sec-   sec-   instant
   level    level   session   ond-   ond-   (ring)
  (poll)  (reach)   open     level  level
```

- **Intensity** L0–L5 is a quantifiable grading; concrete providers within the same channel class inherit the class's level (Telegram and DingTalk are both L3).
- **Real-time-ness** correlates with Intensity but is independent: RSS relies on the reader polling (minute-level), email arrives at hour-level, IM/SMS push at second-level, the phone rings instantly.
- **Delivery direction** is the channel's third property: push channels are delivered proactively by Herald (email/in-app/IM/SMS/phone); pull channels are polled by the reader (RSS). Direction correlates with both Intensity and real-time-ness but is calibrated independently; "delivery mode" (§6.3) is another matter — mode answers the orchestration question, direction answers channel behavior.

### 6.2 Urgency × Intensity Matching Matrix

Urgency decides the **allowed intensity band** (the band's ceiling is "how much you may interrupt at most"):

| Message urgency | Allowed intensity band | Notes |
| --- | --- | --- |
| routine | L0–L1 (RSS/email) | Quiet channels only; marketing/announcements default here |
| normal | L0–L2 (+in-app) | Everyday business notifications |
| urgent | L0–L4 (+IM/SMS) | Alerts default here |
| critical | L0–L5 (+phone) | **Phone disabled by default**; requires explicit enabling (config + audience consent, both) |
| Delivery mode (§6.3) | escalation chain / fixed single channel / parallel multi-channel | critical/urgent default to escalation chain; routine/normal default to fixed single channel; must-deliver forces parallel — all overridable per category / relation type / per call |

Matching performs a three-way intersection check at send time:

```text
 delivery targets = ContactSurfaces bound by the audience
                  ∩ channels allowed by the relation type (§5 relation matrix)
                  ∩ intensity band allowed by the urgency (this section)
```

An empty intersection means no delivery, and it is audited (e.g. the audience bound only RSS — an urgent message still just goes into the feed; a pull channel is not a silent failure, the audit states the reason).

### 6.3 Delivery Mode (three options)

The escalation chain is not the only mode. Three delivery modes, configured with the same semantics in the API (§13) and the admin console, specifiable per category / relation type / per call; inside the policy component the three modes share **one entry point with different executors**:

| Mode | Behavior | Typical scenario |
| --- | --- | --- |
| escalation chain | On no answer, step up intensity level by level (steps/interval configurable) | urgent/critical alerts |
| fixed single channel | No escalation; only the one designated channel (e.g. "bills go by email only") | routine/normal notifications |
| parallel multi-channel | Send all at once, parallel channels to guarantee delivery | must-deliver system category (domain expiry) |

Default urgency mapping: critical/urgent → escalation chain, routine/normal → fixed single channel, must-deliver forces parallel; explicit configuration overrides the default.

#### Escalation Chain Details (Mode One)

Messages of urgent and above automatically step up in intensity when unanswered:

```text
 IM(L3) sent
   │  unanswered, wait ack_timeout (default 15 min)
   ▼
 SMS(L4) sent
   │  unanswered, wait ack_timeout
   ▼
 Phone(L5) dialed       ← only for critical with phone explicitly enabled
   │
   ├─ any step answered (ack) ──> stop escalation, record answered step and latency
   └─ escalation steps exhausted ────> stop, audit records "escalation exhausted, unanswered"
```

- **Answer stops the chain**: reuses the existing ack mechanism (AlertID threads through delivery and callbacks) — once any step is answered, all pending actions further along the chain are cancelled.
- **Steps and interval configurable**: each escalation chain is configured `{channel, ack_timeout, max_steps}`; the default chain is `IM→SMS`; a phone step exists only for critical with explicit enabling.
- Orthogonal to the existing rule engine escalation (escalating to more audiences): rule escalation answers "**whom to call**", intensity escalation answers "**how to call louder**"; the two compose (for the same audience, widen the crowd first, then step up intensity).
- **Folding precedes escalation**: dedup folding (§11) happens before the escalation chain — 10 duplicate alerts are first folded into 1, and only then are answers and escalation counted; repeated triggers will not fire the phone.

> **Implementation note (decided 2026-10-11: advancing the later segments of the dispatch-surface escalation chain — shelved)**: intensity escalation on the rule-engine path has landed in full (`core/escalation.Manager`: `Schedule` arms the ack_timeout timer, ack consultation, firing on timeout, JSON persistence, `Restore` on restart; `POST /api/v1/alerts/{id}/ack` — an answer stops the chain, sharing the alert identity space with the ack store). The app namespace's dispatch surface **delivers only the first segment** — the trigger surface returns the full chain in its `plan` field for display, and `Stages[1:]` never advances (`core/service/dispatch.go` pass two takes `Stages[0]`; no second consumer anywhere in the repo). Reasons the later-segment advance is shelved:
>
> 1. **The mechanism does not fit**: `escalation.Pending{RuleID, AlertID, To []string}` has no audience dimension, and firing performs **broadcast delivery with no audience filtering** (`DeliverEscalation` enqueues directly); the dispatch surface's later segments are **per-audience** (each audience's kept targets that land on that segment's channel), and advancing must re-judge against the relation × matrix state **as of that moment** — within the window an audience may already have unfollowed; reuse would mean either delivering in violation or rewriting the whole mechanism.
> 2. **The ack source would require a new public endpoint**: the sole ack entry `POST /api/v1/alerts/{id}/ack` sits behind the admin console's `withAuth` (an app token calling it gets 401), and the alert id space is shared with the rule path — misuse would cancel the rule path's pending escalations. An app-surface ack can only be a new endpoint, and its identity can only be the dispatch `event_id` (the integrator's own primary key — what the `delivery_result` callback carries back).
> 3. **The capability already exists on the other path**: the organization-level gateway scenario is covered by the rule-path escalation chain; the app namespace's "step up intensity" can be driven by the integrator itself (re-trigger with higher urgency; §11's state machine and dedup keep it controllable).
>
> **Unshelving trigger conditions**: an integrator needs the organization side to host timeout escalation for the app namespace, and accepts ack identity = dispatch `event_id`. Proposed shape (not implemented): `POST /api/v1/apps/{app}/events/{event_id}/ack` (query scope) → arm record (audience × segment × target + relation snapshot) → re-judge Filter before advancing → on timeout deliver that segment's targets → persistence/recovery → add an `escalated` event on the callback surface. Until then, apps.md's plan row reads "first segment delivered first; later segments do not advance automatically".

### 6.4 User Preference Overlay: Can Lower, Never Raise

User preference can only **step down** within the band the urgency allows, never up:

- At routine level the user may keep only RSS — convenient and compliant.
- At urgent level the user may turn SMS off and keep IM — a step down, compliant.
- **Critical-level floor**: even if the user cuts channels down to email only, a critical message still gets one supplementary delivery over the "fallback channel" (designated in config, SMS by default) — the user may switch off reminder methods, but a critical event must get one strong touch; the fallback channel is stated plainly in the preference center and takes effect only after the audience is aware of it.

Code mapping: channels carry an intensity enum (`Intensity`), messages an urgency enum (`Urgency`), the category→urgency default mapping goes into config (alerts→urgent, marketing→routine, …), and a single event may override it explicitly; the matching rules stand alone as a policy component (see rollout batch 9), not scattered across providers.

## 7. Preference Center

Subscribers self-serve in the preference center: "**which category → which channel → which frequency**":

| Dimension | Values |
| --- | --- |
| Category | alerts / bills / domain notices / announcements / system / marketing |
| Channel | the contact surfaces this audience has bound (user-picked subset) |
| Frequency | realtime / daily digest / weekly digest / none |

**Default policies** (behavior for a brand-new audience with zero action):

| Category | Default frequency | Default channels | Notes |
| --- | --- | --- | --- |
| system | realtime, **must receive** | all bound surfaces on | must-deliver system category, explicitly non-unsubscribable |
| alerts | realtime | all bound surfaces on | may step down to digest, may not be silenced (hard floor) |
| domains/bills/announcements | realtime | first bound surface | user may change |
| marketing | **weekly digest** (low frequency by default) | email/in-app | user may change / unsubscribe |

The entry points for changing preferences are not unique (see §8), but the **authoritative registry is unique**: all entry points converge into relation changes in the audience registry, effective immediately.

## 8. Subscription Entry Points: Source Adapters

**The subscription action does not necessarily happen on Herald's side** — official-account follow/unfollow, TG bot `/start`, in-app toggles are all platform-side actions. Herald is the **authoritative registry** of subscription relations; each entry point is a **source adapter** that syncs external actions into audience relation changes:

```text
 Official account follow/unfollow ─┐                       ┌── Subscribe() / Enroll()
 TG bot /start /stop ──────────────┤  Source Adapter         │        │
 Herald preference center ─────────┼───────────────────────>│  Audience registry
 In-app toggle (integrator) ───────┘  (SourceAdapter)       │  (sole authority)
                                  action normalization:      │        │
                          follow   = register ContactSurface + default subscription group
                          unfollow = invalidate ContactSurface + unsubscribe (full stop)
                          check    = preference change
                                                            ▼
                                    audit: every relation change records its "Source"
```

Rules:

1. **Unfollow must backflow**: official-account unfollow / bot `/stop` sync as unsubscribe events and **stop all delivery** (continuing to push = harassment + policy violation). The contact surface flips to `invalid`, subscription relations terminate, audit leaves a trail.
2. **Audit records the entry point**: every relation change records the source adapter (`source: wechat_mp | bot | preference_center | app:<name> | admin`); the audit can answer "where did this subscription come from, who operated, when".
3. **Consistency caliber**: Herald trusts only the relations it has registered itself; external platform state is **reconciled periodically** — the reconciliation targets are the targets registered in the registry (probe each official account's `subscribe` flag, probe TG chat validity); when a probe proves invalidation, mark `invalid` + stop delivery, and the reconciliation result goes into audit. It does not compare against the platform's full set (unbound openids in the follower list have no audience identity and are not Herald's business).

**Landed** (batch 8):

- **Action normalization**: `core/audience.SourceAdapter` — `Follow` (`Activate` the contact surface + default subscription groups land as `subscription`, per-group `Failed` recorded without rolling back over a single group's rejection), `Unfollow` (contact surface `invalid` + full-stop unsubscribe by channel: `RelationsByType` then `TerminateFor` each, enrollment relations and other channels unaffected), `Toggle` (opt in/out; opt-out goes through `TerminateFor`, hitting must-deliver returns `ErrMustDeliver`). Audit is recorded uniformly via the registry's `SetRecorder` (`cmd/heraldd` attaches an `audit.Store` when either feeds or sources is enabled); relation events' `Source` is the entry source (`bot | wechat_mp | preference_center`), and `Detail` carries "via which action".
- **Three entry endpoints** (`api/handler_sources.go`):
  - `POST /api/v1/callbacks/bot` — Telegram webhook (`X-Telegram-Bot-Api-Secret-Token` constant-time comparison): `/start <token>` goes through batch 2's one-time `RedeemBinding` (default groups land only on activation; **a rebind in pending does not land** — it waits for old-channel confirmation); `/stop` reverse-looks-up the audience via `FindByTarget` then full-stops. All legal updates return 200 (Telegram retries non-2xx, and retrying an expired token or a duplicate unfollow changes nothing); wrong secret is an intruder (403), broken JSON is a caller bug (400).
  - `POST /api/v1/callbacks/wechat-mp` — official account server callback: sha1(timestamp,nonce,token) signature (constant-time comparison) as the gate; GET returns the console's `echostr` verification challenge; `subscribe`/`unsubscribe` converge into `Follow`/`Unfollow`. An openid never bound is treated as a no-op (new followers follow first and bind later — normal order; at that point there is no audience identity to converge).
  - `POST|DELETE /api/v1/audiences/{id}/subscriptions` — in-app toggle (integrator side), API token gate at the same level as the rest of the operational surface; field validation (id pattern, category/channel 1–64 chars) 422, opting out of a must-deliver 409 (§4 hard floor), opting out of a nonexistent slot 404.
- **External state reconciliation** (rule 3): `core/audience.Reconciler` synchronous single sweep `RunOnce` — probes **only active contact surfaces registered in the registry** (the reconciliation target is its own registrations, not the platform's full set; official accounts probed one by one via the `subscribe` flag of `user/info`, not a full follower-list diff); a probe error means `Skipped` — **reconciliation never guesses**; proven invalidation means `InvalidateFor` + that channel's subscriptions `TerminateFor` (actor `reconcile`) with audit. `providers/builtin/telegram` and `providers/builtin/wechatmp` each provide a `SurfaceProbe` (for TG, an unknown chat returns 400/403 with `ok:false`, recorded as "definitely unknown"; all other non-2xx are treated as probe-inconclusive). `cmd/heraldd` starts a `reconcileLoop` in the same pattern as the digest window-flip loop: one sweep per hour by default, the `sources.reconcile.enabled` switch, and a redis lease (reusing the digest connection config, lock key `herald:sources:reconcile:leader`) so multiple instances sweep only once per round.
- **Config**: a `sources:` block (the `enabled` master switch + `bot.secret` / `wechat_mp.token` to close individual entry points — **the credential is the switch**; an empty secret/token makes the endpoint 404 even with `enabled: true`); probes are built from the existing `providers:` block by `type: telegram|wechatmp`; a provider with `enabled: false` is not probed, and broken config (missing token) refuses to start.

## 9. RSS Pull Channel

RSS is a **pull channel** (delivery direction pull, see §6.1): not a provider implementation but a pull-based projection of delivery records. The zero-binding, zero-push natural preference channel:

| Property | Design |
| --- | --- |
| Public feed | One per category: `/feeds/alerts.xml`, `/feeds/bills.xml`, `/feeds/domains.xml`, `/feeds/notices.xml` — only public content of that category |
| Private feed | `/feeds/private/<rss_token>.xml` — a personal feed carrying the token; personal content such as bills goes here; the token can be reset (reset invalidates the old address) |
| Subscribe = pick categories | After the user checks categories in the preference center, the corresponding feed content is generated per that audience's visibility |
| Frequency = polling | However often the reader pulls is however often it receives — Herald pushes nothing, naturally harassment-free |
| Multi-address failover | Feed addresses come with backups (primary domain + backup domain both pullable); on disconnect the reader switches automatically |
| Scope | **Subscription-type relations only**: enrolled content never enters feeds |

A feed item is a pull-based projection of a delivery record: it shares the same "relation allows × contact surface bound" validation as push delivery — someone without a subscription relation cannot pull the corresponding category, and the private feed validates token ownership.

**Landed** (batch 7): `core/feeds` (feed storage and RSS 2.0 rendering; items are projections of delivery records; public feeds contain only public content with no audience reference) + the `api` `/feeds/{category}.xml` and `/feeds/private/{rss_token}.xml` endpoints + in-place projection for rss-class channels in the delivery pipeline (rss channels produce no provider tasks). Private-feed visibility is decided **at read time** item by item: token ownership resolves via `SurfaceRegistry.RSSAudience` (batch 2's reset-invalidates-old-address semantics take effect directly), and category admission reuses `Registry.Lookup(audience, category, "rss")` + a channel-matrix recheck — unsubscribing removes the content from the very next pull; the projection side keeps no books at all. Multi-address failover is a reader-side capability (the RSS spec allows multiple sources on one link); Herald neither codifies it nor fakes it with double writes. The token/relation registry the private feed depends on is wired in `cmd/heraldd`, and its data is filled in by the trigger surface in batches 8/11 — until then the private feed 404s on unknown tokens, and public feeds carry only public content.

## 10. Digest Aggregator

Gather scattered events under low-frequency preferences into one digest:

```text
 Event stream (queue side)   Aggregator                  Delivery
  ├─alert t1 ─┐
  ├─alert t2 ──┤   collect: audience+category+time window
  ├─bill t3 ─┤   window fires ──> generate digest ──> through the normal delivery pipeline
  ├─domain t4 ─┘   (daily 09:00 / Mondays 09:00)   "Today: 5 alerts: …"
  └─ ...
      │
      └─ realtime exemption: events whose category or preference is marked realtime
         bypass the window and go out directly
```

Rules:

- **Aggregation key**: audience × category × time window (daily/weekly). When the window fires, one digest event is generated and goes through the existing queue→worker→provider pipeline (the digest itself is a delivery too, enjoying the full retry/audit chain).
- **Exemption**: alert-level (realtime preference) bypasses the window and goes out directly; the must-deliver system category is never aggregated.
- **Digest rendering**: reuses the template system (digest templates defined per category), the "Today: 5 alerts: …" shape, with per-item jump links where available.
- **Timers**: the window flip is driven by in-process timers in heraldd (an extension of the existing single-process timer pattern); under multi-instance deployment a redis lock elects the leader to avoid duplicate sends.
- Timezone and send time for daily/weekly come from instance config, default `09:00 Asia/Shanghai`.

## 11. Dedup and Frequency Control

This section governs "duplication and frequency", distinct from aggregation (§10) as four different gates: **idempotency** handles duplicate submissions of the same event; **folding/state machine** handles duplicate delivery of equivalent content; **frequency control** handles caps within a time window; **digest** handles merging many different messages into one summary (§10). The four gates are ordered: idempotency → folding/state → frequency control → (bypass) aggregation → delivery. Dedup happens before the delivery pipeline and before the escalation chain:

```text
 Trigger stream            Dedup layer                    Delivery pipeline
  ├─ duplicate event_id ────────> idempotent drop (audited, not delivered)
  ├─ same key, same content, ───> fold: count +1, keep original events
  │    within window
  ├─ stateful and state        ──> inhibit: record state trajectory
  │    not flipped
  └─ new key / flipped /       ──> allow ─────────────────────> delivery mode
     outside window                                       execution (§6.3)
```

### 11.1 Three-Layer Dedup

1. **Event idempotency**: triggers carry an `event_id`/dedup key; duplicate triggers deliver only once — covers retry storms, upstream duplicate reports.
2. **Content folding**: same category, same content within a time window folds into one message with a count — "node XX down (×10)"; the window is configurable per category.
3. **State-machine dedup**: stateful alerts are sent on **flips** only — node down → recovered → down again sends again; no repeated "down" in between:

```text
        down                 ok                   down
   ─────────────────>  ─────────────────>  ─────────────────>
   first: send "down"     send "recovered"   send "down" again
   repeated down: inhibit + count
```

Folded artifacts **keep the original event list**: the audit can expand "which 10 events are inside this ×10".

### 11.2 Three Tiers of Frequency Control

The dedup frequency-control policy has three configurable tiers, configured per category / relation type / Policy, same caliber in API and admin console:

| Tier | Behavior | Category default recommendation |
| --- | --- | --- |
| once | Same dedup key sends only once (lifetime / per session) | system (one-time events) |
| throttle | Same key sends only once per N time; N configurable per category (e.g. "node alerts one per 30 minutes") | alerts (window throttle) |
| always | No throttling, every message sends | critical audit class |

User preference **can narrow, never widen**: throttling may only go lower, never higher (the user can change "one per 30 minutes" to "one per 2 hours", not to "send every one").

## 12. Delivery Audit

The audit surface filled in along the way (extending the existing logstore single-row state model):

| Record | Source |
| --- | --- |
| Status / retry count / failure reason of every delivery | Existing TaskLog (status/retry/error) |
| **Relation type** (subscription/enrollment) and **entry source** (source adapter) | Snapshotted from the registry into the task at delivery-plan time |
| Relation change events (subscribe/unsubscribe/enroll/rebind/reconciliation fix) | New audit stream, recorded separately per type |
| ContactSurface changes (bind/invalidate/reset) | Same as above |
| Dedup/folding detail (original event list expandable) | Dedup layer (§11): idempotent drops, fold counts, state-inhibition trajectories |

The audit answers four kinds of questions: which entry point this subscription came from, which relation this delivery was based on, why it failed, and who changed what when.

## 13. Integrator API

Every model capability ships with a **programmable configuration API**: integrators can land everything fully automatically without entering the admin UI.

### 13.1 Application Access Surface

- Each integrating application gets an **app/ namespace**: categories, templates, and policies are isolated from each other — app A's "alerts" category and app B's same-named category are invisible to each other.
- **app token auth with tiered permissions**: config / trigger / query; one app may hold multiple tokens each with its own permission set.

### 13.2 Configuration API

| Capability | Endpoint shape | Notes |
| --- | --- | --- |
| Category registration | `POST /api/v1/apps/{app}/categories` | Carries default urgency (the source of the §6.2 mapping) |
| Template registration | `POST /api/v1/apps/{app}/templates` | Variable declarations + multilingual locales + channel binding |
| Channel intensity matching | `PUT /api/v1/apps/{app}/policies/intensity` | §6.2's application-level override of intensity (decided 2026-10-07: the §5 channel × relation matrix is a safety floor — **no** application-level override; the originally planned `channel-matrix` endpoint never lands — relaxing the matrix means relaxing invariants like "must-deliver refuses RSS / marketing limited to email+app", beyond a namespace policy's authority) |
| Delivery mode | `PUT /api/v1/apps/{app}/policies/delivery-mode` | Per-category / per-relation-type defaults of the three modes |
| Escalation chain parameters | `PUT /api/v1/apps/{app}/policies/escalation` | Steps/timeouts/counts (§6.3) |
| Dedup and frequency control | `PUT /api/v1/apps/{app}/policies/dedup` | Three frequency-control tiers per category (§11.2) |
| Audiences and contact surfaces | `POST /api/v1/audiences`, `POST /api/v1/audiences/{id}/bindings`, unsubscribe/preference endpoints | Every action of §3 binding, §4 Subscribe/Enroll, and §7 preferences has a matching endpoint |

### 13.3 Trigger API

```text
POST /api/v1/apps/{app}/dispatch
{
  "category": "alerts",
  "urgency": "urgent",              // omitted → category default
  "relation_type": "enrollment",    // enrollment-type triggers must declare explicitly
  "audiences": ["user:alice", "group:oncall"],
  "dedup_key": "node-17-down",      // dedup key (§11)
  "event_id": "evt-20261005-001",   // event idempotency (§11.1)
  "template": "node_down",
  "params": { "node": "node-17" }
}
```

The server matches channels by policy (relation matrix × urgency intensity band × contact surface binding, a three-way intersection) and returns the acceptance result and a delivery-plan summary. Relation to the existing `/notify`: `/notify` is the anonymous trigger surface (kept for compatibility); the app trigger surface carries the namespace and full policy semantics.

Implementation note (2026-10-08, integrator onboarding verification batch): integrators whose events speak their own vocabulary get an **event ingestion adapter surface** `POST /api/v1/apps/{app}/events` (alert channel design §3: kind/severity/target + the integrator's own event primary key) — kind→registered category, severity→urgency (critical/warning/info → critical/urgent/normal), target→single-element audiences (ref-form `user:5` maps to audience id `user.5`: `:` is reserved for the `user:`/`group:` reference prefixes and absent from the id vocabulary), own primary key into `event_id`; `occurred_at` has no counterpart (herald audit/callbacks use their own timestamps). The **admin-side binding on behalf of users** entry for binding audiences also lands in step: `POST/DELETE /api/v1/audiences/{id}/surfaces` (contact surface + optional default subscription, audit records entry `admin`) — integrators who keep user contact info in their own database rely on it to complete binding.

### 13.4 Query API

- `GET /api/v1/apps/{app}/deliveries?audience=&category=&status=` — delivery status
- `GET /api/v1/apps/{app}/audit?since=` — audit stream (relation changes + delivery attempts + dedup/folding detail)
- `GET /api/v1/audiences/{id}/relations` — audience relations (type/source/Policy/preference)

### 13.5 SDK and Callbacks

- **Go SDK first** (aligned with the worker-sdk precedent, landed as `apps-sdk/go`): an application completes the whole flow "register categories → bind audiences (via the operational-side entry) → trigger → query status" with audience IDs; one token, one Client, permissions decided by the token. Errors split two ways: `*sdk.Error` (server-side refusal, retrying is pointless) and transport errors (retryable).
- **webhook callbacks**: delivery results and **unsubscribe event backflow** go to the application, which syncs its local state. Landed vocabulary: two event types, `delivery_result` (task-settled status) and `unsubscribe`; each event carries an `event_id` for application-side idempotency; the header `X-Herald-Signature: sha256=<hex>` is the HMAC-SHA256 over the exact request-body bytes (the key is the app-configured secret). At-least-once + retries come for free via the same queue pipeline as delivery; callback config hangs off the app record (`PUT/GET/DELETE /api/v1/apps/{app}/callback`; the read endpoint does not echo the secret). Implementation note (2026-10-07): "status of every attempt" converged into **one event per task settlement** — retries are an internal pipeline affair, what the application cares about is the settled outcome; wiring the §6.3 escalation-chain ack source (timeout advancing to the next segment, ack-answer-stops-the-chain runtime advancement) is not part of the callback surface and was split to another batch; that batch was **shelved by decision on 2026-10-11** (reasons and unshelving trigger conditions in the §6.3 implementation note): the dispatch surface keeps first-segment-first, later segments do not advance automatically. Implementation note (2026-10-08, integrator onboarding batch): the `delivery_result` payload carries back **the integrator's own event identity** `delivery.event_id` (the dispatch request's `event_id`, threaded through delivery with the task) — the application matches the callback back to its original event (the integrator's own event primary key goes home this way), which is a different thing from this callback event's `event_id` (the herald-side idempotency id).

### 13.6 Integration Guide

`docs/guide/integration.md` is one document covering the onboarding flow: register app → configure categories → bind audiences → trigger, each step with a curl example; SDK usage and the callback contract live in the same document. This section is the API contract draft; implementation batches sync updates against this section when they land.

## 14. Joints with the Existing Layers

| Existing mechanism | How it joints |
| --- | --- |
| `core/audience` (config-based recipients/audiences) | Becomes the **static seed** of contact surfaces; the runtime registry loads the seed and then accepts API changes. Existing audiences without contact surfaces behave exactly as before (compatible) |
| `expandRef` pipeline (user:/group:/channels resolution) | After delivery targets are resolved, a **relation filter** step is inserted: targets ∩ relation-allowed ∩ contact-surface-bound; an empty intersection skips and audits |
| `core/groups` (group: channel-side audiences) | Group member expansion passes the same relation filter; the group itself unchanged |
| Queue/worker/retry/errclass | Untouched. Digest summaries ride the same pipeline as ordinary notifications |
| logstore single-row state | Incremental extension (§12), wire vocabulary unchanged |
| Rule engine/silence/inhibition | Take effect **before** the relation filter (first decide "whether to send", then "whom to send to") |
| api/server routing | The integrator API (§13) follows the existing routing/middleware patterns, adding app token auth and namespace isolation middleware |
| Dedup state storage | Idempotency keys / folding windows / state machines checked at trigger acceptance and pre-delivery; in-memory for a single process, redis-shared for multiple instances (same shape as the idempotency-table precedent) |
| heraldd timers | Digest window flips and external-platform reconciliation reuse the in-process timer pattern; multi-instance redis lock leader election |

## 15. Rollout Plan (Atomic Batches)

| # | Atomic item | Content |
| --- | --- | --- |
| 1 | Relation model and audience registry | RelationType enum (subscription/enrollment), relation storage (in-memory + type/source/Policy fields), Subscribe/Enroll as separate named interfaces, queries/audit split by type |
| 2 | ContactSurface and binding API | ContactSurface modeling (pending/active/invalid), one-time token issuing and redemption, rebind old-channel confirmation, RSS private token issuing |
| 3 | Preference center | Category × channel × frequency model and validation, default policy table, preference read/write API |
| 4 | Relation filter in the delivery pipeline | Post-expandRef filter step (relation-allowed × contact-surface-bound intersection), channel × relation matrix validation, must-deliver / marketing-unsubscribe Policy landed |
| 5 | Delivery audit completion | Relation type / entry source snapshotted into tasks and TaskLog, relation-change audit stream |
| 6 | Digest aggregator | Aggregation key and window model, timed flip (incl. redis leader election), digest templates and delivery, realtime exemption |
| 7 | RSS pull channel | ✅ Landed: public/private feed generation (`core/feeds` + `/feeds/**` endpoints), category visibility validation (relation + matrix recheck at read time), multi-address failover goes to the reader side per the boundary rule in §4's scope, not codified |
| 8 | Source adapter | ✅ Landed: `SourceAdapter` action normalization (follow/unfollow/check, unfollow backflow full stop, audit records the entry) + three entry endpoints (bot webhook / official account callback / in-app toggle API) + `Reconciler` periodic external-state reconciliation (probe inconclusive → skip, fixes leave audit) + `sources:` config and the heraldd timed loop (redis lock leader election) |
| 9 | Channel intensity and delivery mode | Intensity/Urgency enums, category→urgency default mapping, three-way intersection matching policy component, three mode executors (escalation chain / fixed single channel / parallel), escalation chain wired to ack-answer-stops |
| 10 | Dedup and frequency control | event_id idempotency, content folding (count + original events kept), state-machine dedup, three frequency-control tiers and defaults |
| 11 | Integrator API and Go SDK | app namespace and token permission tiers, config/trigger/query APIs, webhook callbacks (delivery results / unsubscribe backflow), SDK and integration guide |

Batches 1–2 are the foundation (relation model + contact surface binding), 3–5 form the "notified party in charge" closed loop (preference/filter/audit), 6–8 are reach-form increments (digest/RSS/source adapters), 9–11 are policy components and the integration surface (intensity/dedup/integrator API). Three phases advance: document (this piece) → code implementation → integrator onboarding verification — **all three phases complete** (phase ③ concluded 2026-10-08: the receive-event→dispatch→receipt-audit-trail closed loop walked through twice; the onboarding record is kept in the onboarding party's own repo). Each batch is independently acceptable: commit only after the full gate suite is green.
