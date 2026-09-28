# 企业微信

通过企业微信群机器人 Webhook 将通知推送到企业微信群聊。

## 作用

`wecom` Builtin Provider 调用企业微信群机器人的 Webhook 接口，以 markdown 消息把 Herald 的通知发送到群聊。适合企业内部值班群、告警群场景。

## 配置项

`webhook_url` 与 `key` 二选一（都配置时 `webhook_url` 优先）：

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `webhook_url` | 二选一 | 完整机器人 Webhook 地址 | 无 |
| `key` | 二选一 | 机器人 Webhook 的 `key` 参数（群机器人创建页可见），Herald 自动拼 `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key={key}` | 无 |

两者都缺时 Provider 创建即失败（`wecom: webhook_url or key is required`）。

> **说明**：代码里预留了 `mentioned_list` / `mentioned_mobile_list`（@成员）结构字段，当前消息构造未使用，配置它们不会生效。

## 配置示例

```yaml
providers:
  wecom:
    type: wecom
    enabled: true
    config:
      # 方式一：完整 Webhook 地址
      webhook_url: "$WECOM_WEBHOOK_URL"
      # 方式二：只填 key，由 Herald 拼地址
      # key: "$WECOM_WEBHOOK_KEY"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `WECOM_WEBHOOK_URL` | `webhook_url` | 机器人 Webhook 地址（`.env.example` 惯用名） |
| `WECOM_WEBHOOK_KEY` | `key` | 机器人 Webhook 的 `key` 参数（惯用名，`$VAR` 引用即可） |

## 消息模板与限制

所有消息固定使用企业微信 `markdown` 类型。标题按级别加色：

| 级别 | 标题渲染 |
|------|---------|
| `error` | `<font color='warning'>**{title}**</font>`（橙红色） |
| `warning` | `<font color='info'>**{title}**</font>`（绿色） |
| `info` 及其他 | `**{title}**`（默认色） |

正文后追加引用样式的发送时间戳：

```
**{title}**

{body}

> 2026-09-28 10:00:00
```

- Provider 能力声明支持 `markdown` / `plain` 两种内容格式，但发送恒为 markdown 类型
- 请求超时 30s；HTTP 408/429/5xx 会被包装为可重试错误，走统一重试
- 企业微信侧限制：机器人每群每分钟最多 20 条，超限返回 `errcode: 45009`；markdown 内容最长 4096 字节

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `wecom: webhook_url or key is required` | `webhook_url` / `key` 都未配置 |
| `wecom API error: webhook invalid` | key 错误或机器人已被删除，重新创建机器人获取地址 |
| `wecom API error: webhook rejected` | 机器人限流（每分钟 20 条）或 IP 不在「企业可信 IP」名单，后台补录服务器出口 IP |
| `errcode: 45009` | 触发接口频率限制，降低发送频率 |

错误格式说明：企业微信返回 `errcode != 0` 时报 `wecom API error: {errmsg}`；HTTP 非 2xx 时报 `unexpected status code: {code}, body: …`。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [微信公众号](./wechat-official.md) - 公众号模板消息推送
- [钉钉](./dingtalk.md) - 钉钉群机器人推送
