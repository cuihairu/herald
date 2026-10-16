> English · [中文](./introduction.md)

# Introduction

## When you need Herald

When you find yourself **writing "send a notification" code all over the place** — assembling DingTalk payloads in the alert system, wiring up an SMS SDK in the operations backend, stuffing curl into CI — it's time to consider Herald. It is an event-driven **message notification distribution platform**: the business side just hands "something happened" to Herald, and it takes care of rendering the format, picking the right channel, and delivering reliably.

Typical scenarios:

- **System alert push**: monitoring events are routed by level; error goes to the on-call group, warning stays as a group message, and unacknowledged timeouts escalate automatically
- **Operational message outreach**: an announcement template is defined once and distributed to Feishu / WeCom / email at multiple points, with audiences managed as named groups
- **Verification codes and transactional SMS**: integrate Alibaba Cloud / Tencent Cloud / NetEase Yunxin, with rate limiting for anti-abuse + automatic retry on failure
- **Multi-channel fallback**: one alert fans out to multiple channels at the same time, and a single-channel failure is covered by retries and delivery logs

For the full "how to configure, how to send, what it looks like when running" of each scenario, see [Use Cases and Integration](/guide/use-cases.en).

## What it looks like

Herald is a single binary (or Docker container), a **lightweight, Provider-agnostic** unified subscription and delivery hub: the business side describes "what happened, what to notify about, and whom to notify", and all channel details (Telegram / Feishu / email / SMS … which one exactly, how to deliver, how to retry on failure) are handed to Herald; who receives which category on which channel and at what frequency is decided by the audience's own subscription relations. The core is a single notification pipeline:

```
Notification → audience resolution (whom to notify) → routing (which channel to take) → Delivery Task → Queue → Worker → Provider
                 ↑ template (how content is generated) · rule engine (allow/suppress/reroute) · dedup · retry · rate limiting
```

- **Notification ≠ Delivery**: one notification can expand into multiple deliveries (multi-channel fan-out), and each Delivery has its own state and retries
- **Audience ≠ Channel ≠ Provider**: the three dimensions "whom to notify", "which kind of notification", and "how to send" are decoupled — the audience's `group:` form is the [Notification Group](/design-notification-groups.en), and the `user:`-level subdivision (Recipient/Endpoint, configuration-driven) and the `channels` block are in the [Audience Domain Model Overview](/design-audience-model.en) and the [Configuration Reference](/guide/configuration.en); runtime ContactSurface binding and subscription/enrollment relations are likewise in the overview

- **HTTP First**: `POST /api/v1/notify` sends a notification with a single curl, curl-friendly; an official Go client ships with the repo (`apps-sdk/go`), and any HTTP client can connect directly
- **Multi-channel Providers**: a dozen-plus channels built in — instant messaging (Feishu, WeCom, DingTalk, Slack, Discord, Telegram, WeChat), email, SMS (Alibaba Cloud, Tencent Cloud, NetEase Yunxin), Webhook, etc. — behind a unified interface; see the [Provider Handbook](/providers/overview.en)
- **Runtime First**: a Provider can be built into the core process (Builtin) or run on a separate [Worker node](/runtime/worker.en) — acknowledging the differences in runtime environments for scenarios like browser automation
- **Event First**: it handles events rather than bare messages — routing (by type/level), dedup windows, exponential-backoff retry, per-Provider rate limiting, and rule-engine decisions (priority, default policies, shadow observation, for/group_by/inhibit/silence/escalation)

## Two ways to run it

**Standalone service**: `heraldd serve` starts a scheduling center of API + queue + worker pool, and business code in any language sends HTTP to it. Getting your first notification working takes about 1 minute following [Getting Started](/guide/getting-started.en).

**Embedded as a Go library**: you can also pull Herald's delivery core into your own program as a library; see [Using as a Go Library](/library-usage.en).

## How it differs from other approaches

Webhook notification tools solve "send a message to an address"; templates, dedup, retry, and rule routing all have to be written by the caller, and each vendor's message-push SDK is bound to a single channel, so switching channels means switching libraries. Herald makes aggregation, templates, routing, and reliability an independent layer, so business code does not change with the channel.

## Next Steps

- [Getting Started](/guide/getting-started.en) - run the full pipeline in 1 minute
- [Use Cases and Integration](/guide/use-cases.en) - complete integration for four typical scenarios
- [Provider Handbook](/providers/overview.en) - channel credential application and configuration
