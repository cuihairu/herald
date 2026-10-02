# JPush (极光推送)

向 Android / iOS / 鸿蒙设备发送推送通知，国内厂商通道到达率优化。

## 作用

`jpush` Builtin Provider 直接调用极光推送 REST API v3（`POST https://api.jpush.cn/v3/push`，见[官方文档](https://docs.jiguang.cn/jpush/server/push/rest_api_v3_push)），鉴权为 HTTP Basic：`base64(app_key:master_secret)`。

每个投递目标是一个 **registration_id**（极光 SDK 为每台设备生成的唯一标识），按设备逐次发送，一条消息两种承载：

- Content（title/body）→ `notification` 通知段，进系统通知栏（Android 带标题行，iOS 默认响铃）
- Raw 载荷 → `message` 透传段，不上通知栏，由 App 内集成的极光 SDK 回调处理
- `options.apns_production` 控制 iOS 走生产还是开发 APNs 环境

## 申请凭据

前提：一个[极光账号](https://www.jiguang.cn/)（注册免费）。

1. **创建应用**：[极光控制台](https://www.jiguang.cn/) → 创建应用（Android 填包名，iOS 填 Bundle ID）
2. **记下 AppKey**：应用列表进入该应用 → **应用设置** 页的 **AppKey**（形如 `7d431e42dfa6a6d693ac2d04`）
3. **记下 Master Secret**：同页的 **Master Secret**（只在服务端使用，等同推送全权凭据，可在控制台重置）
4. **设备 registration_id**：App 集成 [JPush SDK](https://docs.jiguang.cn/jpush/client/Android/android_guide) 后，通过 SDK 的 RegistrationID 接口拿到，由你的业务服务端存进花名册/用户表，投递时作为收件人传入。Herald 不负责从极光获取 registration_id。

## 发第一条消息

按下面配好并 `heraldd serve --config config.yaml` 启动后（**必须带 registration_id 收件人**）：

```yaml
# config.yaml 追加（AppKey/Master Secret 从上面的申请步骤取）
providers:
  jpush:
    type: jpush
    enabled: true
    config:
      app_key: "7d431e42dfa6a6d693ac2d04"
      master_secret: "$JPUSH_MASTER_SECRET"   # 见环境变量
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order.notice",
    "level": "info",
    "title": "订单已发货",
    "body": "您的订单 SF123456 已发出",
    "channels": ["jpush"],
    "recipients": {"jpush": ["registration-id-字符串"]}
  }'
```

手机通知栏收到「订单已发货」。没收到？检查 registration_id 是否正确、App 是否集成极光 SDK 且处于可推送状态，查[排错指南](/guide/troubleshooting)与下面的[常见错误](#常见错误)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `app_key` | ✅ | 应用 AppKey（控制台应用设置页） | 无 |
| `master_secret` | ✅ | 应用 Master Secret（控制台应用设置页） | 无 |
| `platform` | ❌ | 推送平台：`all`、单个（`android`）、逗号分隔（`android,ios`），也可写 YAML 列表；官方取值 `android`/`ios`/`quickapp`/`hmos` | `all` |
| `apns_production` | ❌ | iOS 目标走生产（`true`）还是开发（`false`）APNs 环境；调试包装调试包时设 `false` | `true`（极光不指定即生产） |
| `time_to_live` | ❌ | 离线消息保留时长（秒），`0` 表示不保留离线消息 | 极光默认 `86400`（1 天；普通用户最长 3 天、VIP 最长 10 天） |
| `endpoint` | ❌ | API 端点（测试/代理场景覆盖） | `https://api.jpush.cn` |

缺 `app_key`/`master_secret` 时 Provider 创建即失败（`jpush: app_key is required` 等），启动日志可见。

## 配置示例

```yaml
providers:
  jpush:
    type: jpush
    enabled: true
    config:
      app_key: "7d431e42dfa6a6d693ac2d04"
      master_secret: "$JPUSH_MASTER_SECRET"
      platform: "android,ios"
      apns_production: false   # 开发包装调试包时
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `JPUSH_APP_KEY` | `app_key` | 应用 AppKey（`.env.example` 惯用名） |
| `JPUSH_MASTER_SECRET` | `master_secret` | 应用 Master Secret |

## 消息模板与限制

### 内容映射

| 场景 | body 结构 |
|------|-----------|
| 带 title/body 的通知 | `notification.alert`（共享）+ `android{title,alert}` + `ios{alert,sound:default}` |
| 纯 Raw 载荷 | `message{msg_content,extras}`——透传给 App，不上通知栏；`msg_content` 为 Raw 的 JSON |
| Content + Raw 同时有 | `notification` 与 `message` 同发，`msg_content` 取可读正文 |

- `body` 为空时 `alert` 取 `title`（title-only 也能推）
- 只有 `title` 非空才带 `android` 段（JPush 要求 `android.alert` 必填，共享 `alert` 已覆盖所有平台）；`ios` 段固定带 `sound: default`
- Raw 的键进 `extras`，值统一转字符串；`message.msg_content` 是极光必填项，无正文时用 Raw 的 JSON 兜底
- Content 与 Raw 至少其一，否则报 `jpush: payload needs content or raw data`

### 发送语义

- 收件人逐 registration_id 发送；部分失败聚合为 `jpush: N/M succeeded - failed: ...`（与 FCM/APNs 一致），死设备（错误 1011）只算它自己失败
- 极光按 API 调用次数做频控（按 AppKey、分钟级窗口），触发返回 429 走统一重试；相同内容大量投递时注意压低并发

### 能力声明

- `PayloadKinds`: `Content`、`Raw`
- `ContentFormats`: `plain`

### 重试语义

- 请求超时、408/429/5xx 由 `httpclient` 包装为可重试错误，走统一重试
- 400（1011 无目标、1003 参数非法等）、401（1004 验证失败）、403（受限/白名单）为确定性错误，不重试

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `jpush: app_key is required` | 没配 AppKey |
| `jpush: master_secret is required` | 没配 Master Secret |
| `jpush: time_to_live must be >= 0` | 离线保留时长配成了负数 |
| `jpush: payload needs content or raw data` | Content 和 Raw 都为空 |
| `{"error":{"code":1004,...}}`（401） | 验证失败：AppKey 与 Master Secret 不匹配（对照控制台，注意别多空格） |
| `{"error":{"code":1011,...}}`（400） | 没有满足条件的推送目标：registration_id 无效，或设备超 255 天不活跃被排除 |
| `{"error":{"code":1003,...}}`（400） | 参数值不合法：registration_id 有空值或格式错误 |
| `{"error":{"code":1005,...}}`（400） | 消息体太大：Android Notification+Message 合计 4000 字节、iOS `notification.ios{}` 段 3584 字节上限 |
| `{"error":{"code":2002,...}}`（429） | API 调用频率超限，走统一重试（把请求摊匀到时间窗口） |
| `{"error":{"code":2003,...}}`（403） | appKey 被限制调用 API，联系极光技术支持 |
| `{"error":{"code":2004,...}}`（403） | 源 IP 不在应用的 IP 白名单 |
| `{"error":{"code":2010,...}}`（400） | 内容含黑词或推送总量超限 |

错误格式：HTTP 非 2xx 返回 `unexpected status code: {code}, body: {"error":{"code":..,"message":..}}`。整体排错见[排错指南](/guide/troubleshooting)。

## 安全建议

1. **Master Secret 等同推送全权凭据**：只放服务端环境变量/密管系统，不进客户端与代码仓库；怀疑泄露立即在控制台重置
2. **registration_id 是设备标识**：按个人数据对待，落库、传输注意合规
3. **开发/生产区分**：开发包装调试包用 `apns_production: false`，上线改回 `true`（默认）

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [FCM](./fcm.md) - Android/Web 推送通道
- [APNs](./apns.md) - iOS/macOS 推送通道
- [Getui](./getui.md) - 另一家国内厂商推送通道
- [排错指南](/guide/troubleshooting) - 启动失败、消息没到、重试不生效
