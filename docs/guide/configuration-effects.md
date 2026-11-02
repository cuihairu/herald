# Configuration Effects

This page visualizes every Herald configuration switch whose `off` → `on`
transition changes runtime behavior. Each entry includes the two static states
and a looping comparison GIF generated from the same switch component.

## Regenerate

The full pipeline is deterministic and repeatable:

```bash
python3 tools/config_effect/generate.py
pnpm --dir docs build
``+

It requires `python3`, a Playwright headless Chromium shell, and `ffmpeg`.
The command regenerates `manifest.json` and every `-off.png`, `-on.png`, and
`.gif` under `docs/public/screenshots/config/`. Keep all 27 switch records and
all three assets in sync when adding a switch.

## Security

### `auth.enabled`

When enabled, the management API requires a Bearer token or `X-API-Key`;
the service accepts a public/wildcard bind. When disabled, the API is anonymous,
but the service only accepts a loopback bind.

![auth.enabled off/on comparison](/screenshots/config/auth_enabled.gif)

<img src="/screenshots/config/auth_enabled-off.png" alt="API authentication off state" width="360" />
<img src="/screenshots/config/auth_enabled-on.png" alt="API authentication on state" width="360" />

## Delivery

### `dedup.enabled`

Duplicate identical events are collapsed within the configured window.

![dedup.enabled off/on comparison](/screenshots/config/dedup_enabled.gif)

<img src="/screenshots/config/dedup_enabled-off.png" alt="Deduplication off state" width="360" />
<img src="/screenshots/config/dedup_enabled-on.png" alt="Deduplication on state" width="360" />

### `digest.enabled`

Events are aggregated into one digest and flushed on the configured interval.

![digest.enabled off/on comparison](/screenshots/config/digest_enabled.gif)

<img src="/screenshots/config/digest_enabled-off.png" alt="Digest aggregation off state" width="360" />
<img src="/screenshots/config/digest_enabled-on.png" alt="Digest aggregation on state" width="360" />

### `feeds.enabled`

Public and private RSS feeds are served under `/feeds/**`.

![feeds.enabled off/on comparison](/screenshots/config/feeds_enabled.gif)

<img src="/screenshots/config/feeds_enabled-off.png" alt="RSS feeds off state" width="360" />
<img src="/screenshots/config/feeds_enabled-on.png" alt="RSS feeds on state" width="360" />

### `delivery.allow_phone`

Phone-class channels are released from the default intensity gate. Audience
phone bindings remain mandatory.

![delivery.allow_phone off/on comparison](/screenshots/config/delivery_allow_phone.gif)

<img src="/screenshots/config/delivery_allow_phone-off.png" alt="Phone channel off state" width="360" />
<img src="/screenshots/config/delivery_allow_phone-on.png" alt="Phone channel on state" width="360" />

## Integration

### `sources.enabled`

Telegram and WeChat Official Account source adapters accept inbound events.

![sources.enabled off/on comparison](/screenshots/config/sources_enabled.gif)

<img src="/screenshots/config/sources_enabled-off.png" alt="Source adapters off state" width="360" />
<img src="/screenshots/config/sources_enabled-on.png" alt="Source adapters on state" width="360" />

### `sources.reconcile.enabled`

The reconciler probes external platforms periodically and removes invalid
contact surfaces.

![sources.reconcile.enabled off/on comparison](/screenshots/config/sources_reconcile_enabled.gif)

<img src="/screenshots/config/sources_reconcile_enabled-off.png" alt="Source reconciliation off state" width="360" />
<img src="/screenshots/config/sources_reconcile_enabled-on.png" alt="Source reconciliation on state" width="360" />

## Providers

Each `providers.<name>.enabled` switch controls whether the provider can be
selected by routing; disabled providers are skipped. All 18 built-in provider
switches follow the same behavior.

<img src="/screenshots/config/provider_log_enabled-off.png" alt="Log provider disabled state" width="360" />
<img src="/screenshots/config/provider_log_enabled-on.png" alt="Log provider enabled state" width="360" />

![providers.log.enabled off/on comparison](/screenshots/config/provider_log_enabled.gif)

The complete list is: `log`, `telegram`, `feishu`, `wecom`, `dingtalk`,
`slack`, `discord`, `email`, `webhook`, `aliyunsms`, `tencentsms`,
`neteasesms`, `wechatmp`, `wechat`, `fcm`, `apns`, `jpush`, and `getui`.
Each has the matching `-off.png`, `-on.png`, and `.gif` assets in
[`manifest.json`](/screenshots/config/manifest.json).

### `providers.feishu.config.interactive_cards`

Alerts become interactive Feishu cards with an acknowledgement button; off
sends plain text messages.

![interactive_cards off/on comparison](/screenshots/config/feishu_interactive_cards.gif)

<img src="/screenshots/config/feishu_interactive_cards-off.png" alt="Feishu interactive cards off state" width="360" />
<img src="/screenshots/config/feishu_interactive_cards-on.png" alt="Feishu interactive cards on state" width="360" />

### `providers.jpush.config.apns_production`

Selects the APNs production environment instead of sandbox.

![apns_production off/on comparison](/screenshots/config/jpush_apns_production.gif)

<img src="/screenshots/config/jpush_apns_production-off.png" alt="JPush APNs sandbox state" width="360" />
<img src="/screenshots/config/jpush_apns_production-on.png" alt="JPush APNs production state" width="360" />
