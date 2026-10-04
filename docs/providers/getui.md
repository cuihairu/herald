# Getui (个推推送)

向 Android / iOS / 鸿蒙设备发送推送通知，国内厂商通道到达率优化。

## 作用

`getui` Builtin Provider 直接调用个推 REST API v2（`POST {BaseUrl}/push/single/cid`，`BaseUrl` 为 `https://restapi.getui.com/v2/$appId`，见[接口调用规范](https://docs.getui.com/getui/server/rest_v2/standard/)），鉴权为接口 token：`POST {BaseUrl}/auth` 换取，签名是 `sha256(appkey + timestamp + mastersecret)` 的固定顺序拼接。

每个投递目标是一个 **CID**（个推为设备分配的用户唯一标识），按设备逐次发送，一条消息两种承载：

- Content（title/body）→ `push_message.notification` 通知段，进系统通知栏
- Raw 载荷 → `push_message.transmission` 透传段，不上通知栏，由 App 内集成的个推 SDK 回调处理
- 个推通道的通知只在 Android/鸿蒙展示，iOS 另走 `push_channel.ios.aps`（Herald 对带 Content 的消息自动补上）

## 申请凭据

前提：一个[个推账号](https://dev.getui.com/)（注册免费）。

1. **创建应用**：[开发者中心](https://dev.getui.com/dev/#/appManage) → **应用管理** → 创建应用（Android 填包名，iOS 填 Bundle ID）
2. **记下 AppID**：应用唯一标识，也是接口路径的一段（`BaseUrl` 里的 `$appId`）
3. **记下 AppKey**：创建应用时生成，用于 `/auth` 的 `appkey` 与签名
4. **记下 MasterSecret**：应用详情页可查，等同推送全权凭据，只在服务端使用
5. **设备 CID**：App 集成[个推 SDK](https://docs.getui.com/getui/mobile/android/androidstudio/)后拿到，Android 在 `onReceiveClientId` 回调、iOS 在 `GeTuiSdkDidRegisterClient` 回调。CID 由你的业务服务端存进花名册/用户表，投递时作为收件人传入；Herald 不负责从个推获取。

## 发第一条消息

按下面配好并 `heraldd serve --config config.yaml` 启动后（**必须带 CID 收件人**）：

```yaml
# config.yaml 追加（AppID/AppKey/MasterSecret 从上面的申请步骤取）
providers:
  getui:
    type: getui
    enabled: true
    config:
      app_id: "your_app_id"
      app_key: "your_app_key"
      master_secret: "$GETUI_MASTER_SECRET"   # 见环境变量
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order.notice",
    "level": "info",
    "title": "订单已发货",
    "body": "您的订单 SF123456 已发出",
    "channels": ["getui"],
    "recipients": {"getui": ["CID-字符串"]}
  }'
```

手机通知栏收到「订单已发货」。没收到？检查 CID 是否正确、App 是否集成个推 SDK 且处于可推送状态，查[排错指南](/guide/troubleshooting)与下面的[常见错误](#常见错误)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `app_id` | ✅ | 应用唯一标识（接口路径的一段） | 无 |
| `app_key` | ✅ | 应用 AppKey（`/auth` 的 `appkey` 与签名输入） | 无 |
| `master_secret` | ✅ | 应用 MasterSecret（签名输入，仅服务端） | 无 |
| `ttl` | ❌ | 离线保留时长（毫秒），`-1` 表示不设离线保留；范围 `-1` ～ `259200000`（3 天） | 个推默认 2 小时 |
| `click_type` | ❌ | 点击通知的后续动作，见下表 | `none` |
| `endpoint` | ❌ | API 端点（测试/代理场景覆盖） | `https://restapi.getui.com/v2` |

`click_type` 可选值（Herald 只支持不需要额外字段的四种）：

| 值 | 行为 |
|----|------|
| `none` | 纯通知，无后续动作 |
| `startapp` | 打开应用首页 |
| `payload` | 自定义消息内容，**启动应用** |
| `payload_custom` | 自定义消息内容，**不启动应用** |

个推还支持 `url`（打开网页）与 `intent`（打开应用内特定页面），但两者分别要求额外的 `url`/`intent` 字段，Herald 的消息模型不携带，故不支持——配了会在启动时报 `getui: unsupported click_type`。

缺 `app_id`/`app_key`/`master_secret` 时 Provider 创建即失败（`getui: app_id is required` 等），启动日志可见。

## 配置示例

```yaml
providers:
  getui:
    type: getui
    enabled: true
    config:
      app_id: "your_app_id"
      app_key: "your_app_key"
      master_secret: "$GETUI_MASTER_SECRET"
      ttl: 86400000          # 离线保留 1 天
      click_type: "startapp" # 点击回 App 首页
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `GETUI_APP_ID` | `app_id` | 应用唯一标识（`.env.example` 惯用名） |
| `GETUI_APP_KEY` | `app_key` | 应用 AppKey |
| `GETUI_MASTER_SECRET` | `master_secret` | 应用 MasterSecret |

## 消息模板与限制

### 内容映射

| 场景 | body 结构 |
|------|-----------|
| 带 title/body 的通知 | `push_message.notification{title,body,click_type}` + `push_channel.ios.aps{alert,sound:default}` |
| 纯 Raw 载荷 | `push_message.transmission`——透传给 App，不上通知栏；内容是 Raw 的 JSON |
| Content + Raw 同时有 | 只发 `notification`，Raw 进 `notification.payload`；仅 `click_type` 为 `payload`/`payload_custom` 时携带 |

- 个推把 `notification.title`/`body` 都标为必填，所以 title-only 或 body-only 的消息会让另一字段取同样的值兜底
- `notification` 与 `transmission` 个推要求三选一（与 `revoke` 互斥），不能同发；Herald 据此把 Raw 降级成点击回传的 `payload`
- Raw 的值经 JSON 序列化后按**字符数**（`utf8.RuneCountInString`）计长度上限
- Content 与 Raw 至少其一，否则报 `getui: payload needs content or raw data`

### 长度与取值限制

| 字段 | 限制 | 谁来拦 |
|------|------|--------|
| `transmission`、`notification.payload` | ≤ 3072 字 | `transmission` 超限直接报错；`payload` 超限则丢弃该字段（通知照发） |
| `notification.title` | ≤ 50 字 | 个推服务端校验（返回 20001），Herald 不预检 |
| `notification.body` | ≤ 256 字 | 同上 |
| `settings.ttl` | `-1` ～ `259200000` 毫秒 | Herald 启动时校验 |
| `request_id` | 10-32 位，且不可重复 | Herald 每次发送生成 24 位随机 id；重复会导致个推**丢消息** |

### 发送语义

- 收件人逐 CID 发送；部分失败聚合为 `getui: N/M succeeded - failed: ...`（与 FCM/APNs/JPush 一致），失效 CID 只算它自己失败
- 业务码 `10001`（token 失效）触发个推推荐的被动刷新：丢弃缓存 token 重新 `/auth`，并用**同一个 `request_id`** 重试一次（首次未投递，复用 id 不会撞上个推的去重）
- `/auth` 每分钟最多 100 次、每天最多 10 万次。token 有效期是「调用时间 + 1 天」，Herald 按返回的 `expire_time` 缓存并提前 5 分钟刷新，且刷新在缓存锁内完成——冷启动时并发投递只会触发一次 `/auth`

### 能力声明

- `PayloadKinds`: `Content`、`Raw`
- `ContentFormats`: `plain`

### 重试语义

- 请求超时、408/429/5xx 由 `httpclient` 包装为可重试错误，走统一重试
- 400（20001 参数不合法）、401（10001/10002/10003）、403（30000 段）为确定性错误，不重试
- 业务码 `10001` 是唯一的例外：Herald 已在 provider 内做过一次被动刷新 + 重试

## 常见错误

### 启动即失败

| 错误 | 原因与处理 |
|------|-----------|
| `getui: app_id is required` | 没配 AppID |
| `getui: app_key is required` | 没配 AppKey |
| `getui: master_secret is required` | 没配 MasterSecret |
| `getui: ttl must be between -1 and 259200000 ms` | 离线保留时长超出个推范围（注意单位是毫秒） |
| `getui: unsupported click_type "url"` | `url`/`intent` 需要额外字段，Herald 不支持 |
| `getui: transmission exceeds 3072 chars: N` | 纯透传消息超长，裁剪 Raw 载荷 |
| `getui: payload needs content or raw data` | Content 和 Raw 都为空 |

### 接口返回的业务码

| code | HTTP | 含义与处理 |
|------|------|-----------|
| `10001` | 401 | token 错误/失效：Herald 已自动刷新并重试一次；持续出现说明 `/auth` 频繁超限或凭据不对 |
| `10002` | 401 | appId 或 IP 在黑名单中 |
| `10003` | 401 | 每分钟鉴权频率超限（上限 100 次/分钟） |
| `10005` | 401 | 每分钟调用频率超限 |
| `20001` | 400 | 参数不合法，`msg` 会指明字段（如 `title is invalid`、`ios长度不能超过...`） |
| `30007` | 403 | `app/all` 推送次数超每日上限（每日最多 20 次）——本 provider 走单推，正常不会碰到 |
| `30014` | 403 | app 推送频率超每分钟上限（每分钟最多 5 次） |
| `30015` | 403 | list 推送频率超每分钟上限 |
| `5000` | — | 请求未到个推服务器：查本地网络、代理、IP 白名单 |

`30000` 段（30000-30028）是套餐/权限类限制，非 VIP 账号设置厂商策略或用定时推送时会碰到。

错误格式：HTTP 非 2xx 返回 `unexpected status code: {code}, body: {"code":..,"msg":".."}`；HTTP 200 但 `code` 非 0 时返回 `getui: push failed (code N): msg`。整体排错见[排错指南](/guide/troubleshooting)。

## 安全建议

1. **MasterSecret 等同推送全权凭据**：只放服务端环境变量/密管系统，不进客户端与代码仓库；怀疑泄露立即在开发者中心重置
2. **token 是全局接口凭据**：Herald 只在内存里缓存，不落盘、不打日志；`master_secret` 已被 `core.SensitiveFields` 标记，API 查询配置时返回掩码值
3. **CID 是设备标识**：按个人数据对待，落库、传输注意合规
4. **别把 `/auth` 打满**：Herald 已按 `expire_time` 缓存并去重并发刷新；自行加签发实例时注意每分钟 100 次上限

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [JPush](./jpush.md) - 另一家国内厂商推送通道
- [FCM](./fcm.md) - Android/Web 推送通道
- [APNs](./apns.md) - iOS/macOS 推送通道
- [排错指南](/guide/troubleshooting) - 启动失败、消息没到、重试不生效