# Discord

通过 Discord Webhook（或 Bot API）将通知推送到 Discord 频道。

## 作用

`discord` Builtin Provider 把 Herald 的通知以 Embed 卡片形式发送到 Discord 频道。发送优先走 Webhook；未配置 Webhook 时回退到 Bot API 通道。

## 申请凭据

推荐 Webhook 方式，无需审批：

1. 打开 Discord 目标频道 → **编辑频道（齿轮）** → **整合** → **创建 Webhook**
2. 点 **复制 Webhook URL**（形如 `https://discord.com/api/webhooks/xxxx/yyyy`）

> ⚠️ Bot API 方式当前实现不附加 `Authorization` 头，会被 Discord 401 拒绝——**用 Webhook 方式**。

## 发第一条消息

配置好（见下）并 `heraldd serve --config config.yaml` 启动后：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "demo",
    "level": "error",
    "title": "Herald 第一条推送",
    "body": "from curl",
    "channels": ["discord"]
  }'
```

API 返回 `"accepted":["discord"]`（Webhook 成功时 Discord 返回 204），频道里出现红色 **Herald** Embed 卡片。没收到？查[排错指南](/guide/troubleshooting)。

## 配置项

三种方式按优先级排列（Webhook 优先）：

| 键 | 说明 |
|----|------|
| `webhook_url` | 完整 Webhook 地址（频道 → 编辑 → 整合 → Webhook）。配置后优先使用 |
| `webhook_id` + `webhook_token` | Webhook 地址的两段参数，Herald 自动拼 `https://discord.com/api/webhooks/{id}/{token}`。只配 `webhook_id` 不配 `webhook_token` 时创建即失败 |
| `bot_token` + `channel_id` | Bot API 通道：向 `{api_url}/channels/{channel_id}/messages` 发送。**注意：当前实现未给请求附加 `Authorization: Bot …` 认证头，Bot 通道实际会被 Discord 拒绝（401），生产请使用 Webhook 方式** |
| `api_url` | Bot API 根地址覆盖，自建代理时使用；默认 `https://discord.com/api/v10` |

一个键都没配（或只配了 `bot_token` / `channel_id` 之一）时 Provider 创建即失败（`discord: either webhook_url or bot_token+channel_id is required`）。

## 配置示例

```yaml
providers:
  discord:
    type: discord
    enabled: true
    config:
      # 方式一（推荐）：完整 Webhook 地址
      webhook_url: "$DISCORD_WEBHOOK_URL"
      # 方式二：Webhook 两段参数，由 Herald 拼地址
      # webhook_id: "$DISCORD_WEBHOOK_ID"
      # webhook_token: "$DISCORD_WEBHOOK_TOKEN"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `DISCORD_WEBHOOK_URL` | `webhook_url` | 完整 Webhook 地址（`.env.example` 惯用名） |
| `DISCORD_WEBHOOK_ID` | `webhook_id` | Webhook ID（惯用名，`$VAR` 引用即可） |
| `DISCORD_WEBHOOK_TOKEN` | `webhook_token` | Webhook Token（惯用名） |

## 消息模板与限制

消息以 Embed 形式发送，发送者名为 **Herald**：

| Embed 字段 | 取值 |
|------------|------|
| `title` / `description` | 通知标题 / 正文（原样，不做级别前缀） |
| `color` | 按级别着色，见下表 |
| `timestamp` | 通知创建时间（RFC 3339） |

级别颜色：

| 级别 | color（十进制） | 颜色 |
|------|----------------|------|
| `error` | `16711680` | 红 |
| `warning` | `16776960` | 橙 |
| `info` | `3447003` | 蓝 |
| 其他 | `9807270` | 灰 |

- Provider 能力声明支持 `markdown` / `plain` 两种内容格式（Embed 的 description 支持 Discord markdown）
- 请求超时 30s；HTTP 408/429/5xx 会被包装为可重试错误，走统一重试（Discord 限流返回 429 时自动重试）
- Discord 侧限制：Webhook 每频道约 30 条/分钟（持续超限会返回 429）；Embed 的 `description` 最长 4096 字符、`title` 最长 256 字符

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `discord: webhook_token is required when using webhook_id` | 只配了 `webhook_id`，补上 `webhook_token`（或直接用完整 `webhook_url`） |
| `discord: either webhook_url or bot_token+channel_id is required` | Webhook 与 Bot 通道都未配置（或只配了半组） |
| `discord API error: Unknown Webhook` | Webhook 地址错误或已被删除，重新创建 |
| `discord API error: 401: Unauthorized` | Token 无效；或走了 Bot 通道——当前实现未附加 Bot 认证头，请改用 Webhook |
| `unexpected status code: 429, body: …` | 触发 Discord 限流，该错误为可重试类型，会自动退避重试 |

错误格式说明：Discord 返回体中 `code != 0` 时报 `discord API error: {message}`；HTTP 非 2xx 时报 `unexpected status code: {code}, body: …`。Webhook 发送成功时 Discord 返回 204 无响应体。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Slack](./slack.md) - Slack Webhook 推送
- [Telegram](./telegram.md) - Telegram Bot 推送
