# Email (SMTP)

通过 SMTP 协议发送邮件通知，兼容所有标准 SMTP 服务商（Gmail、Outlook、QQ 邮箱、企业邮箱、自建 Postfix 等）。

## 作用

`email` Builtin Provider 直接使用 Go `net/smtp` 库通过 SMTP 发送邮件。支持纯文本与 HTML 两种格式，适合告警通知、报表发送、账号验证等场景。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `host` | ✅ | SMTP 服务器地址（如 `smtp.gmail.com`、`smtp.qq.com`） | 无 |
| `port` | ❌ | SMTP 端口 | `587` |
| `username` | ❌ | 登录用户名（通常为邮箱地址） | 空（不鉴权） |
| `password` | ❌ | 登录密码或应用专用密码 | 空 |
| `from` | ✅ | 发件人邮箱地址 | 若留空且配置了 `username`，则回退为 `username` |
| `from_name` | ❌ | 发件人显示名称 | 空 |

缺 `host` 或 `from`（且无 `username` 回退）时 Provider 创建即失败（`email: host is required` / `email: from is required`），启动日志可见。

## 配置示例

### Gmail（需开启两步验证并生成应用专用密码）

```yaml
providers:
  email:
    type: email
    enabled: true
    config:
      host: "smtp.gmail.com"
      port: 587
      username: "$EMAIL_USERNAME"
      password: "$EMAIL_APP_PASSWORD"
      from: "$EMAIL_FROM"
      from_name: "Herald Alert"
```

### 企业邮箱 / 自建 SMTP（465 SSL 端口）

```yaml
providers:
  email:
    type: email
    enabled: true
    config:
      host: "smtp.exmail.qq.com"
      port: 465
      username: "$EMAIL_USERNAME"
      password: "$EMAIL_PASSWORD"
      from: "$EMAIL_FROM"
      from_name: "系统通知"
```

### 仅发件人、无鉴权（内网直连 Postfix）

```yaml
providers:
  email:
    type: email
    enabled: true
    config:
      host: "smtp.internal.example.com"
      port: 25
      from: "alerts@example.com"
      from_name: "内网监控"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `EMAIL_HOST` | `host` | SMTP 服务器地址 |
| `EMAIL_PORT` | `port` | SMTP 端口（数字） |
| `EMAIL_USERNAME` | `username` | 登录用户名 |
| `EMAIL_PASSWORD` | `password` | 登录密码/应用专用密码 |
| `EMAIL_FROM` | `from` | 发件人邮箱 |
| `EMAIL_FROM_NAME` | `from_name` | 发件人显示名称 |

## 消息模板与限制

### 邮件结构

- **主题**：`[LEVEL] title`（如 `[WARNING] CPU 使用率超过 90%`）
- **发件人**：`from_name <from>`（若配置了 `from_name`）
- **收件人**：任务的 `targets` 字段（多个邮箱用逗号分隔）
- **正文**：任务的 `body`
- **格式**：
  - `task.Payload.Content.Format == "html"` → `text/html; charset=UTF-8`
  - 否则 → `text/plain; charset=UTF-8`

### 能力声明

- `PayloadKinds`: `content`（标准 title/body）
- `ContentFormats`: `html` / `plain`

### 限制说明

| 限制项 | 说明 |
|--------|------|
| **SMTP 服务商限额** | Gmail 500/天、Outlook 300/分钟、企业邮箱视套餐而定 |
| **连接超时** | 由 Go `net/smtp` 决定（默认无显式超时，建议服务商侧配置） |
| **大附件** | 不支持附件（仅纯文本/HTML 正文） |
| **重试** | 网络错误/临时失败走统一重试（429/5xx 等价语义由上层限流器处理） |

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `email: host is required` | 未配置 `host` |
| `email: from is required` | 未配置 `from` 且无 `username` 可回退 |
| `failed to send email: 535 Authentication failed` | 用户名/密码错误；Gmail 需用应用专用密码而非登录密码 |
| `failed to send email: 550 Sender address rejected` | `from` 与认证账号不匹配，或服务商要求发件人验证 |
| `failed to send email: 421 Too many connections` | 并发连接过多，降低并发或配置限流器 |
| `failed to send email: dial tcp: timeout` | 网络不通、防火墙拦截、或端口错误（465 需 SSL、587 需 STARTTLS） |
| `failed to send email: 554 Message rejected` | 内容触发反垃圾规则，精简正文或调整发件人信誉 |

错误格式：`failed to send email: {底层 smtp 错误}`。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Webhook](./webhook.md) - 自定义 HTTP 接收端
- [Log](./log.md) - 本地调试输出