# Telegram

通过 Telegram Bot API 将通知推送到指定会话（私聊 / 群组 / 频道）。

## 作用

`telegram` Builtin Provider 调用 Telegram Bot API 的 `sendMessage` 接口，把 Herald 的通知内容发送到 `chat_id` 指定的会话。适合个人告警接收、群组值班通知等场景。

## 申请凭据

全程无需审批，两样东西：Bot Token 和 chat_id。

1. Telegram 里找 [@BotFather](https://t.me/BotFather) → 发送 `/newbot` → 按提示起名 → 得到 **Bot Token**（形如 `123456:ABC-DEF...`）
2. **给你的机器人发一条消息**（私聊必须用户先发起，否则机器人无法主动推送给你；拉进群则自动可见）
3. 拿 **chat_id**：浏览器打开 `https://api.telegram.org/bot<你的Token>/getUpdates`，在返回的 JSON 里找 `result[].message.chat.id`（私聊是正数，群组是 `-100…` 负数）

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
    "channels": ["telegram"]
  }'
```

API 返回 `"accepted":["telegram"]`，你的 Telegram 会收到 `🔴 Herald 第一条推送`。没收到？先 `getUpdates` 确认 token 和 chat_id 有效，再查[排错指南](/guide/troubleshooting)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `token` | ✅ | Bot Token，从 [@BotFather](https://t.me/BotFather) 创建机器人获得 | 无 |
| `chat_id` | ✅ | 目标会话 ID（私聊为数字 ID，群组为 `-100…` 负数 ID） | 无 |
| `parse_mode` | ❌ | 消息解析模式：`markdown` 或 `html`；留空则按纯文本发送 | 空 |
| `api_url` | ❌ | API 根地址，自建代理 / 反代时替换 | `https://api.telegram.org` |

缺 `token` 或 `chat_id` 时 Provider 创建即失败（`telegram: token is required` / `telegram: chat_id is required`），启动日志可见。

## 配置示例

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
      parse_mode: "markdown"        # 可选：markdown / html，留空纯文本
      # api_url: "https://api.telegram.org"   # 走自建代理时替换
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `TELEGRAM_BOT_TOKEN` | `token` | Bot Token（`.env.example` 惯用名） |
| `TELEGRAM_CHAT_ID` | `chat_id` | 目标会话 ID |

## 消息模板与限制

消息文本格式（级别 emoji + 粗体标题 + 正文）：

```
{级别emoji} *{title}*

{body}
```

| 级别 | emoji |
|------|-------|
| `error` | 🔴 |
| `warning` | 🟡 |
| `info` | 🔵 |
| 其他 | ⚪ |

- 粗体（`*…*`）需要 `parse_mode: "markdown"` 才会渲染；留空时星号原样显示
- Provider 能力声明支持 `markdown` / `plain` 两种内容格式
- Telegram 侧限制：单条消息文本最长 4096 字符，超长会被 API 拒绝（`message is too long`）
- 请求超时 30s；HTTP 408/429/5xx 会被包装为可重试错误，走统一重试

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `telegram: token is required` | 未配置 `token` |
| `telegram: chat_id is required` | 未配置 `chat_id` |
| `telegram API error: Unauthorized` | Token 无效或被吊销，重新向 @BotFather 获取 |
| `telegram API error: chat not found` | `chat_id` 错误，或用户未先给机器人发过消息（私聊需用户主动 `/start`） |
| `telegram API error: message is too long` | 超过 4096 字符上限，精简正文 |
| `telegram API error: Bad Request: can't parse entities` | `parse_mode` 与内容不匹配（如 markdown 星号未闭合），改纯文本或修正语法 |

错误格式说明：API 返回 `ok: false` 时报 `telegram API error: {description}`；HTTP 非 2xx 时报 `unexpected status code: {code}, body: …`。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [飞书](./feishu.md) - 飞书机器人推送
- [企业微信](./wecom.md) - 企业微信群机器人推送
