# Email (SMTP)

通过 SMTP 协议发送邮件通知。

## 作用

`email` Builtin Provider 直接连接 SMTP 服务器发送邮件，无需第三方中转。适合已有邮件服务器（企业邮箱、自建 Postfix/Exim、云厂商 SMTP）的场景。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `host` | ✅ | SMTP 服务器地址 | 无 |
| `port` | ❌ | SMTP 端口 | `587` |
| `username` | ❌ | SMTP 认证用户名（通常为邮箱全称） | 空（匿名发送） |
| `password` | ❌ | SMTP 认证密码/授权码 | 空 |
| `from` | ❌ | 发件人邮箱地址 | 若 `username` 非空则默认取 `username`，否则必填 |
| `from_name` | ❌ | 发件人显示名称 | 空 |

缺 `host` 或 `from`（且无 `username` 回退）时 Provider 创建即失败（`email: host is required` / `email: from is required`）。

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
      from_name: "Herald Alert"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `EMAIL_USERNAME` | `username` | SMTP 用户名（`.env.example` 惯用名） |
| `EMAIL_PASSWORD` | `password` | SMTP 密码/授权码 |
| `EMAIL_FROM` | `from` | 发件人邮箱地址 |

> ⚠️ `host`、`port`、`from_name` 通常不含敏感信息，可直接写在配置文件中。

## 消息模板与限制

### 邮件结构

- **Subject**: `[级别] 标题`（如 `[WARNING] 服务器告警`），级别转大写
- **From**: `from_name <from>`（若配置 `from_name`），否则仅邮箱
- **To**: 多个收件人用逗号分隔（来自 `targets`）
- **Body**: 任务 `body` 内容
- **格式**: 当 `content.format == "html"` 时发送 HTML（`Content-Type: text/html`），否则纯文本（`Content-Type: text/plain`），均 `charset=UTF-8`

### 认证

- `username` + `password` 均非空时使用 `PLAIN` 认证（`smtp.PlainAuth`）
- 任一为空则尝试匿名发送（视服务器策略而定）

### 限制

- 单次发送收件人数由 SMTP 服务器限制（通常 ≤ 100）
- 发送频率、大小限制由 SMTP 服务器决定
- 无内置重试；HTTP 层面的重试不适用（直连 SMTP），失败即返回错误

### 能力声明

- `PayloadKinds`: `Content`
- `ContentFormats`: `html`、`plain`

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `email: host is required` | 未配置 `host` |
| `email: from is required` | 未配置 `from` 且无 `username` 回退 |
| `failed to send email: 535 Authentication failed` | 用户名/密码/授权码错误，或服务器要求 SSL/TLS 而端口未匹配 |
| `failed to send email: 550 Relay not permitted` | 服务器拒绝中转，检查 `from` 域名是否在许可列表 |
| `failed to send email: connection refused / timeout` | `host`/`port` 错误、防火墙拦截、网络不通 |
| `failed to send email: 421 Too many connections` | 并发过高触发服务器限流，降低并发或分批发送 |

错误格式说明：SMTP 错误直接包装返回 `failed to send email: {smtp error}`。

## 常见 SMTP 服务器参考

| 服务商 | Host | Port | 加密 | 备注 |
|--------|------|------|------|------|
| Gmail | smtp.gmail.com | 587 | STARTTLS | 需开启「应用专用密码」 |
| Outlook/Office365 | smtp.office365.com | 587 | STARTTLS | 同账号密码 |
| QQ 邮箱 | smtp.qq.com | 587/465 | STARTTLS/SSL | 需开启「授权码」 |
| 163 邮箱 | smtp.163.com | 465/994 | SSL | 需开启「授权密码」 |
| 阿里云企业邮箱 | smtp.qiye.aliyun.com | 465 | SSL |  |
| 自建 Postfix | 自定 | 25/587/465 | 视配置 | 25 端口常被云厂商封禁，建议 587+STARTTLS |

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Webhook](./webhook.md) - 自定义 HTTP 接收端
- [Log](./log.md) - 本地调试输出