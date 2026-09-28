# 钉钉

通过钉钉自定义机器人 Webhook 将通知推送到钉钉群。

## 作用

`dingtalk` Builtin Provider 调用钉钉自定义机器人的 Webhook 接口，以 markdown 消息把 Herald 的通知发送到群聊。

## 配置项

`webhook_url` 与 `access_token` 二选一（都配置时 `webhook_url` 优先）：

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `webhook_url` | 二选一 | 完整机器人 Webhook 地址 | 无 |
| `access_token` | 二选一 | 机器人 Webhook 地址里的 `access_token` 参数，Herald 自动拼 `https://oapi.dingtalk.com/robot/send?access_token={access_token}` | 无 |
| `secret` | ❌ | 机器人「加签」密钥（SEC 开头）。**注意：当前实现仅解析保存，发送时未参与加签计算**（预留字段） | 空 |

两者都缺时 Provider 创建即失败（`dingtalk: webhook_url or access_token is required`）。

> **⚠️ 加签**：若机器人在钉钉侧安全设置选择了「加签」，当前实现不会计算签名，钉钉会拒绝请求（`sign not match`）。请改用「自定义关键词」或「IP 白名单（段）」安全设置。代码里同样预留了 @成员（`at`）字段，当前未使用。

## 配置示例

```yaml
providers:
  dingtalk:
    type: dingtalk
    enabled: true
    config:
      # 方式一：完整 Webhook 地址
      webhook_url: "$DINGTALK_WEBHOOK_URL"
      # 方式二：只填 access_token，由 Herald 拼地址
      # access_token: "$DINGTALK_ACCESS_TOKEN"
      # secret: "$DINGTALK_SECRET"   # 预留，当前未参与加签
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `DINGTALK_WEBHOOK_URL` | `webhook_url` | 机器人 Webhook 地址（惯用名） |
| `DINGTALK_ACCESS_TOKEN` | `access_token` | Webhook 地址中的 `access_token`（`.env.example` 惯用名） |
| `DINGTALK_SECRET` | `secret` | 加签密钥（预留，当前未参与加签） |

## 消息模板与限制

所有消息固定使用钉钉 `markdown` 类型。标题为 `### ` 三级标题，按级别着色：

| 级别 | 标题渲染 |
|------|---------|
| `error` | `### <font color='#ff0000'>{title}</font>`（红色） |
| `warning` | `### <font color='#ff9900'>{title}</font>`（橙色） |
| `info` 及其他 | `### {title}`（默认色） |

正文后追加分隔线与发送时间戳：

```
### {title}

{body}

---

> 2026-09-28 10:00:00
```

- Provider 能力声明支持 `markdown` / `plain` 两种内容格式，但发送恒为 markdown 类型
- 请求超时 30s；HTTP 408/429/5xx 会被包装为可重试错误，走统一重试
- 钉钉侧限制：每个机器人每分钟最多 20 条，超限返回 `errcode: 130101`（限流）；markdown 消息最长 5000 字节

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `dingtalk: webhook_url or access_token is required` | `webhook_url` / `access_token` 都未配置 |
| `dingtalk API error: sign not match` | 机器人安全设置为「加签」而当前实现不加签，改用「自定义关键词」或 IP 白名单 |
| `dingtalk API error: keywords not in content` | 安全设置为「自定义关键词」但消息里不含该关键词（把关键词设为标题中的一个词即可） |
| `dingtalk API error: ip is not in white list` | 服务器出口 IP 不在机器人「IP 白名单（段）」内，后台补录 |
| `errcode: 130101` | 触发限流（每分钟 20 条），降低发送频率 |

错误格式说明：钉钉返回 `errcode != 0` 时报 `dingtalk API error: {errmsg}`；HTTP 非 2xx 时报 `unexpected status code: {code}, body: …`。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [企业微信](./wecom.md) - 企业微信群机器人推送
- [飞书](./feishu.md) - 飞书机器人推送
