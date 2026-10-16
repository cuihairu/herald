> English · [中文](./design-notification-groups.md)

# Notification Groups Design: Group and Member Model, Rule Binding, and Routing by Group

> Status: implemented. A notification group is a **named delivery audience**: a set of "channel
> instances + optional recipients", referable as `group:name` anywhere a channel name is accepted,
> and expanded into members at delivery time.
> It is orthogonal to the rule engine ([design-rule-engine.en.md](./design-rule-engine.en.md)): rules decide "whether to send,
> and which reroute path to take", while groups decide "which real channels and recipients the path resolves to".

## 1. The Problem: Audiences Have No Name in the Config

The only granularity herald has today for describing an audience is the **channel instance name**
(provider instance, such as `feishu-oncall`). Two pains:

1. **Duplication**. The "on-call echelon" is two people (channels), feishu-oncall + sms-duty, and today this list has to be copied into
   every rule, every caller's `channels`, and every escalation's `to`. Changing who is on call (changing the channel instance config)
   means editing N places;
2. **Cannot express people**. A channel instance is "a Feishu bot", not "a team of people". Sending the same SMS channel to
   two numbers, Zhang and Li, today means either creating two channel instances or relying on notification-level `recipients`,
   and traffic routed by rules has nowhere at all to carry recipients.

Groups give audiences a **stable name**: behind `group:ops-oncall` is a member list; changing who is on call only changes the group,
and all rules, callers, and escalation chains that reference it follow automatically.

## 2. Group and Member Model

```go
// core/groups
type Member struct {
    Channel    string   `json:"channel"`              // channel instance name (provider name) or channels block name (expanded by block at delivery time)
    Recipients []string `json:"recipients,omitempty"` // optional recipients pinned for this channel
}

type Group struct {
    ID          string   `json:"id"`                  // group name, such as ops-oncall
    Description string   `json:"description,omitempty"`
    Members     []Member `json:"members"`
}
```

The design trade-offs, one by one:

**A member = channel + optional recipients, not a "user"**. The alternative was to introduce a user entity (users bound to multiple contact methods, with groups holding users). Rejected: a user-to-contact-method mapping is an identity system, whereas herald is a delivery system, and an extra layer of entity brings another whole set of CRUD/audit/permissions, while delivery only needs "who this message goes to"; the `Recipients` field already covers every scenario of "the same channel instance sending to specific people" (SMS numbers, Feishu chat_id, email addresses). When a user system really is needed, the user system can generate group members, and the model does not need to change.

**Groups do not nest**. A member channel can only be a provider instance name or a `channels` block name, not another `group:`. Alternative: nested groups (a group containing groups).
Rejected: it requires cycle detection, the expansion depth is unbounded, and the recipient semantics are unclear when a member is overridden at multiple layers; and the "echelon contains sub-teams" that nesting wants to express is already expressible with a flat member list. Expansion is **single-level, non-recursive**, and auditable.

**Member channels are unique within a group**. Sending to multiple people via the same channel is expressed with the `Recipients` list
(`channel: sms-duty, recipients: [138..., 139...]`), not by creating two members.
Duplicate channel members are rejected at save time. Two members with the same name each carrying half the recipients is the typical shape of a configuration accident.

**Validation** is consistent with rules: the ID follows the rule id pattern (`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`);
the member count is ≥1 and ≤64 (a group is "a team of people", not "an address book"); channel names are non-empty and recipient entries are non-empty;
Recipients does not try to interpret the contents (that is the channel's business).

## 3. Reference Syntax: the `group:` Prefix

**Anywhere a channel name is accepted** also accepts `group:name`:

- Rule `route[].channels` (the only way a rule binds to a group);
- Caller-explicit `channels` (`"channels": ["group:ops-oncall"]` in `POST /api/v1/notify`);
- The channel list of the static route tables `routes`/`level_routes`;
- The `escalation.to` escalation target.

**Alternative: a separate `groups` field on rules** (`route: [channels: [...], groups: [...]]`).
Rejected: it overlaps semantically with channels (union of the two sets? which comes first?), and caller-explicit channels, static routes,
and escalation all want to reference groups too. Adding a field per location is worse than one unified reference syntax.
**Alternative: an `@ops-oncall` prefix**. Shorter, but `@` is already taken by username semantics in the notification context
(@-mentions on Feishu/Twitter, @username on telegram), and someone reading a rule would mistake the @ for a person.
`group:` carries its own category word and is judgeable at a glance.
**Alternative: make groups a virtual provider** (register a synthetic channel named ops-oncall that fans out at delivery time).
Rejected: a virtual provider mixed into the provider registry would have to implement the entire Provider interface (Send/health check/start-stop),
without having independent send semantics; and delivery logs, rate limiting, and retries would all see a fake channel name, adding a layer of
translation to troubleshooting. A prefix reference stays a **routing-layer** concept, and the delivery layer always sees real channels.

Expansion semantics (the precedence between `Member.Recipients` and the notification's own `Recipients`):
when a member carries Recipients, **the member pins the recipients** (the group's audience is decided by the group);
when a member does not carry them, it falls back to the notification's own `n.Recipients[channel]`; when neither is present, it uses the channel instance's default target.

## 4. Routing by Group: Expansion Happens at a Single Point in the Delivery Orchestration

The expansion point is chosen at `NotificationService.enqueue` (`core/service/notification.go`),
that is, at the conversion from "channel reference → delivery task", for each channel reference:

```
ref = "feishu-oncall"          → itself, no recipient override
ref = "group:ops-oncall"       → group exists: one {channel, recipients?} per member
                                → group does not exist: the reference is recorded as a failed channel (unknown group)
```

**Why expand at enqueue rather than at engine evaluation time**:

1. **A single point covers all paths**. Explicit channels, rule rerouting, static routes, escalation, group digest,
   and recovery notifications all ultimately flow into enqueue, so expanding in one place takes effect everywhere; expanding in the engine would only cover the rule path,
   and explicit channels and the escalation chain would each have to be written again;
2. **The Decision keeps the "intent" rather than the "implementation"**. A reroute decision writes `group:ops-oncall`,
   and audience resolution is the delivery orchestration's responsibility. Two things with different change frequencies (rule policy vs on-call roster) are not in the same object;
3. **The dedup key is stable**. dedupKey is based on the notification content (including the `group:` reference text) and is unaffected by additions or
   removals of group members. Changing who is on call should not let the same alert slip through the dedup window and fire repeatedly.

**Failure semantics**: an unknown group reference **does not interrupt the other channels**, and is handled the same as
"channel instance does not exist / is disabled" (recorded item by item in `ProcessResult.Failed`). A group being deleted while a rule still references it is configuration drift, and the behavior
should be "this reference fails and is visible", not a 500 for the whole message.

**Empty expansion**: a group that exists but has empty members is blocked by validation at save time; at runtime the member list only ever grows and never
shrinks to zero (Delete removes the whole group), so the expansion result is always non-empty.

## 5. Management and Persistence

Isomorphic with the rule engine, to reduce a second mental model:

| Method | Path | Semantics |
|---|---|---|
| GET | `/api/v1/groups` | List |
| POST | `/api/v1/groups` | Create (400 on validation failure) |
| GET | `/api/v1/groups/{id}` | Single item |
| PUT | `/api/v1/groups/{id}` | Full replacement (changing on-call = changing members) |
| DELETE | `/api/v1/groups/{id}` | Delete (rules referencing it are not deleted; they fail at delivery time as unknown group) |

Persistence goes through the `Store` interface + `MemoryStore` + JSON `FileStore` (temp file + rename atomic write),
configured with `groups_store: /var/lib/herald/groups.json`. `Manager` holds the in-memory active table (Put/Delete refresh in place),
and the delivery hot path (`Resolver.Members`) reads only the in-memory snapshot, never touching disk. The `groups:` seed
list is loaded at startup (coexisting with the store, semantics the same as `rules:`). Groups have no shadow mode: a group is not a judgment but
data, and if the data is wrong you just change it back, with no observation period needed.

## 6. Explicit Boundaries

- Groups **do not participate in** rule matching (there is no group concept in Env); matching is based on message content, and only the routing destination uses groups;
- Groups **carry no hierarchy/roles** (no owner, no approval flow); that is the permission system's business;
- Nested groups and time-rotating on-call schedules (schedule) are not done; the on-call roster is integrated by an external scheduling system
  generating group members (PUT group), and herald does not build in a scheduling engine.
