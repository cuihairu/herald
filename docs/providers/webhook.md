# Webhook (通用 HTTP 回调)

将通知以 JSON 形式 POST/PUT 到任意 HTTP 端点。

## 作用

`webhook` Builtin Provider 把 Herald 通知封装为统一 JSON 载荷，发送到配置的 `url`。适合对接自建告警平台、IM 机器人、CI/CD 系统、Serverless 函数等任意 HTTP 接收端。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `url` | ✅ | 目标 HTTP 端点（完整 URL，含 scheme） | 无 |
| `method` | ❌ | HTTP 方法：`POST`、`PUT`、`GET`、`DELETE` | `POST` |
| `headers` | ❌ | 自定义请求头（`map[string]string`） | 空 |

缺 `url` 时 Provider 创建即失败（`webhook: url is required`）。

> ⚠️ `headers` 为 `map[string]string`，YAML 写法示例见下。**headers 不参与环境变量展开**（`ExpandEnv` 仅处理顶层字符串值），请直接写字面值或在应用层注入。

## 配置示例

```yaml
providers:
  webhook:
    type: webhook
    enabled: true
    config:
      url: "$WEBHOOK_URL"
      method: "POST"                       # 可选：POST / PUT / GET / DELETE
      headers:                             # 可选：自定义请求头
        Authorization: "Bearer your-token" # 字面值，不展开环境变量
        X-Source: "herald"
        Content-Type: "application/json"   # POST/PUT 默认已设为 application/json
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `WEBHOOK_URL` | `url` | 目标 HTTP 端点（`.env.example` 惯用名） |

> ⚠️ `method`、`headers` 通常不含敏感信息，可直接写在配置文件中。

## 消息模板与限制

### 载荷结构 (WebhookPayload)

```json
{
  "id": "task-uuid",
  "provider": "webhook",
  "level": "warning",
  "targets": ["group:ops", "user:alice"],
  "timestamp": "2026-09-28T12:34:56+08:00",
  "title": "服务器告警",
  "body": "CPU 使用率超过 90%",
  "raw": { "custom": "field" }
}
```

| 字段 | 来源 |
|------|------|
| `id` | `task.ID` |
| `provider` | `task.Provider` |
| `level` | `task.Level` |
| `targets` | `task.Targets` |
| `timestamp` | `task.CreatedAt` (RFC3339) |
| `title` | `task.Payload.Content.Title` |
| `body` | `task.Payload.Content.Body` |
| `raw` | `task.Payload.Raw`（透传原始字段） |

### HTTP 行为

- **POST / PUT**: 以 `application/json` 发送上述 JSON，`Content-Type: application/json` 自动设置（可被 `headers` 覆盖）
- **GET / DELETE**: **尚未实现**（返回 `method {method} not yet implemented`），仅 POST/PUT 可用
- 响应体不解析；仅记录响应日志（`httpclient.LogResponse`）
- 请求超时、408/429/5xx 由 `httpclient` 包装为可重试错误，走统一重试

### 限制

- 仅 `POST`/`PUT` 实际可用
- 无签名/防重放机制（如需安全性，请在接收端校验 `Authorization` 或 IP 白名单）
- 载荷大小受 HTTP 客户端/服务端限制（建议单条 < 1MB）

### 能力声明

- `PayloadKinds`: `Content`、`Raw`
- `ContentFormats`: `json`

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `webhook: url is required` | 未配置 `url` |
| `method GET not yet implemented` | 使用了 GET/DELETE，请改用 POST 或 PUT |
| `unexpected status code: 401` | 目标端点返回 401，检查 `headers.Authorization` |
| `unexpected status code: 403` | 目标端点返回 403，检查 IP 白名单/签名校验 |
| `unexpected status code: 404` | `url` 路径错误 |
| `unexpected status code: 500` | 目标端点内部错误，查看对端日志 |
| `context deadline exceeded` | 请求超时（默认 30s），检查网络/目标端点性能 |
| `connection refused` | 目标主机/端口不可达 |

错误格式说明：HTTP 非 2xx 返回 `unexpected status code: {code}, body: {response body}`；网络/超时错误由 `httpclient` 包装。

## 安全建议

1. **HTTPS**：生产环境必须使用 `https://` 端点
2. **认证**：在 `headers` 配置 `Authorization: Bearer <token>` 或 `X-Api-Key`，接收端校验
3. **IP 白名单**：接收端限制仅 Herald 所在 IP 段访问
4. **幂等**：接收端按 `id` 去重（Herald 重试会带相同 `id`）

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Email](./email.md) - SMTP 邮件通道
- [Log](./log.md) - 本地调试输出