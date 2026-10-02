# FCM (Firebase Cloud Messaging)

通过 Firebase Cloud Messaging HTTP v1 API 向 Android / iOS / Web 设备发送推送通知。

## 作用

`fcm` Builtin Provider 使用 Firebase **服务账号**（service account）鉴权：用 `client_email` + RSA 私钥签出 RS256 JWT，向 Google OAuth 端点换取短时效 access token（内存缓存，过期前 5 分钟自动刷新），再以 Bearer 方式调用 FCM v1 `messages:send`。每个投递目标是一个 **FCM 注册令牌（registration token）**，适合 App 推送、设备告警等场景。

## 申请凭据

全程在 [Firebase 控制台](https://console.firebase.google.com/)：

1. **创建/选择项目**：项目设置里的 **项目 ID** 即 `project_id`（形如 `my-firebase-project`）
2. **生成私钥**：**项目设置 ⚙️ → 服务账号 → 生成新的私钥**，下载 JSON 文件
3. **取三个字段**：
   - `project_id` → JSON 的 `project_id`
   - `client_email` → JSON 的 `client_email`（形如 `firebase-adminsdk-xxx@<project>.iam.gserviceaccount.com`）
   - `private_key` → JSON 的 `private_key`（PKCS#8 PEM，`-----BEGIN PRIVATE KEY-----` 开头）

> ⚠️ 服务账号需要 `firebase.messaging` 权限（Firebase 生成的默认服务账号已内置）。App 侧需集成 Firebase SDK 并把 registration token 作为收件人传给 Herald。

## 发第一条消息

按下面的配置配好并 `heraldd serve --config config.yaml` 启动后（**必须带 registration token 收件人**）：

```yaml
# config.yaml 追加（三字段从上面的 JSON 里取）
providers:
  fcm:
    type: fcm
    enabled: true
    config:
      project_id: "my-firebase-project"
      client_email: "firebase-adminsdk-xxx@my-firebase-project.iam.gserviceaccount.com"
      private_key: "$FCM_PRIVATE_KEY"   # PEM 全文放环境变量，见环境变量一节
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "device.alert",
    "level": "error",
    "title": "磁盘告警",
    "body": "/dev/sda1 使用率 95%",
    "channels": ["fcm"],
    "recipients": {"fcm": ["cX618eW0VP6:APA91bH..."]}
  }'
```

设备通知栏弹出「磁盘告警 /dev/sda1 使用率 95%」。没收到？App 是否拿到 token、token 是否传对，查 [排错指南](/guide/troubleshooting) 与下面的[常见错误](#常见错误)。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `project_id` | ✅ | Firebase 项目 ID | 无 |
| `client_email` | ✅ | 服务账号邮箱（JSON 的 `client_email`） | 无 |
| `private_key` | ✅ | 服务账号 RSA 私钥（PKCS#8 PEM 全文） | 无 |
| `endpoint` | ❌ | FCM API 端点（测试/代理场景覆盖） | `https://fcm.googleapis.com` |

缺任一必填项或私钥无法解析时 Provider 创建即失败（`fcm: project_id is required` 等），启动日志可见。私钥只接受 PKCS#8（`PRIVATE KEY`）PEM——Firebase 下载的 JSON 即此格式；非 RSA 密钥会报 `fcm: private_key must be an RSA key`。

## 配置示例

```yaml
providers:
  fcm:
    type: fcm
    enabled: true
    config:
      project_id: "my-firebase-project"
      client_email: "firebase-adminsdk-xxx@my-firebase-project.iam.gserviceaccount.com"
      private_key: "$FCM_PRIVATE_KEY"       # 推荐：PEM 全文放环境变量
      # endpoint: "https://fcm.googleapis.com"  # 可选，一般不用配
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `FCM_PROJECT_ID` | `project_id` | Firebase 项目 ID |
| `FCM_CLIENT_EMAIL` | `client_email` | 服务账号邮箱 |
| `FCM_PRIVATE_KEY` | `private_key` | PKCS#8 PEM 全文（含真实换行） |

> ⚠️ `private_key` 中的换行必须是**真实换行符**。若从 JSON 复制得到 `-----BEGIN PRIVATE KEY-----\n...`（含字面 `\n`），需先替换为换行，或整段放入环境变量后由 shell/YAML 处理。

## 消息模板与限制

- `Content`（title/body，纯文本）→ FCM `notification` 段，由系统托盘渲染
- `Raw` 载荷 → FCM `data` 段（值统一字符串化，FCM data 只支持字符串）
- 两者都缺省时发送纯 data 消息；FCM 要求二者至少其一
- 收件人（registration token）逐 token 发送；部分失败聚合为一个错误返回（`fcm: 1/2 succeeded - failed: ...`），与其它 provider 的部分成功语义一致
- 单 token 消息上限 4KB（FCM 平台限制）

### 能力声明

- `PayloadKinds`: `Content`、`Raw`
- `ContentFormats`: `plain`

### 重试语义

- 请求超时、408/429/5xx 由 `httpclient` 包装为可重试错误，走统一重试
- 其余 4xx（404 未注册 token、400 载荷非法等）为确定性错误，不重试
- OAuth 换取 token 的网络/5xx 失败同样走可重试错误

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `fcm: project_id/client_email/private_key is required` | 配置缺字段 |
| `fcm: private_key is not valid PEM data` | 私钥不是 PEM（可能复制时丢了头尾行） |
| `fcm: private_key must be an RSA key` | 私钥不是 RSA（检查是否拿错了密钥文件） |
| `fcm: token error: invalid_grant` | JWT 换取失败：私钥与 `client_email` 不匹配、时钟偏差过大、或密钥已被吊销——重新生成服务账号密钥 |
| `unexpected status code: 401/403` | access token 无效或项目未开通 FCM |
| `unexpected status code: 404, body: ... NOT_FOUND` | registration token 已失效（App 卸载/token 轮换），应从收件人中剔除 |
| `unexpected status code: 400, body: ... INVALID_ARGUMENT` | 载荷非法（超 4KB、notification 与 data 均为空等），看 body 里的 message |

错误格式说明：HTTP 非 2xx 返回 `unexpected status code: {code}, body: {response body}`，FCM 的结构化错误在 body 里的 `error.message` / `error.status`。整体排错见[排错指南](/guide/troubleshooting)。

## 安全建议

1. **私钥即凭据**：服务账号私钥可代表项目发推送，务必放环境变量或密管系统，不要提交进仓库
2. **最小授权**：为 Herald 单独生成服务账号，仅保留 messaging 权限
3. **密钥轮换**：泄露后在 Firebase 控制台删除旧密钥并重新生成，同步更新配置
4. **失效 token 清理**：404 `NOT_FOUND` 表示 token 已注销，持续重投无意义，应从受众中移除

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Webhook](./webhook.md) - 自定义 HTTP 接收端
- [排错指南](/guide/troubleshooting) - 启动失败、消息没到、重试不生效
