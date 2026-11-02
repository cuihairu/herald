# 配置效果

本页可视化 Herald 中每个 `off` → `on` 会改变运行时行为的配置开关。每个条目都包含
两个静态状态图，以及由同一开关组件生成的循环对比 GIF。

## 重新生成

完整流水线可确定性重跑：

```bash
python3 tools/config_effect/generate.py
pnpm --dir docs build
```

依赖为 `python3`、Playwright headless Chromium shell 与 `ffmpeg`。命令会重建
`manifest.json` 以及 `docs/public/screenshots/config/` 下全部 `-off.png`、
`-on.png`、`.gif`。新增开关时必须保持 27 条记录与三类资产同步。

## 安全

### `auth.enabled`

开启后管理面 API 需要 Bearer token 或 `X-API-Key`，并允许公网/通配绑定；关闭后 API
匿名访问，但只允许回环绑定。

![auth.enabled off/on 对比](/screenshots/config/auth_enabled.gif)

<img src="/screenshots/config/auth_enabled-off.png" alt="API 鉴权关闭状态" width="360" />
<img src="/screenshots/config/auth_enabled-on.png" alt="API 鉴权开启状态" width="360" />

## 投递

### `dedup.enabled`

同一窗口内的相同事件会被折叠。

![dedup.enabled off/on 对比](/screenshots/config/dedup_enabled.gif)

<img src="/screenshots/config/dedup_enabled-off.png" alt="去重关闭状态" width="360" />
<img src="/screenshots/config/dedup_enabled-on.png" alt="去重开启状态" width="360" />

### `digest.enabled`

事件聚合为一条摘要，并按配置间隔翻转投递。

![digest.enabled off/on 对比](/screenshots/config/digest_enabled.gif)

<img src="/screenshots/config/digest_enabled-off.png" alt="Digest 关闭状态" width="360" />
<img src="/screenshots/config/digest_enabled-on.png" alt="Digest 开启状态" width="360" />

### `feeds.enabled`

开放 `/feeds/**` 下的公共与私密 RSS feed。

![feeds.enabled off/on 对比](/screenshots/config/feeds_enabled.gif)

<img src="/screenshots/config/feeds_enabled-off.png" alt="RSS feed 关闭状态" width="360" />
<img src="/screenshots/config/feeds_enabled-on.png" alt="RSS feed 开启状态" width="360" />

### `delivery.allow_phone`

解除电话类渠道的默认强度门；受众电话绑定仍必须存在。

![delivery.allow_phone off/on 对比](/screenshots/config/delivery_allow_phone.gif)

<img src="/screenshots/config/delivery_allow_phone-off.png" alt="电话渠道关闭状态" width="360" />
<img src="/screenshots/config/delivery_allow_phone-on.png" alt="电话渠道开启状态" width="360" />

## 集成

### `sources.enabled`

Telegram 与微信公众号来源适配器开始接受入站事件。

![sources.enabled off/on 对比](/screenshots/config/sources_enabled.gif)

<img src="/screenshots/config/sources_enabled-off.png" alt="来源适配器关闭状态" width="360" />
<img src="/screenshots/config/sources_enabled-on.png" alt="来源适配器开启状态" width="360" />

### `sources.reconcile.enabled`

定期探测外部平台，并清理失效联系面。

![sources.reconcile.enabled off/on 对比](/screenshots/config/sources_reconcile_enabled.gif)

<img src="/screenshots/config/sources_reconcile_enabled-off.png" alt="来源对账关闭状态" width="360" />
<img src="/screenshots/config/sources_reconcile_enabled-on.png" alt="来源对账开启状态" width="360" />

## Provider

每个 `providers.<name>.enabled` 开关决定该 provider 是否可被路由选中；关闭后会被
跳过。18 个内置 provider 开关行为一致。

<img src="/screenshots/config/provider_log_enabled-off.png" alt="日志 Provider 关闭状态" width="360" />
<img src="/screenshots/config/provider_log_enabled-on.png" alt="日志 Provider 开启状态" width="360" />

![providers.log.enabled off/on 对比](/screenshots/config/provider_log_enabled.gif)

完整清单为：`log`、`telegram`、`feishu`、`wecom`、`dingtalk`、`slack`、`discord`、
`email`、`webhook`、`aliyunsms`、`tencentsms`、`neteasesms`、`wechatmp`、`wechat`、
`fcm`、`apns`、`jpush`、`getui`。每个开关在
[`manifest.json`](/screenshots/config/manifest.json) 都有对应 `-off.png`、`-on.png`、
`.gif`。

### `providers.feishu.config.interactive_cards`

告警渲染为带「确认告警」按钮的飞书交互卡片；关闭时发送普通文本消息。

![interactive_cards off/on 对比](/screenshots/config/feishu_interactive_cards.gif)

<img src="/screenshots/config/feishu_interactive_cards-off.png" alt="飞书交互卡片关闭状态" width="360" />
<img src="/screenshots/config/feishu_interactive_cards-on.png" alt="飞书交互卡片开启状态" width="360" />

### `providers.jpush.config.apns_production`

选择 APNs 生产环境，而不是沙盒环境。

![apns_production off/on 对比](/screenshots/config/jpush_apns_production.gif)

<img src="/screenshots/config/jpush_apns_production-off.png" alt="JPush APNs 沙盒状态" width="360" />
<img src="/screenshots/config/jpush_apns_production-on.png" alt="JPush APNs 生产状态" width="360" />
