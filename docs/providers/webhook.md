# Webhook (通用 HTTP 回调)

通过标准 HTTP 请求将通知推送到任意 HTTP 端点，适配自建服务、第三方平台、Serverless 函数等一切能接收 HTTP 的下游。

## 作用

`webhook` Builtin Provider 将 Herald 的通知内容序列化为 JSON，按配置的 HTTP Method 与 Headers 发送到指定 URL。适合对接内部运维平台、钉钉/飞书自定义机器人（非 SDK 模式）、Prometheus Alertmanager、Serverless 函数、自研告警中心等场景。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `url` | ✅ | 目标 HTTP 端点完整地址（含 scheme、host、path、query） | 无 |
| `method` | ❌ | HTTP 方法：`POST` / `PUT` / `GET` / `DELETE` | `POST` |
| `headers` | ❌ | 自定义请求头（YAML 映射 `key: value`） | 空 |

缺 `url` 时 Provider 创建即失败（`webhook: url is required`），启动日志可见。

> ⚠️ **Headers 不参与环境变量展开**：`config.yaml` 中 `headers` 下的值为字面字符串，**不会**被 `$VAR` 替换。如需在 Header 中传递 Token，请在下游侧通过查询参数或请求体携带，或由 Worker Provider 动态组装。

## 配置示例

### 基础 POST JSON（最常用）

```yaml
providers:
  webhook:
    type: webhook
    enabled: true
    config:
      url: "$WEBHOOK_URL"
      method: "POST"
      headers:
        Content-Type: "application/json"
        X-Source: "herald"
```

### 自建告警平台（Bearer Token 在 URL query 中）

```yaml
providers:
  alert-center:
    type: webhook
    enabled: true
    config:
      url: "https://alert.example.com/api/v1/ingest?token=$ALERT_CENTER_TOKEN"
      method: "POST"
      headers:
        Content-Type: "application/json"
```

### 兼容 Alertmanager Webhook 格式

```yaml
providers:
  alertmanager:
    type: webhook
    enabled: true
    config:
      url: "http://alertmanager:9093/api/v2/alerts"
      method: "POST"
      headers:
        Content-Type: "application/json"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `WEBHOOK_URL` | `url` | 目标 HTTP 端点（含 query 参数时整体写入） |

`method` 与 `headers` 的值通常为字面常量，**不建议**用环境变量。

## 消息模板与限制

### 请求体（POST / PUT）

```json
{
  "id": "task-uuid",
  "provider": "webhook",
  "level": "warning",
  "targets": ["ops-team"],
  "timestamp": "2026-09-28T12:34:56+08:00",
  "title": "CPU 使用率超过 90%",
  "body": "服务器 node-01 持续 5 分钟负载过高",
  "raw": {}
}
```

- 字段来源：`task.ID`、`task.Provider`、`task.Level`、`task.Targets`、`task.CreatedAt` (RFC3339)、`task.Payload.Content.Title/Body`、`task.Payload.Raw`
- `Content-Type: application/json` 由 `headers` 控制，默认不自动添加

### GET / DELETE

当前实现**仅支持 POST / PUT 发送 JSON**（其他方法返回 `method {method} not yet implemented`）。

### 能力声明

- `PayloadKinds`: `content`（标准 title/body）+ `raw`（透传 `task.Payload.Raw`）
- `ContentFormats`: `json`

### 限制说明

| 限制项 | 说明 |
|--------|------|
| **仅 POST/PUT 可用** | GET/DELETE 返回未实现错误 |
| **超时** | 底层 `httpclient.Client` 默认 30s；429/5xx 走统一重试 |
| **响应体** | 仅记录日志（`httpclient.LogResponse`），不做业务校验 |
| **Headers 字面量** | 环境变量不展开，敏感信息勿写在 headers |
| **证书验证** | 使用系统 CA 池；自签证书需在 OS 层面信任或用 Worker 侧控制 |

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `webhook: url is required` | 未配置 `url` |
| `method GET not yet implemented` | 仅 POST/PUT 支持发送 JSON |
| `unexpected status code: 401, body: …` | 下游鉴权失败，检查 Token/签名 |
| `unexpected status code: 404, body: …` | URL 路径错误 |
| `unexpected status code: 500, body: …` | 下游服务异常，查看下游日志 |
| `Post …: dial tcp: timeout` | 网络不通、防火墙、或下游超时 |
| `Post …: x509: certificate signed by unknown authority` | 自签证书未被信任 |

错误格式：HTTP 非 2xx → `unexpected status code: {code}, body: {body}`；网络/HTTP 错误走统一重试。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Email](./email.md) - SMTP 邮件通道
- [Log](./log.md) - 本地调试输出
- [Worker Runtime](/runtime/worker) - 复杂逻辑请用 Worker Provider