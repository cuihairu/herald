# Providers

Herald 支持多种通知渠道，包括即时通讯、邮件、短信和 Webhook。

## Provider 分类

### 即时通讯

| Provider | 说明 | 状态 |
|----------|------|------|
| `telegram` | Telegram Bot | ✅ |
| `feishu` | 飞书机器人 | ✅ |
| `wecom` | 企业微信机器人 | ✅ |
| `dingtalk` | 钉钉机器人 | ✅ |
| `slack` | Slack | ✅ |
| `discord` | Discord | ✅ |

### 邮件

| Provider | 说明 | 状态 |
|----------|------|------|
| `email` | SMTP 邮件 | ✅ |

### 短信

| Provider | 说明 | 状态 |
|----------|------|------|
| `aliyunsms` | 阿里云短信 | ✅ |
| `tencentsms` | 腾讯云短信 | ✅ |
| `neteasesms` | 网易云信短信 | ✅ |

### 其他

| Provider | 说明 | 状态 |
|----------|------|------|
| `webhook` | 通用 Webhook | ✅ |
| `log` | 日志输出 | ✅ |

## Builtin vs Worker

### Builtin Providers

Builtin Providers 直接在 Herald 核心进程中运行，配置简单：

```yaml
providers:
  telegram:
    type: builtin
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
      worker_id: "custom-worker-01"
```

## 启用/禁用 Provider

### 通过配置文件

```yaml
providers:
  telegram:
    type: builtin
    enabled: true    # 启用
    config:
      token: "your_token"

  slack:
    type: builtin
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

## 最佳实践

1. **敏感信息**：使用环境变量存储 API 密钥
2. **多服务商**：配置多个同类型 Provider 提高可用性
3. **按需启用**：根据环境启用不同的 Provider
4. **监控状态**：定期检查 Provider 健康状态

## 下一步

- [SMS Providers](./sms.md) - 短信 Provider 详细配置
