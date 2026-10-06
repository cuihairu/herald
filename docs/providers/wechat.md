# 微信个人推送

通过第三方推送服务把消息送到**个人微信**，无需企业资质、无需公众号，适合自用告警与个人事务提醒。

## 作用

`wechat` Builtin Provider 复用了三个第三方服务的 HTTP API，由 `service` 选择后端：

| `service` | 服务 | 官方接口 |
|-----------|------|----------|
| `serverchan`（默认） | [Server酱](https://sct.ftqq.com/) | `POST https://sctapi.ftqq.com/{send_key}.send` |
| `pushplus` | [PushPlus](http://www.pushplus.plus/) | `POST https://www.pushplus.plus/send` |
| `wxpusher` | [WxPusher](https://wxpusher.zjiecode.com/) | `POST https://wxpusher.zjiecode.com/api/send/message` |

三个服务都是「推到我的微信」而非「推给某个用户」，因此 `recipients`（投递目标）对本 Provider **不生效**——消息统一发给该凭据对应的账号或其全部关注者。要按人定向推送请用 [FCM](./fcm.md) / [APNs](./apns.md) / [微信公众号模板消息](./wechatmp.md)。

## 申请凭据

前提：任选一家第三方服务注册账号（都免费，都需微信扫码授权）。

### Server酱（默认）

1. 访问 [sct.ftqq.com](https://sct.ftqq.com/) 微信扫码登录
2. 右上角「消息推送」→ **SendKey** 页面，复制形如 `SCT1234567890abcdef` 的 SendKey
3. 免费额度 5 条/天，超额后需升级会员或等次日重置

### PushPlus

1. 访问 [pushplus.plus](http://www.pushplus.plus/) 微信扫码登录
2. 左侧「令牌」页，复制 Token（形如 `a1b2c3...`）
3. 免费额度 200 条/天；可在「消息通道」绑定微信、邮箱、公众号等，消息会同时投递过去

### WxPusher

1. 访问 [wxpusher.zjiecode.com](https://wxpusher.zjiecode.com/) 微信扫码登录
2. 左侧「应用管理」→ 新建应用，填名称与描述 → 创建后得到 **AppToken**（形如 `AT_xxxxxxxx`）
3. 关注应用后可在「用户列表」看到自己的 **UID**；不配 UID 则推给全部关注者
4. 免费额度 1000 条/天

## 发第一条消息

按下面配好并 `heraldd serve --config config.yaml` 启动后：

```yaml
# config.yaml 追加（Server酱；SendKey 从上面的申请步骤取）
providers:
  wechat:
    type: wechat
    enabled: true
    config:
      service: serverchan        # 可省略，默认 serverchan
      send_key: "$WECHAT_SERVERCHAN_SEND_KEY"
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "alert.disk",
    "level": "warning",
    "title": "磁盘告警",
    "body": "/dev/sda1 已用 95%",
    "channels": ["wechat"]
  }'
```

微信收到「🟡 磁盘告警 / /dev/sda1 已用 95%」，末尾附发送时间。没收到？先确认凭据对不对、服务选对没有，再查[排错指南](/guide/troubleshooting)与下面的[常见错误](#常见错误)。

> 注意配置键名随 `service` 而变：Server酱 用 `send_key`，PushPlus 用 `token`，WxPusher 用 `app_token`。写错键名会在启动时直接报 `... is required`，不会静默失败。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `service` | ❌ | 后端服务：`serverchan` / `pushplus` / `wxpusher` | `serverchan` |
| `send_key` | 条件必填 | Server酱 SendKey（`service: serverchan` 时必填） | 无 |
| `token` | 条件必填 | PushPlus Token（`service: pushplus` 时必填） | 无 |
| `app_token` | 条件必填 | WxPusher AppToken（`service: wxpusher` 时必填） | 无 |
| `uid` | ❌ | WxPusher 定向 UID；留空推给全部关注者，多个用英文逗号分隔 | 无 |

值里不含 `service` 时默认走 Server酱——**只填 `send_key` 之外的服务凭据而忘了写 `service`，会以 `send_key is required for serverchan` 启动失败**。

## 配置示例

三个后端各一份，按需二选一：

```yaml
providers:
  # Server酱
  wechat:
    type: wechat
    enabled: true
    config:
      service: serverchan
      send_key: "$WECHAT_SERVERCHAN_SEND_KEY"
```

```yaml
providers:
  # PushPlus
  wechat:
    type: wechat
    enabled: true
    config:
      service: pushplus
      token: "$WECHAT_PUSHPLUS_TOKEN"
```

```yaml
providers:
  # WxPusher（可选定向到自己的 UID）
  wechat:
    type: wechat
    enabled: true
    config:
      service: wxpusher
      app_token: "$WECHAT_WXPUSHER_APP_TOKEN"
      uid: "$WECHAT_WXPUSHER_UID"   # 可留空，留空则推给全部关注者
```

高可用可以配多个 `wechat` 实例（不同 provider 名）指向不同服务，某家限流或宕机时换道：

```yaml
providers:
  wechat-sc:
    type: wechat
    enabled: true
    config:
      service: serverchan
      send_key: "$WECHAT_SERVERCHAN_SEND_KEY"
  wechat-wx:
    type: wechat
    enabled: true
    config:
      service: wxpusher
      app_token: "$WECHAT_WXPUSHER_APP_TOKEN"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `WECHAT_SERVERCHAN_SEND_KEY` | `send_key` | Server酱 SendKey |
| `WECHAT_PUSHPLUS_TOKEN` | `token` | PushPlus Token |
| `WECHAT_WXPUSHER_APP_TOKEN` | `app_token` | WxPusher AppToken |
| `WECHAT_WXPUSHER_UID` | `uid` | WxPusher UID（可留空） |

## 消息模板与限制

### 内容映射

| 字段 | Server酱 | PushPlus | WxPusher |
|------|-----------|----------|----------|
| 标题 | `title` | `title` | `summary`（微信通知栏标题） |
| 正文 | `desp`（Markdown） | `content`（模板默认 HTML） | `content`，`contentType=3`（Markdown） |
| 摘要 | `short`，取正文前 64 个字符 | — | — |
| 定向 | — | — | `uids`，配置了 `uid` 才带 |

消息正文由 Provider 组装：**级别图标 + body + 发送时间**，所以 curl 里只需给 `title`/`body`：

| `level` | 前置图标 |
|---------|----------|
| `critical` / `error` | 🔴 |
| `warning` | 🟡 |
| `info` | 🟢 |
| `debug` | ⚪ |

想发富文本（加粗、列表）就直接在 `body` 里写 Markdown：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "deploy.done",
    "level": "info",
    "title": "部署成功",
    "body": "**项目**: Herald\n**版本**: v1.0.0\n**状态**: ✅ 成功",
    "channels": ["wechat"]
  }'
```

### 发送语义

- 一次投递只发一条消息，目标固定为凭据对应账号/关注者，`recipients` 不参与
- 正文末尾自动追加 `---\n2026-10-03 10:20:30` 形式的时间戳，方便回看
- Server酱 `short` 摘要按 **64 个字符**截断；正文完整保留

### 能力声明

- `PayloadKinds`：`Content`（透传类载荷请改用 FCM/APNs/WxPusher 之外的方案）
- `ContentFormats`：`plain`

### 重试语义

- 第三方返回的业务错误码（Server酱 `code != 0`、PushPlus `code != 200`、WxPusher `code` 非 `1000`/`0`）是**确定性错误**，不重试——重发只会再撞一次额度或限流
- 请求超时、HTTP 408/429/5xx 由 `httpclient` 包装为可重试错误，走统一重试与退避

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `wechat: unsupported service 'xxx', use: serverchan, pushplus, or wxpusher` | `service` 拼错或留了空值以外的非法值 |
| `wechat: send_key is required for serverchan` | 走了 Server酱 但没配 `send_key`（常见于配了 PushPlus/WxPusher 却漏写 `service`） |
| `wechat: token is required for pushplus` | 走了 PushPlus 但没配 `token` |
| `wechat: app_token is required for wxpusher` | 走了 WxPusher 但没配 `app_token` |
| `wechat: serverchan error: <message>` | Server酱 侧拒绝：`SendKey` 不正确、额度用尽、消息含敏感词 |
| `wechat: pushplus error: <msg>` | PushPlus 侧拒绝：`Token` 失效、额度用尽、`IP` 未加入白名单（控制台「账号设置」可配） |
| `wechat: wxpusher error: <msg>` | WxPusher 侧拒绝：`AppToken` 写错、没关注应用、额度用尽 |
| `wechat: failed to parse response: ...` | 服务返回体不是预期 JSON——通常是代理/网关返回的 HTML 错误页 |
| `wechat: task is nil` | 内部调用传了空任务，不会出现在正常投递路径 |

三家免费额度都很小（5 / 200 / 1000 条每天）。额度型错误**不会重试**，需要减少告警噪音或改用带免费额度的通道（[Webhook](./webhook.md) + 自建、[飞书](./feishu.md)、[企业微信](./wecom.md) 群机器人均无条数限制）。整体排错见[排错指南](/guide/troubleshooting)。

## 安全建议

1. **凭据走环境变量**：SendKey / Token / AppToken 等同账号权限，`.env` 或密管系统注入，别写进仓库的 config
2. **Server酱 SendKey 可公开**：它出现在推送 URL 里，日志/截图外发前留意
3. **换服务即换凭据**：三家凭据互不通用，别把 PushPlus Token 填进 `send_key`
4. **个人通道发敏感内容前**：第三方服务内容会经过其服务器，注意合规

## 下一步

- [微信公众号模板消息](./wechatmp.md) - 需要按用户定向时改走公众号
- [FCM](./fcm.md) / [APNs](./apns.md) - App 定向推送
- [Webhook](./webhook.md) - 无条数限制的自建通道
- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式