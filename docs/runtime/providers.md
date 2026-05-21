# Provider 分类

## Provider Category

| 类型                  | 示例                 |
| ------------------- | ------------------ |
| Official API        | Telegram / Discord |
| Webhook API         | 飞书 / 企业微信          |
| SMTP                | 邮件                 |
| Third-party Push    | Server酱 / PushPlus  |
| Official Account    | 微信公众号模板消息          |
| Browser Automation  | WhatsApp Web       |

## Builtin Providers

| Provider     | 描述          |
| ------------ | ----------- |
| Telegram     | Bot API     |
| Discord      | Webhook     |
| Feishu       | 飞书机器人       |
| WeCom        | 企业微信机器人     |
| Email        | SMTP        |
| Generic Webhook | 通用 Webhook |

## Worker Providers

| Provider     | 描述            | 平台        |
| ------------ | ------------- | --------- |
| WhatsAppWeb  | 浏览器自动化        | Any + Chrome |
| SlackWeb     | 浏览器自动化        | Any + Chrome |

## Provider Descriptor

```yaml
providers:

  telegram:
    runtime: builtin
    type: bot-api
    config:
      token: "${TELEGRAM_BOT_TOKEN}"

  wechat:
    runtime: builtin
    type: third-party-push
    config:
      service: "pushplus"
      token: "${WECHAT_PUSHPLUS_TOKEN}"

  whatsapp_web:
    runtime: worker
    type: browser-automation
    platform:
      - any
    config:
      browser: "chromium"
```
