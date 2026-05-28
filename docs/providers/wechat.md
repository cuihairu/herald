# 微信个人推送

Herald 支持通过第三方服务发送消息到个人微信。

## 支持的服务

| 服务 | 说明 | 官网 |
|------|------|------|
| **Server酱** | 简单易用的推送服务 | https://sct.ftqq.com/ |
| **PushPlus** | 功能丰富的推送服务 | http://www.pushplus.plus/ |
| **WxPusher** | 支持多 UID 的推送服务 | https://wxpusher.zjiecode.com/ |

## 配置

### Server酱

1. 访问 https://sct.ftqq.com/ 登录并获取 SendKey
2. 配置 Herald：

```yaml
providers:
  wechat:
    type: wechat
    enabled: true
    config:
      sendkey: "${WECHAT_SENDKEY}"
```

### PushPlus

1. 访问 http://www.pushplus.plus/ 登录并获取 Token
2. 配置 Herald：

```yaml
providers:
  wechat:
    type: wechat
    enabled: true
    config:
      sendkey: "${WECHAT_PUSHPLUS_TOKEN}"
```

### WxPusher

1. 访问 https://wxpusher.zjiecode.com/ 创建应用并获取 AppToken
2. （可选）获取 UID 进行定向推送
3. 配置 Herald：

```yaml
providers:
  wechat:
    type: wechat
    enabled: true
    config:
      sendkey: "${WECHAT_WXPUSHER_APP_TOKEN}"
      uid: "${WECHAT_WXPUSHER_UID}"  # 可选，不填则发送给所有订阅者
```

## 使用示例

### 发送简单通知

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "alert",
    "title": "服务器告警",
    "body": "CPU 使用率超过 90%",
    "level": "warning",
    "channels": ["wechat"]
  }'
```

### 发送 Markdown 格式消息

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "deploy",
    "title": "部署成功",
    "body": "**项目**: Herald\n**版本**: v1.0.0\n**状态**: ✅ 成功",
    "level": "info",
    "channels": ["wechat"]
  }'
```

## 级别图标

Herald 会根据消息级别自动添加 emoji 图标：

| 级别 | 图标 |
|------|------|
| `critical` / `error` | 🔴 |
| `warning` | 🟡 |
| `info` | 🟢 |
| `debug` | ⚪ |

## 服务对比

| 特性 | Server酱 | PushPlus | WxPusher |
|------|----------|----------|----------|
| 免费额度 | 5条/天 | 200条/天 | 1000条/天 |
| 消息模板 | Markdown | HTML/Markdown | Markdown |
| 多UID支持 | ❌ | ❌ | ✅ |
| 通道变量 | ✅ | ✅ | ❌ |
| 定时发送 | ❌ | ✅ | ❌ |

## 注意事项

1. **每日限额**：各服务有免费发送限额，超限后需要等待或付费
2. **消息频率**：建议合理控制发送频率，避免被限流
3. **敏感词**：部分服务对敏感词有过滤，请避免使用违规内容
4. **Token 安全**：请妥善保管 Token/Key，避免泄露

## 最佳实践

1. **多服务配置**：可配置多个 wechat provider 提高可用性
2. **按环境使用**：开发环境用 Server酱，生产环境用 PushPlus/WxPusher
3. **消息模板**：使用 Markdown 格式使消息更易读

## 下一步

- [Provider 概览](./overview.md) - 查看所有支持的 Provider
