# Email (SMTP)

Send email notifications over SMTP.

## What It Does

The `email` Builtin Provider connects directly to an SMTP server and sends mail — no third-party relay. Suited to scenarios where you already have a mail server (corporate mailbox, self-hosted Postfix/Exim, cloud SMTP provider).

## Obtaining Credentials

You need a mailbox account that can send via SMTP. **Common pitfall: it is not your mailbox login password, it is an authorization code**:

| Service | Where to apply | What you get |
|--------|---------|--------|
| QQ Mail | Settings → Account → POP3/IMAP → enable IMAP and generate an **authorization code** | Use the authorization code as `password` |
| 163 Mail | Settings → POP3/SMTP → enable it and generate an **authorization password** | Use the authorization password as `password` |
| Gmail | Google account → 2-Step Verification → **App Password** | The App Password |
| Corporate mailbox | Ask your administrator for the SMTP address and account | The SMTP password |

For server addresses and ports, see the [Common SMTP Servers](#common-smtp-servers) table below.

## Sending Your First Message

Configure it (see below) and start with `heraldd serve --config config.yaml` (**an email recipient is required**):

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "demo",
    "level": "info",
    "title": "Herald first push",
    "body": "from curl",
    "channels": ["email"],
    "recipients": {"email": ["you@example.com"]}
  }'
```

The API returns `"accepted":["email"]` and your inbox gets `[INFO] Herald first push` (HTML template below). Didn't arrive? Check the spam folder + the [Troubleshooting Guide](/guide/troubleshooting).

## Configuration

| Key | Required | Description | Default |
|----|------|------|--------|
| `host` | ✅ | SMTP server address | None |
| `port` | ❌ | SMTP port | `587` |
| `username` | ❌ | SMTP authentication username (usually the full mailbox address) | Empty (anonymous sending) |
| `password` | ❌ | SMTP authentication password / authorization code | Empty |
| `from` | ❌ | The sender email address | Defaults to `username` when `username` is non-empty, otherwise required |
| `from_name` | ❌ | The sender display name | Empty |

If `host` or `from` is missing (and there is no `username` fallback), Provider creation fails immediately (`email: host is required` / `email: from is required`).

## Configuration Example

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

## Environment Variables

When Herald loads config, it replaces any **string value starting with `$`** in the provider config with the value of the same-named environment variable (using the `$VAR` form, looked up as `VAR`). Note: the `"${VAR}"` form with braces is **not expanded** (it looks up `{VAR}` and keeps the text as-is) — use `$VAR`.

| Environment Variable | Config Key | Description |
|----------|-----------|------|
| `EMAIL_USERNAME` | `username` | SMTP username (conventional name in `.env.example`) |
| `EMAIL_PASSWORD` | `password` | SMTP password / authorization code |
| `EMAIL_FROM` | `from` | The sender email address |

> ⚠️ `host`, `port`, and `from_name` usually contain no sensitive information and can be written directly in the config file.

## Message Templates and Limits

### Email Structure

- **Subject**: `[level] title` (e.g. `[WARNING] Server Alert`), with the level converted to upper case
- **From**: `from_name <from>` (when `from_name` is configured), otherwise the email address only
- **To**: multiple recipients separated by commas (from `targets`)
- **Body**: the task's `body` content
- **Format**: when `content.format == "html"`, HTML is sent (`Content-Type: text/html`); otherwise plain text (`Content-Type: text/plain`); both use `charset=UTF-8`

### Authentication

- When both `username` and `password` are non-empty, `PLAIN` authentication is used (`smtp.PlainAuth`)
- If either is empty, anonymous sending is attempted (depending on the server's policy)

### Limits

- The number of recipients per send is limited by the SMTP server (usually ≤ 100)
- Frequency and size limits are set by the SMTP server
- No built-in retry; HTTP-level retry does not apply (direct SMTP) — a failure returns an error immediately

### Capability Declaration

- `PayloadKinds`: `Content`
- `ContentFormats`: `html`, `plain`

## Common Errors

| Error | Cause and remedy |
|------|-----------|
| `email: host is required` | `host` not configured |
| `email: from is required` | `from` not configured and no `username` fallback |
| `failed to send email: 535 Authentication failed` | Wrong username/password/authorization code, or the server requires SSL/TLS and the port does not match |
| `failed to send email: 550 Relay not permitted` | The server refuses to relay — check whether the `from` domain is on the allowed list |
| `failed to send email: connection refused / timeout` | Wrong `host`/`port`, blocked by a firewall, or the network is unreachable |
| `failed to send email: 421 Too many connections` | Too much concurrency triggered the server's rate limit; lower the concurrency or send in batches |

Error format notes: SMTP errors are wrapped and returned directly as `failed to send email: {smtp error}`.

## Common SMTP Servers

| Service | Host | Port | Encryption | Notes |
|--------|------|------|------|------|
| Gmail | smtp.gmail.com | 587 | STARTTLS | Requires an App Password |
| Outlook/Office365 | smtp.office365.com | 587 | STARTTLS | Same account and password |
| QQ Mail | smtp.qq.com | 587/465 | STARTTLS/SSL | Requires an authorization code |
| 163 Mail | smtp.163.com | 465/994 | SSL | Requires an authorization password |
| Aliyun Corporate Mailbox | smtp.qiye.aliyun.com | 465 | SSL |  |
| Self-hosted Postfix | Custom | 25/587/465 | Depends on config | Port 25 is often blocked by cloud providers; 587+STARTTLS is recommended |

## Next Steps

- [Provider Overview](./overview.md) - see all Providers and how to enable them
- [Webhook](./webhook.md) - a custom HTTP receiver
- [Log](./log.md) - local debugging output
