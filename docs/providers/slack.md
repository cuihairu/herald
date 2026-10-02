# Slack

通过 Slack Incoming Webhook 将通知推送到 Slack 频道。

## 作用

`slack` Builtin Provider 调用 Slack Incoming Webhook，以带色 attachment 的形式把 Herald 的通知发送到固定频道，消息附带级别与来源 Provider 字段。

## 申请凭据

无需审批，但要在 Slack 后台创建 App：

1. 打开 [api.slack.com/apps](https://api.slack.com/apps) → **Create New App** → **From scratch** → 起名、选工作区
2. 左侧 **Incoming Webhooks** → 打开 **Activate Incoming Webhooks** → **Add New Webhook to Workspace** → 选择目标频道
3. 复制生成的 Webhook URL（形如 `https://hooks.slack.com/services/T00/B00/xxxx`）

> ⚠️ Slack 这家是唯一**没有启动校验**的 Provider：`webhook_url` 配错也要到发送时才报错，所以拿到 URL 后务必按下面发一条验证。

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
    "channels": ["slack"]
  }'
```

API 返回 `"accepted":["slack"]`，频道里出现 **Herald** 发的红色 attachment 消息（带 Level/Provider 字段）。没收到？查[排错指南](/guide/troubleshooting)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `webhook_url` | ✅（实际必需） | Incoming Webhook 地址（Slack App → Incoming Webhooks → Add New Webhook 获得） | 无 |

> **说明**：代码的 Config 结构体里还声明了 `workspace` / `app_id` / `app_secret` 三个字段，但当前实现只读取 `webhook_url`，其余为预留，配置不会生效。
>
> **⚠️ 无启动校验**：与其他 Provider 不同，`webhook_url` 为空时 Provider 仍能创建成功，错误会在发送阶段暴露（请求构造失败）。请确认配置加载后跑通一次发送验证。

## 配置示例

```yaml
providers:
  slack:
    type: slack
    enabled: true
    config:
      webhook_url: "$SLACK_WEBHOOK_URL"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `SLACK_WEBHOOK_URL` | `webhook_url` | Incoming Webhook 地址（`.env.example` 惯用名） |

## 消息模板与限制

消息以 attachment 形式发送，发送者为 **Herald**（头像 `:bell:`）：

| 字段 | 取值 |
|------|------|
| `title` / `text` | 通知标题 / 正文（原样，不做级别前缀或标题加色） |
| `color` | 按级别着色，见下表 |
| `fields` | `Level`（级别）、`Provider`（来源 Provider 名），均为短字段 |
| `footer` | `Herald` |
| `ts` | 通知创建时间（Unix 秒） |

级别颜色：

| 级别 | color |
|------|-------|
| `error` | `danger`（红） |
| `warning` | `warning`（黄） |
| `info` | `#36a64f`（绿） |
| 其他 | `#808080`（灰） |

- Provider 能力声明支持 `markdown` / `plain` 两种内容格式（attachment text 支持 Slack mrkdwn）
- 请求超时 30s；HTTP 408/429/5xx 会被包装为可重试错误，走统一重试
- Slack 侧限制：Incoming Webhook 只能发到创建时绑定的频道，1 条 Webhook 每秒约 1 条，超限返回 `HTTP 429`（带 `Retry-After`）

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `slack API error: invalid_payload` | 请求体不合法（一般为超长或字段异常），精简正文后重试 |
| `slack API error: channel_not_found` | Webhook 绑定的频道已被删除，重新创建 Webhook |
| `slack API error: channel_is_archived` | 频道已归档，换频道重建 Webhook |
| `slack API error: invalid_credentials` / HTTP 404 | Webhook 地址已失效（App 卸载或 Webhook 被撤销），重新生成 |
| `failed to send request: …` | `webhook_url` 为空或地址非法——发送阶段才会暴露，检查配置 |

错误格式说明：Slack 返回 `{"ok": false, "error": …}` 时报 `slack API error: {error}`；HTTP 非 2xx 时报 `unexpected status code: {code}, body: …`。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Discord](./discord.md) - Discord Webhook 推送
- [Telegram](./telegram.md) - Telegram Bot 推送
