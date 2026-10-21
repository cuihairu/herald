# APNs (Apple Push Notification service)

向 iOS / iPadOS / macOS / watchOS 设备发送推送通知。

## 作用

`apns` Builtin Provider 直接调用 APNs（`POST https://api.push.apple.com/3/device/{token}`）。支持两种认证：

- **Token 认证（推荐）**：用 `.p8` 私钥签 ES256 JWT（`key_id` + `team_id`），缓存一小时、过期前 5 分钟自动重签；一个密钥可同时用于开发与生产环境
- **证书认证**：TLS 客户端证书（PEM 证书链 + 私钥），兼容老的 .p12 流程

每个投递目标是一个 **APNs 设备令牌（device token）**。消息分 `aps` 段（alert 通知 / data 静默推送）与自定义数据键，由通知类型自动推导 `apns-push-type` 与 `apns-priority`。

## 申请凭据

前提：**Apple Developer Program 付费账号**（$99/年，个人或公司均可）。

### Token 认证（推荐，四步）

1. **记下 Team ID**：[Account](https://developer.apple.com/account) 页面 Membership Information 里的 **Team ID**（形如 `ABCDE12345`）
2. **创建 APNs 密钥**：[Certificates, Identifiers & Profiles → Keys](https://developer.apple.com/account/resources/authkeys/list) → **+** → 勾选 **Apple Push Notifications service (APNs)** → 注册后**立即下载 `.p8` 文件**（形如 `AuthKey_XXXXXXXXXX.p8`）——**只能下载一次**，丢失只能重建
3. **记下 Key ID**：下载页与密钥列表里的 10 位 **Key ID**
4. **注册 App ID（Bundle ID）**：[Identifiers → App IDs](https://developer.apple.com/account/resources/identifiers/list/bundleId) → 新建，形如 `com.example.app`——这就是配置里的 `topic`

### 证书认证（备选）

1. Identifiers → App IDs → 勾选 **Push Notifications** 能力
2. Certificates → 创建 **APNs SSL** 证书（开发/生产各一张），本地 Keychain 导出 .p12
3. 转成 PEM（证书链 + 私钥在一个文件）：

```bash
openssl pkcs12 -in cert.p12 -out apns_cert.pem -nodes -legacy
```

### 设备 token 从哪来

App 集成 APNs 后在 `didRegisterForRemoteNotificationsWithDeviceToken` 回调里拿到（iOS 13+ 为 64+ 位十六进制字符串），由你的业务服务端存进花名册/用户表，投递时作为收件人传入。Herald 不负责从 APNs 获取 token。

## 发第一条消息

按下面配好并 `heraldd serve --config config.yaml` 启动后（**必须带 device token 收件人**）：

```yaml
# config.yaml 追加（token 认证三要素从上面的申请步骤取）
providers:
  apns:
    type: apns
    enabled: true
    config:
      key_id: "ABC123DEFG"
      team_id: "ABCDE12345"
      private_key: "$APNS_PRIVATE_KEY"   # .p8 的 PKCS#8 PEM 全文，见环境变量
      topic: "com.example.app"
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order.notice",
    "level": "info",
    "title": "订单已发货",
    "body": "您的订单 SF123456 已发出",
    "channels": ["apns"],
    "recipients": {"apns": ["device-token-十六进制字符串"]}
  }'
```

手机通知栏收到「订单已发货」。没收到？检查 token 是否有效、App 是否处于可推送状态，查[排错指南](/guide/troubleshooting)与下面的[常见错误](#常见错误)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `topic` | ✅ | App Bundle ID（VoIP 推送加 `.voip` 后缀等场景按 APNs 规则加后缀） | 无 |
| `key_id` | 二选一 | APNs Key ID（Token 认证） | 无 |
| `team_id` | 二选一 | Apple Team ID（Token 认证） | 无 |
| `private_key` | 二选一 | `.p8` 私钥，PKCS#8 PEM 全文（Token 认证） | 无 |
| `cert_pem` | 二选一 | 证书链 + 私钥 PEM 全文（证书认证） | 无 |
| `push_type` | ❌ | 覆盖自动推导的 `apns-push-type`（`voip`/`liveactivity`/`location` 等） | 按内容推导 |
| `priority` | ❌ | 覆盖自动推导的 `apns-priority`（10=立即，5=省电） | 按类型推导 |
| `endpoint` | ❌ | 端点；沙盒用 `https://api.sandbox.push.apple.com` | `https://api.push.apple.com` |

Token 认证缺 `key_id`/`team_id`/`private_key` 任一、证书缺 `cert_pem`、或缺 `topic` 时，Provider 创建即失败（`apns: key_id is required` 等），启动日志可见。私钥必须是 **P-256** 曲线（`.p8` 即此格式），错误曲线会报 `apns: private_key must be a P-256 EC key`。

## 配置示例

```yaml
providers:
  apns:
    type: apns
    enabled: true
    config:
      key_id: "ABC123DEFG"
      team_id: "ABCDE12345"
      private_key: "$APNS_PRIVATE_KEY"
      topic: "com.example.app"
      # endpoint: "https://api.sandbox.push.apple.com"  # 开发环境沙盒
```

证书认证（二选一，替换 key_id/team_id/private_key）：

```yaml
    config:
      topic: "com.example.app"
      cert_pem: "$APNS_CERT_PEM"   # openssl 转出的证书链 + 私钥 PEM
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `APNS_KEY_ID` | `key_id` | APNs Key ID（`.env.example` 惯用名） |
| `APNS_TEAM_ID` | `team_id` | Apple Team ID |
| `APNS_PRIVATE_KEY` | `private_key` | `.p8` 的 PKCS#8 PEM 全文（含真实换行） |
| `APNS_CERT_PEM` | `cert_pem` | 证书链 + 私钥 PEM 全文（证书认证） |

> ⚠️ 私钥里的换行必须是**真实换行符**。从文件复制时保留 `-----BEGIN PRIVATE KEY-----` 头尾行与内部换行，或整段放入环境变量。

## 消息模板与限制

### 内容映射

| 场景 | push-type | priority | body 结构 |
|------|-----------|----------|-----------|
| 带 title/body 的通知 | `alert` | 10（立即） | `aps.alert` + `aps.sound=default` |
| 纯 data 载荷 | `background` | 5（省电） | `aps.content-available=1` + 顶层自定义键 |
| 配置 `push_type`/`priority` | 显式覆盖 | 显式覆盖 | 同上 |

- Raw 载荷的键放在 JSON **顶层**（`aps` 之外）；重复的 `aps` 键会被忽略，防止覆盖计算好的段
- Content 与 Raw 至少其一，否则报 `apns: payload needs content or raw data`

### 发送语义

- 收件人逐 device token 发送；部分失败聚合为 `apns: N/M succeeded - failed: ...`（与 FCM/公众号一致）
- Token 认证的 JWT 内存缓存，过期前 5 分钟重签
- 单消息上限 4KB（APNs 平台限制）

### 能力声明

- `PayloadKinds`: `Content`、`Raw`
- `ContentFormats`: `plain`

### 重试语义

- 请求超时、408/429/5xx 由 `httpclient` 包装为可重试错误，走统一重试
- 400（`BadDeviceToken` 等）、410（`Unregistered`）为确定性错误，不重试

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `apns: topic is required` | 没配 Bundle ID |
| `apns: key_id/team_id/private_key is required` | Token 认证缺要素（且没配 `cert_pem`） |
| `apns: private_key is not valid PEM data` | 私钥不是 PEM（复制丢了头尾行） |
| `apns: private_key must be an EC key` | 拿错了密钥（非 EC） |
| `apns: private_key must be a P-256 EC key` | 非 P-256 曲线，APNs 只接受 P-256 |
| `apns: cert_pem has no CERTIFICATE block` | 证书认证的 PEM 里没有证书段 |
| `apns: cert_pem: ...` | 证书与私钥不匹配（X509KeyPair 校验失败） |
| `{"reason":"BadDeviceToken"}`（400） | device token 无效/来自沙盒但打生产端点（或反之），核对 `endpoint` 与 token 来源 |
| `{"reason":"TopicDisallowed"}`（400） | `topic` 与证书/JWT 对应的 App ID 不一致 |
| `{"reason":"Unregistered"}`（410） | token 已失效（App 卸载/token 轮换），从收件人中剔除 |
| `{"reason":"ExpiredProviderToken"}`（403） | 系统时钟偏差过大导致 JWT 过期——校准服务器时间 |
| `{"reason":"InvalidProviderToken"}`（403） | `.p8` 私钥与 `key_id` 不匹配，或 Key 被撤销 |
| `{"reason":"TooManyRequests"}`（429） | 频率超限，走统一重试 |

错误格式：HTTP 非 2xx 返回 `unexpected status code: {code}, body: {APNs JSON}`（body 内含 `reason`）。整体排错见[排错指南](/guide/troubleshooting)。

## 安全建议

1. **`.p8` 只能下载一次**：存进密管系统；丢失需在 Keys 页面撤销并重建（Key ID 会变）
2. **最小授权**：创建 Key 时只勾 APNs 一项
3. **证书认证注意**：`.p12`/PEM 含私钥，等同凭据；开发与生产证书区分环境
4. **失效 token 清理**：410 `Unregistered` 表示 token 已注销，持续重投无意义，应从受众中移除

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [FCM](./fcm.md) - Android/Web 推送通道
- [排错指南](/guide/troubleshooting) - 启动失败、消息没到、重试不生效
