# Providers

Herald 支持多种通知渠道，包括即时通讯、邮件、短信、推送和 Webhook。

## 按任务找渠道

| 你想做什么 | 用哪个 | 申请难度 |
|-----------|--------|---------|
| 消息发进飞书/企微/钉钉群 | [`feishu`](./feishu.md) / [`wecom`](./wecom.md) / [`dingtalk`](./dingtalk.md) | 群里加机器人即得，**零审批** |
| 推送到自己手机 | [`telegram`](./telegram.md) 或 [`wechat`](./wechat.md)（Server酱等） | Telegram 零审批；微信第三方扫码即用 |
| 发验证码 / 事务短信 | [`aliyunsms`](./aliyunsms.md) / [`tencentsms`](./tencentsms.md) / [`neteasesms`](./neteasesms.md) | 需签名+模板审核 |
| 发邮件 | [`email`](./email.md) | 有 SMTP 账号即可（授权码） |
| 推到 Discord / Slack 频道 | [`discord`](./discord.md) / [`slack`](./slack.md) | 创建 Webhook 即得，零审批 |
| 对接自建系统 / 本地调试 | [`webhook`](./webhook.md) / [`log`](./log.md) | 零凭据 |
| 公众号模板消息 | [`wechatmp`](./wechatmp.md) | 需认证服务号 |
| App 推送（Android/Web） | [`fcm`](./fcm.md) | 需 Firebase 项目，控制台生成服务账号即用 |
| App 推送（iOS/macOS） | [`apns`](./apns.md) | 需 Apple Developer 账号，Keys 页生成 .p8 |
| App 推送（国内，Android/iOS/鸿蒙） | [`jpush`](./jpush.md) / [`getui`](./getui.md) | 需极光/个推账号，控制台建应用即得 AppKey/Master Secret |

每个渠道页都含：**申请凭据 → 配置 → 发第一条消息（可跟跑的命令）→ 常见错误**。

## Provider 分类

### 即时通讯

| Provider | 说明 | 状态 |
|----------|------|------|
| [`telegram`](./telegram.md) | Telegram Bot | ✅ |
| [`feishu`](./feishu.md) | 飞书机器人 | ✅ |
| [`wecom`](./wecom.md) | 企业微信机器人 | ✅ |
| [`wechat`](./wechat.md) | 微信个人推送 (ServerChan/PushPlus/WxPusher) | ✅ |
| [`wechatmp`](./wechatmp.md) | 微信公众号模板消息 | ✅ |
| [`dingtalk`](./dingtalk.md) | 钉钉机器人 | ✅ |
| [`slack`](./slack.md) | Slack | ✅ |
| [`discord`](./discord.md) | Discord | ✅ |

### 邮件

| Provider | 说明 | 状态 |
|----------|------|------|
| [`email`](./email.md) | SMTP 邮件 | ✅ |

### 短信

| Provider | 说明 | 状态 |
|----------|------|------|
| [`aliyunsms`](./aliyunsms.md) | 阿里云短信 | ✅ |
| [`tencentsms`](./tencentsms.md) | 腾讯云短信 | ✅ |
| [`neteasesms`](./neteasesms.md) | 网易云信短信 | ✅ |

### 推送

| Provider | 说明 | 状态 |
|----------|------|------|
| [`fcm`](./fcm.md) | Firebase Cloud Messaging (Android/Web) | ✅ |
| [`apns`](./apns.md) | Apple Push Notification service (iOS/macOS) | ✅ |
| [`jpush`](./jpush.md) | 极光推送 JPush (Android/iOS/鸿蒙) | ✅ |
| [`getui`](./getui.md) | 个推 Getui (Android/iOS/鸿蒙) | ✅ |

### 其他

| Provider | 说明 | 状态 |
|----------|------|------|
| [`webhook`](./webhook.md) | 通用 Webhook | ✅ |
| [`log`](./log.md) | 日志输出 | ✅ |

## Builtin vs Worker

### Builtin Providers

Builtin Providers 直接在 Herald 核心进程中运行，配置简单：

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "your_token"
      chat_id: "your_chat_id"
```

### Worker Providers

Worker Providers 在独立的进程中运行，通过 WebSocket 连接到 Herald：

```yaml
providers:
  custom-provider:
    type: worker
    config:
      target: "custom-worker-01"   # 等价 name 键
```

## 启用/禁用 Provider

### 通过配置文件

```yaml
providers:
  telegram:
    type: telegram
    enabled: true    # 启用
    config:
      token: "your_token"

  slack:
    type: slack
    enabled: false   # 禁用
    config:
      webhook_url: "your_url"
```

### 通过 API

```bash
# 启用
curl -X POST http://localhost:8080/api/v1/providers/telegram/enable

# 禁用
curl -X POST http://localhost:8080/api/v1/providers/telegram/disable
```

### 通过 Dashboard

在 Dashboard 的 Providers 页面中，点击每个 Provider 卡片的启用/禁用按钮。

## 配置优先级

1. API 调用（运行时修改）
2. 配置文件（启动时加载）
3. 默认配置

配置文件中的 `enabled` 字段指定 Provider 的初始状态，之后可以通过 API 或 Dashboard 动态修改。

## 错误分类与重试

投递失败是否重试由**错误分类**决定（六类词汇，见 [Audience 领域模型](/design-audience-model)）：

| 分类 | 触发 | 是否重试 |
|------|------|---------|
| `temporary` | 上游 5xx、一般网络错误 | ✅ 按 `retry` 配置退避重投 |
| `rate_limited` | 上游 429 | ✅ 同上 |
| `timeout` | 请求超时（408 或传输超时） | ✅ 同上 |
| `permanent` | 上游明确拒绝且重试无意义 | ❌ 立即 failed |
| `authentication` | 凭据无效 | ❌ 立即 failed |
| `invalid_request` | 请求本身不合法 | ❌ 立即 failed |

- 走内置 HTTP 客户端（`core/httpclient`）的 Provider 自动获得分类：408 归 `timeout`、429 归 `rate_limited`、5xx 与传输错误归 `temporary`（三类可重试），其余 4xx 不分类也重试不了（`classifyStatus` 注释原话：retrying them cannot succeed）
- 「HTTP 200 但业务码失败」不在自动分类范围内，由 Provider 自己决定：腾讯云/网易云信给业务错误整体套 `httpclient.WithRetry`（业务码失败也重试），阿里云业务码只返回裸错误（不重试）——各渠道差异见对应 Provider 页
- 可重试类耗尽 `retry.max` 后任务进入 dead，终态错误留在任务的 `last_error`；日志（`/api/v1/logs`）的 `status` 始终是 `success/failed` 二值 wire 词汇

## 最佳实践

1. **敏感信息**：使用环境变量存储 API 密钥
2. **多服务商**：配置多个同类型 Provider 提高可用性
3. **按需启用**：根据环境启用不同的 Provider
4. **监控状态**：定期检查 Provider 健康状态

## 注册机制与 worker 占位

- 所有 Builtin Provider 的工厂统一在 `providers/builtin/registry` 注册（`RegisterBuiltinProviders`），上表即注册全集，无需手工注册
- `worker` 类型的 provider 是**远程 Worker 的本地占位**：本地 Deliver 为空操作，任务由调度器按 `target` 路由到对应 Worker 节点执行（`name` 默认 `worker`，`target` 默认取 `name`）。适用场景与开发方式见 [Worker Runtime](/runtime/worker) 与 [Worker SDK](/runtime/sdk)

## 下一步

- [微信个人推送](./wechat.md) - 使用第三方服务推送
- [微信公众号指南](./wechat-official.md) - 自建公众号推送指南
- [微信公众号模板消息](./wechatmp.md) - wechatmp Provider 配置参考
- [Email](./email.md) - SMTP 邮件通道
- [Webhook](./webhook.md) - 自定义 HTTP 接收端
- [Log](./log.md) - 本地调试输出
- [SMS Providers](./sms.md) - 短信 Provider 详细配置
