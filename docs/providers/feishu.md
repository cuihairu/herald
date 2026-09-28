# 飞书

通过飞书自定义机器人 Webhook 将通知推送到飞书群。

## 作用

`feishu` Builtin Provider 调用飞书自定义机器人的 Webhook 接口，把 Herald 的通知以纯文本或交互卡片的形式发送到群聊。告警类通知可携带「确认告警」按钮，与 Herald 的告警回调联动。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `webhook_url` | ✅ | 自定义机器人 Webhook 地址（群设置 → 群机器人 → 添加自定义机器人获得） | 无 |
| `sign_secret` | ❌ | 机器人「签名校验」密钥。**注意：当前实现仅解析保存，发送时未参与加签计算**（预留字段） | 空 |
| `interactive_cards` | ❌ | 告警通知是否发送为交互卡片（含「确认告警」按钮） | `false` |

缺 `webhook_url` 时 Provider 创建即失败（`feishu: webhook_url is required`）。

> **⚠️ 签名校验**：若机器人在飞书侧开启了「签名校验」，当前实现不会计算签名，飞书会拒绝请求（返回签名不匹配错误）。请将机器人安全设置改为「自定义关键词」等不加签方式，或等待加签支持落地。

## 配置示例

```yaml
providers:
  feishu:
    type: feishu
    enabled: true
    config:
      webhook_url: "$FEISHU_WEBHOOK_URL"
      interactive_cards: true      # 可选：告警发送为交互卡片
      # sign_secret: "$FEISHU_SIGN_SECRET"   # 预留，当前未参与加签
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `FEISHU_WEBHOOK_URL` | `webhook_url` | 自定义机器人 Webhook 地址（`.env.example` 惯用名） |

## 消息模板与限制

**默认纯文本**（级别前缀 + 标题 + 正文）：

```
[错误] {title}

{body}
```

| 级别 | 前缀 |
|------|------|
| `error` | `[错误] ` |
| `warning` | `[警告] ` |
| `info` 及其他 | `[信息] ` |

**交互卡片**（`interactive_cards: true` 且通知关联了告警 ID 时）：

- 卡片 header 颜色：`error` / `critical` → 红色，`warning` → 橙色，其余 → 蓝色
- 卡片正文为 `lark_md` 格式
- 底部「确认告警」按钮，点击后回调 Herald 告警确认接口（见规则引擎 / 告警回调文档）

- Provider 能力声明仅支持 `plain`：默认纯文本消息中的 markdown 语法**不会**被飞书渲染
- 请求超时 30s；HTTP 408/429/5xx 会被包装为可重试错误，走统一重试
- 飞书侧限制：自定义机器人默认限频 100 条/分钟、5 条/秒，超限返回 `code: 9499`（频繁限流）

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `feishu: webhook_url is required` | 未配置 `webhook_url` |
| `feishu API error: sign match fail` | 机器人开了「签名校验」而当前实现不加签，改用「自定义关键词」安全设置 |
| `feishu API error: key words not found` | 机器人设置了「自定义关键词」但消息里不含该关键词（可在消息正文里带上关键词） |
| `feishu API error: webhook is invalid` / `url not found` | Webhook 地址错误或机器人已被删除/停用，重新获取地址 |
| `feishu API error: Forbidden` | 机器人被限流或被移出群聊 |

错误格式说明：飞书返回 `code != 0` 时报 `feishu API error: {msg}`；HTTP 非 2xx 时报 `unexpected status code: {code}, body: …`。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [企业微信](./wecom.md) - 企业微信群机器人推送
- [钉钉](./dingtalk.md) - 钉钉群机器人推送
