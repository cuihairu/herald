> English · [中文](./wechatmp.md)

# WeChat Official Account Template Messages

Send notifications to followers via the WeChat Official Account template message API.

## What it does

The `wechatmp` Builtin Provider uses the WeChat Official Account template message API (requires a verified service account) to push Herald notifications to users' WeChat. Good for formal business notices, order status, alert delivery, and similar scenarios.

## Get credentials

Template messages impose hard requirements on Official Account qualifications and the application chain is long; follow in order:

1. **Verified service account**: template messages are open only to **verified service accounts** (personal subscription accounts don't qualify). If you don't have one, register a service account at the [WeChat Official Accounts Platform](https://mp.weixin.qq.com/) and complete WeChat verification first
2. **AppID / AppSecret**: Official Accounts Platform → **Settings & Development** → **Basic Configuration**, visible once you become a developer; **add the Herald server's egress IP to the IP allowlist**, otherwise access_token requests are rejected
3. **Template**: **Advertising & Services** → **Template Messages** → pick from the template library or apply for a new template (title + keywords, review required); you get a **template ID** (shaped like `AtE-xxxx`)
4. **OpenID**: for users who follow the Official Account, obtained via the "get user list" API or business-side authorization; used as the delivery target

## Send your first message

After configuring (see below) and starting with `heraldd serve --config config.yaml` (**OpenID recipients are mandatory**):

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order.notice",
    "level": "info",
    "title": "订单已发货",
    "body": "您的订单 SF123456 已发出",
    "channels": ["wechatmp"],
    "recipients": {"wechatmp": ["用户的OpenID"]}
  }'
```

A template card arrives in WeChat's "Service Notifications" (title/body filled into the template's keyword slots, see below). Nothing arrived? Check the [troubleshooting guide](/guide/troubleshooting.en).

## Configuration options

| Key | Required | Description | Default |
|----|------|------|--------|
| `app_id` | ✅ | Official Account AppID | none |
| `app_secret` | ✅ | Official Account AppSecret | none |
| `template_id` | ✅ | template message ID (from the template library in the Official Account admin console) | none |
| `default_url` | ❌ | default URL opened when the template message is tapped | empty |

If `app_id`, `app_secret`, or `template_id` is missing, Provider creation fails immediately (`wechatmp: app_id is required` / `wechatmp: app_secret is required` / `wechatmp: template_id is required`); it shows up in startup logs.

## Configuration example

```yaml
providers:
  wechatmp:
    type: wechatmp
    enabled: true
    config:
      app_id: "$WECHATMP_APP_ID"
      app_secret: "$WECHATMP_APP_SECRET"
      template_id: "$WECHATMP_TEMPLATE_ID"
      default_url: "$WECHATMP_DEFAULT_URL"  # optional
```

## Environment variables

When loading config, Herald replaces string values in provider config **that start with `$`** with the value of the environment variable of the same name (the `$VAR` form, looked up as `VAR`). Note: the braced form `"${VAR}"` **is not expanded** (it is looked up as `{VAR}` and kept as-is); use `$VAR`.

| Environment variable | Corresponding config key | Description |
|----------|-----------|------|
| `WECHATMP_APP_ID` | `app_id` | Official Account AppID (the conventional name in `.env.example`) |
| `WECHATMP_APP_SECRET` | `app_secret` | Official Account AppSecret |
| `WECHATMP_TEMPLATE_ID` | `template_id` | template message ID |
| `WECHATMP_DEFAULT_URL` | `default_url` | default tap-through URL (optional) |

## Message template and limits

### Template field mapping

Herald maps content to WeChat template fields automatically:

| WeChat field | Herald content | Notes |
|---------|------------|------|
| `thing1` | `title` | title (20 characters max, truncated beyond) |
| `thing2` | `body` | body (30 characters max, truncated beyond) |
| `character_string1` | `level` | message level (e.g. `error`/`warning`/`info`) |
| `time3` | current time | timestamp `2006-01-02 15:04:05` |

> ⚠️ The template must be created/selected under "Template Messages" in the Official Account admin console, and **the field names must be** `thing1`, `thing2`, `character_string1`, `time3` (or corresponding fields of the same types), otherwise delivery errors out.

### Message limits

- At most 100 messages per minute per user
- At most 25 templates per Official Account
- Templates require WeChat review (1-7 days) and can only be used after approval
- The template's industry must match the Official Account's verified industry
- Content must not contain sensitive words

### Capability declaration

- `PayloadKinds`: `ProviderTemplate`, `Content`
- `SupportsTemplate`: `true`

## Common errors

| Error | Cause and fix |
|------|-----------|
| `wechatmp: app_id is required` | `app_id` is not configured |
| `wechatmp: app_secret is required` | `app_secret` is not configured |
| `wechatmp: template_id is required` | `template_id` is not configured |
| `wechatmp: at least one target (OpenID) is required` | no receiving user OpenID given at send time |
| `wechatmp: empty target found in targets` | the targets list contains an empty string |
| `wechatmp: failed to get access token: …` | failed to get access_token (wrong AppID/Secret, network unreachable, or rate limited) |
| `wechatmp: API error {errcode}: {errmsg}` | WeChat returned an error code; common ones:<br/>`40037` invalid template_id<br/>`45015` user not following / wrong OpenID<br/>`45009` API rate limit exceeded<br/>`40003` wrong AppSecret |

Error format notes: when the API returns `errcode != 0`, the error is `wechatmp: API error {errcode}: {errmsg}`; network/parse errors come back in the unified HTTP error format.

## Get a user's OpenID

After a user follows the Official Account you need their OpenID to send messages:

### Method 1: user authorization (web authorization)

Generate an authorization link for the user to authorize (silent authorization `snsapi_base`):

```
https://open.weixin.qq.com/connect/oauth2/authorize?appid=APPID&redirect_uri=REDIRECT_URI&response_type=code&scope=snsapi_base#wechat_redirect
```

In the callback, exchange the `code` for an `access_token` + `openid`.

### Method 2: the follow event

Handle the `subscribe` event in the Official Account server configuration and take `FromUserName` directly (that is the OpenID).

## Prerequisites (official limits)

- **Business qualifications**: business license, corporate bank account, CNY 300/year verification fee, 7-30 business days of review
- **Service account**: it must be a service account (subscription accounts have no template message permission)
- **Verified**: WeChat verification passed
- **Enable template messages**: turn it on in the admin console under "Features" → "Template Messages"

## Next steps

- [Provider overview](./overview.en.md) - see all Providers and how to enable them
- [WeChat personal push](./wechat.en.md) - the easy way with third-party services
- [WeChat Official Account guide](./wechat-official.en.md) - the full application process and limits
