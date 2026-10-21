---
layout: home

hero:
  name: Herald
  text: Unified Message Notification and Delivery Platform
  tagline: One HTTP request, delivered to any channel — instant messaging · SMS · email · Webhook · app push. Routing, retry, dedup, rate limiting, and a rule engine built in; single-binary deployment, or embed as a Go library.
  actions:
    - theme: brand
      text: Getting Started
      link: /guide/getting-started
    - theme: alt
      text: Use Cases
      link: /guide/use-cases
    - theme: alt
      text: Provider Overview
      link: /providers/overview
    - theme: alt
      text: Architecture Design
      link: /architecture/overview

features:
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 9.5h18"/><circle cx="6.3" cy="7.2" r="0.9"/><circle cx="9.3" cy="7.2" r="0.9"/><path d="M7 14h6"/><path d="m11 12 2 2-2 2"/></svg>'
    title: HTTP First
    details: curl-friendly REST API; the official Go client ships with the repo (apps-sdk/go) — any HTTP client can integrate directly
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><rect x="5" y="5" width="14" height="14" rx="2.5"/><path d="M9.5 9.5v5l4-2.5z"/><path d="M9 3v2"/><path d="M15 3v2"/><path d="M9 19v2"/><path d="M15 19v2"/></svg>'
    title: Runtime First
    details: accepts that Providers differ fundamentally by runtime environment; supports both the Builtin and Worker runtimes
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><path d="M2.5 12.5H7l2-6.5 3.5 13 2.5-9 1.8 5.5h4.7"/></svg>'
    title: Event First
    details: processes events rather than simply sending messages; routing, retry, dedup, and rate limiting built in
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><path d="M4 5.5v4a3 3 0 0 0 3 3h9.5"/><path d="M4 18.5v-4"/><circle cx="19" cy="12.5" r="2"/><path d="m17.6 11.1 2.8-2.8"/><path d="M14 12.5h1.5"/></svg>'
    title: Rule Engine
    details: 'expression rules decide allow/inhibit/reroute — priority, default policy, shadow observation, for/group_by/inhibit/silence/escalation'
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><circle cx="8.5" cy="8.5" r="3"/><circle cx="16.5" cy="9.5" r="2.4"/><circle cx="12" cy="16.5" r="3"/><path d="m10.6 10.7 1.6 3.4"/><path d="m14.7 11.4-1.6 3"/></svg>'
    title: Notification Groups
    details: named audiences — write group:&lt;id&gt; in any channel slot; members expand at delivery time, roster hot-updated via API
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><rect x="3.5" y="7" width="10" height="8" rx="1.5"/><rect x="10.5" y="12" width="10" height="7" rx="1.5"/><path d="M7 10.5h3"/><path d="M14 15h3"/><path d="M14 10.5h3.5"/></svg>'
    title: Worker Model
    details: standalone Runtime nodes for complex scenarios such as browser automation
---

## What Is Herald

Herald is a **lightweight, Provider-agnostic unified subscription and delivery hub**: callers only describe "what happened, what to notify about, and whom to notify", hand the notification over as a single HTTP request, and Herald takes care of audience resolution, template rendering, channel routing, and reliable delivery. Who receives what category of notification, on which channel, and at what frequency is decided by each party's own subscription relations. Built-in Providers cover instant messaging (Feishu, WeCom, DingTalk, Slack, Discord, Telegram, WeChat), SMS (Alibaba Cloud, Tencent Cloud, NetEase Yunxin), email, Webhook, log/debug channels, and app push (FCM, APNs, JPush, Getui); the decoupled audience-channel-Provider model is described in the [Audience Domain Model Overview](/design-audience-model).

Dedup, retry, rate limiting, multi-channel template reuse, and rule-engine allow/suppress/redirect are the common concerns of a notification system; they are handled uniformly on the platform side, so callers only care about "what to send and to whom".

## Use Cases

- **System alert push**: events from monitoring and inspection are graded and routed by the rule engine — instant pop-ups in IM groups with SMS fallback — and the escalation steps up automatically per the escalation policy if unacknowledged past the timeout.
- **Operational message reach**: define a template once and reuse it across channels; named audiences (Notification Groups) maintain the roster, so an operational announcement lands in Feishu/WeCom/email in a single shot.
- **Verification codes and transactional SMS**: connect to the Alibaba Cloud/Tencent Cloud/NetEase Yunxin SMS channels with built-in rate limiting against abuse and failed-delivery retry, so high-urgency messages like verification codes are never dropped.
- **Multi-channel fallback retry**: the same event can be routed to multiple channels; if the primary channel fails, automatic retries and alternate-channel delivery kick in with exponential backoff until success or the retry limit.
- **CI/CD and automation integration**: integrate with a single curl command, or use the Go client shipped with the repo; the final state of build, deploy, and backup tasks lands directly in the group chat, or fans out to your own systems via Webhook.

## Interface at a Glance

A quick look at the management console (Dashboard): an auto-advancing carousel — you can also flip through slides with the arrows on either side, the dots below, or the ←/→ arrow keys, and click any slide to view it full size:

<script setup>
const showcaseSlides = [
  { image: '/screenshots/overview.png', title: 'Dashboard', caption: 'Dashboard: system status, notification counts, and Provider health at a glance' },
  { image: '/screenshots/providers.png', title: 'Providers', caption: 'Providers: enable, health, and configuration entry points for channel instances' },
  { image: '/screenshots/rules.png', title: 'Notification Rules', caption: 'Notification Rules: priority, active/observation modes, and shadow-hit statistics' },
  { image: '/screenshots/groups.png', title: 'Notification Groups', caption: 'Notification Groups: named audiences and the channel recipient roster' },
  { image: '/screenshots/workers.png', title: 'Workers', caption: 'Workers: local/remote Runtime nodes and heartbeat status' },
  { image: '/screenshots/logs.png', title: 'Delivery Logs', caption: 'Delivery Logs: channel, level, status, and duration for each notification' },
  { image: '/screenshots/send.png', title: 'Send Message', caption: 'Send Message: send one notification by hand from the console to debug the delivery path' },
]
</script>

<ShowcaseCarousel :slides="showcaseSlides" locale="en" />

The screenshots above come from a locally running instance (in-memory queue + log Provider); the data is demo seed data.

## Quick Links

- [Getting Started](/guide/getting-started) — send your first notification in 60 seconds with zero credentials
- [Use Cases and Integration](/guide/use-cases) — how to hook up alert push / operational reach / verification-code SMS / multi-channel fallback
- [Provider Overview](/providers/overview) — for every channel: request credentials → configure → first message
- [Troubleshooting Guide](/guide/troubleshooting) — startup failures, messages not arriving, retries not taking effect
- [Architecture Design](/architecture/overview) — Runtime, queue, and delivery models
