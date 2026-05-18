# 核心设计

## 重要概念区分

### Provider vs Runtime

很多 IM 软件：

- 没有官方 API
- 不允许自动化
- 不允许 Hook
- 不允许逆向
- 不允许 Bot

这种 Runtime 本质属于 **"非官方客户端自动化"**。

### Provider Category

| 类型                  | 示例                 |
| ------------------- | ------------------ |
| Official API        | Telegram / Discord |
| Webhook API         | 飞书 / 企业微信          |
| SMTP                | 邮件                 |
| Client Automation   | 微信 Hook            |
| GUI Automation      | QQ UI              |
| Browser Automation  | WhatsApp Web       |

### 微信 Provider 的定位

微信不是 **Notification API**，而是 **Client Runtime Control**。

这是完全不同层级的概念。

### 抽象升级

Herald 不应该只定义 **Provider**，还应该定义 **Runtime Capability**。

```yaml
capabilities:
  - send_message
  - group_message
  - gui_session
  - hook_runtime
```

Herald Core 根本不关心"怎么发"，只知道：
> 某个 Runtime 支持某种 Delivery Capability

## Delivery Method

| 类型                  | 示例         |
| ------------------- | ---------- |
| webhook             | 飞书         |
| bot-api             | Telegram   |
| smtp                | 邮件         |
| client-hook         | 微信         |
| gui-automation      | QQ         |
| browser-automation  | WhatsApp   |

## 系统定位升级

Herald 不是：
- ❌ Plugin System
- ❌ Webhook Notification Tool

Herald 是：
- ✅ **Runtime System**
- ✅ **Event Delivery Runtime System**

## Runtime 类型支持

### Windows GUI Runtime
- 微信
- QQ
- Outlook
- 企业微信客户端

### Browser Runtime
通过 Playwright / CDP 支持：
- WhatsApp Web
- Slack Web
- Discord Web

### Mobile Runtime
- Android Notification Bridge

## Runtime vs Plugin

微信这种 Runtime 本质上更像：

| 系统                   | 类比点      |
| -------------------- | ------- |
| Discord Gateway Bot  | session |
| QQ Bot               | runtime |
| Jenkins Agent        | worker  |
| Game Bot Node        | automation  |
| Browser Automation Node | stateful |

而不是简单的"插件"。

> 它们是 **长期在线 Runtime**，不是临时加载的插件。
