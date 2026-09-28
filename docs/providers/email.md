# Email

`email` Builtin Provider 通过 SMTP 把通知作为邮件发送给收件人列表。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `host` | ✅ | SMTP 服务器地址（如 `smtp.example.com`） | 无 |
| `port` | ❌ | SMTP 端口 | `587` |
| `username` | ❌ | SMTP 认证用户名；同时是 `from` 的默认值 | 空 |
| `password` | ❌ | SMTP 认证密码 / 授权码 | 空 |
| `from` | 二选一 | 发件人地址；留空时取 `username`，仍为空则创建失败 | 空 |
| `from_name` | ❌ | 发件人显示名，设置后发件人为 `显示名 <地址>` | 空 |

- 缺 `host` 报 `email: host is required`；`from` 与 `username` 都为空报 `email: from is required`
- `username` 与 `password` **都非空**时启用 SMTP Plain 认证，否则匿名发送（多数公网 SMTP 会拒绝）

## 配置示例

```yaml
providers:
  email:
    type: email
    enabled: true
    config:
      host: "smtp.example.com"
      port: 587
      username: "$EMAIL_USERNAME"
      password: "$EMAIL_PASSWORD"
      from: "$EMAIL_FROM"
      from_name: "Herald 告警"    # 可选
```

发送时通过 `recipients` 指定收件人邮箱列表：

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "daily-report",
    "title": "每日巡检报告",
    "body": "<h1>巡检通过</h1><p>详情……</p>",
    "level": "info",
    "channels": ["email"],
    "recipients": { "email": ["ops@example.com", "dev@example.com"] }
  }'
```

## 环境变量

provider config 里以 `$` 开头的字符串值会在加载时展开为同名环境变量的值（`$VAR` 写法；`"${VAR}"` 带花括号不会被展开）。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `EMAIL_USERNAME` | `username` | SMTP 用户名（`.env.example` 惯用名） |
| `EMAIL_PASSWORD` | `password` | SMTP 密码 / 授权码 |
| `EMAIL_FROM` | `from` | 发件人地址 |

## 消息模板与格式

**主题**：`[级别] 标题`（级别大写，如 `[ERROR] 磁盘告警`；级别为空时仅标题）。

**正文格式**由通知内容的 `format` 决定：

| 内容 format | 邮件 Content-Type |
|-------------|-------------------|
| `html` | `text/html; charset=UTF-8` |
| 其他 / 空 | `text/plain; charset=UTF-8` |

正文原样发送，不做模板加工。

## 传输与限制

- 走标准 `net/smtp`：587 端口下 STARTTLS 由服务端协商自动升级；**不支持 465 端口的隐式 TLS**（SMTPS），如需 TLS 请使用支持 STARTTLS 的 587/25 端口
- 收件人取自 `targets`，为空报 `email: no recipients specified`
- 发送失败统一包装为 `failed to send email: {原因}`；SMTP 层无独立重试语义，5xx 类瞬时错误视 httpclient 之外——email 不走 httpclient，重试由上层统一投递重试承担
- 认证信息经 `smtp.PlainAuth` 发送：该实现仅在 TLS 连接上发送明文密码（标准库安全约束），所以服务器必须支持 STARTTLS，否则认证会失败

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `email: host is required` | 未配置 `host` |
| `email: from is required` | `from` 与 `username` 都为空 |
| `email: no recipients specified` | 请求未带 `recipients.email` |
| `failed to send email: 535 Authentication failed` | 用户名/密码错误；QQ/163 等国内邮箱须用**授权码**而非登录密码 |
| `failed to send email: tls: first record does not look like a SMTP handshake` | 端口是 465（隐式 TLS），改用 587 |
| `failed to send email: …unencrypted connection` | 服务器不支持 STARTTLS 却要求认证，换支持 TLS 的服务商或端口 |
| `failed to send email: 554 …` | 被收件方判为垃圾邮件 / 发件人未验证，检查 SPF/DKIM 与服务商要求 |

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [SMS Providers](./sms.md) - 短信通道配置
- [Webhook](./webhook.md) - 自定义 HTTP 接收端
