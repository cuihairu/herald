# Bark

通过公开 `api.day.app` 服务或自建的 bark-server，向装有 [Bark](https://github.com/Ashengzxb/Bark) App 的 iPhone（或 iPad）推送通知。

## 能做什么

`bark` 内置 Provider 把 Herald 通知发到 `POST {server}/push`，`server` 默认指向公开 Bark 端点 `https://api.day.app`。一枚 `device_key` 标识目标设备；`title`、`body`、`url` 承载消息，`group`、`ttl`、`sound`、`level_sound` 控制推送的分组、留存与提示音。它适合「告警直接拍到自己手机上」这类场景——零审批、不需要开发者账号。

Bark 成功时返回 `{"code":200}`；其他业务码一律按投递失败上报。

## 获取设备密钥

1. 在 iPhone 的 App Store 安装 **Bark**。
2. 打开 App，首页会展示一条示例推送链接，形如 `https://api.day.app/AbCdEf1234567890/ping?title=...`。
3. `https://api.day.app/` 后面那一路径段（示例中的 `AbCdEf1234567890`）就是 **device key**，即配置里的 `device_key`。
4. 先把 App 里那条示例推送发一次，确认设备可达，再把密钥填进 Herald 配置。

想自部署 [bark-server](https://github.com/Ashengzxb/bark-server)？把 `server` 指向它，密钥则取自该服务器的设备列表（而不是 `api.day.app`）。

## 发送第一条消息

配好 Provider（见下），启动 Herald，然后：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "demo",
    "level": "error",
    "title": "Herald 首次推送",
    "body": "经 Bark 投递",
    "channels": ["bark"]
  }'
```

接口返回 `"accepted":["bark"]`，几秒后推送落到手机上。没收到？先查 `device_key`，再看[排错指南](/zh/guide/troubleshooting)。

## 配置项

| 配置项 | 必填 | 说明 | 默认值 |
|-----|----------|-------------|---------|
| `device_key` | 是 | 目标 iPhone 的 Bark 设备密钥 | — |
| `server` | 否 | Bark 服务器地址；指向自建的 bark-server 时改写此项 | `https://api.day.app` |
| `group` | 否 | Bark App 内显示的分组名 | 空（走 Bark 默认分组） |
| `ttl` | 否 | 消息留存时长（秒），原样转给 Bark | `0`（由 Bark 决定） |
| `sound` | 否 | 提示音名称 | 空（系统默认） |
| `level_sound` | 否 | 通知级别（`error` / `warning` / `info` / …）到提示音的映射；任务级别命中时优先于 `sound` | 未设置 |
| `url` | 否 | 用户点击推送时打开的链接 | 空 |

Bark 接口还有 `icon` 字段；它属于载荷面，但目前没有配置映射，Herald 从不填这个字段。

## 配置示例

```yaml
providers:
  bark:
    type: bark
    enabled: true
    config:
      device_key: "$BARK_DEVICE_KEY"
      # 可选 —— 默认走公开服务
      # server: "https://api.day.app"
      group: "herald"
      ttl: 600
      sound: "minuet"
      level_sound:
        error: "alarm"
        warning: "bell"
      url: "https://herald.example.com"
```

## 环境变量

- `BARK_DEVICE_KEY` - 设备密钥（`device_key`）

## 消息模板与限制

- `title`、`body` 直接取通知内容，与其他文本渠道一致；Provider 不做渠道专属截断。
- 提示音取值：任务级别在 `level_sound` 里有配置时用它，否则回落 `sound`。
- `ttl` 以秒为单位转发；省略或传 `0` 时由 Bark 用自身默认留存。

## 常见错误

| 错误 | 原因与处理 |
|-------|---------------|
| `bark: device_key is required` | `config.device_key` 为空或未设置。填 `device_key`，或导出 `$BARK_DEVICE_KEY` 后写成 `"$BARK_DEVICE_KEY"`。 |
| `bark API error: code <n> <message>` | Bark 返回了 HTTP 200 但业务码不是 `200`，推送被 Bark 服务器拒绝。最常见是 `device_key` 填错或已失效——重新从 Bark App 复制密钥，并确认它属于你配置的 `server`。 |
| 接口说已受理但推送没到 | 确认手机端 Bark App 已安装并在线，且 `server` 指向的就是签发该密钥的服务（公开密钥配自建服务器、或反之，都会静默失败）。 |

## 下一步

- [Provider 概览](./overview.md) - 完整渠道表与错误/重试分类
- [APNs](./apns.md) - 向运行自己 App 的设备推送（需 Apple Developer 账号）
- [JPush](./jpush.md) / [Getui](./getui.md) - 国内厂商推送
- [排错指南](/zh/guide/troubleshooting) - 投递失败与日志查看