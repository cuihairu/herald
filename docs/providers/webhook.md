# Webhook

`webhook` Builtin Provider 把通知以 JSON POST 到你指定的 HTTP 端点，用于接入自建系统、IM 聚合网关、自动化流水线等任意接收端。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `url` | ✅ | 接收端 URL | 无 |
| `method` | ❌ | HTTP 方法；**当前仅实现 `POST` / `PUT`**，配置其他值发送时报错 | `POST` |
| `headers` | ❌ | 自定义请求头。**注意：从 YAML 加载时当前不会生效**（见下方说明） | 空 |

- 缺 `url` 报 `webhook: url is required`
- `method` 非 POST/PUT 报 `method {method} not yet implemented`（GET 查询参数形态尚未实现）

> **⚠️ headers 现状**：代码对 `headers` 做 `map[string]string` 类型断言，而 YAML 解码产物是 `map[string]interface{}`，断言不命中——配置不报错但**请求头不会带上**（预留能力）。需要带认证头的接收端，暂用 URL query token 或等待修复。

## 配置示例

```yaml
providers:
  webhook:
    type: webhook
    enabled: true
    config:
      url: "$WEBHOOK_URL"
      method: "POST"       # 可选，默认 POST；支持 POST / PUT
      # headers:           # 预留：YAML 加载当前不生效
      #   Authorization: "Bearer your_webhook_token"
```

## 环境变量

provider config 里以 `$` 开头的字符串值会在加载时展开为同名环境变量的值（`$VAR` 写法；`"${VAR}"` 带花括号不会被展开）。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `WEBHOOK_URL` | `url` | 接收端 URL（`.env.example` 惯用名） |

## 请求体格式

`Content-Type: application/json`，固定结构：

```json
{
  "id": "task-uuid",
  "provider": "source-provider-name",
  "level": "error",
  "targets": ["ops@example.com"],
  "timestamp": "2026-09-28T10:00:00+08:00",
  "title": "磁盘告警",
  "body": "/data 使用率 92%",
  "raw": { "…": "payload.raw 原样透传，无则为空" }
}
```

- `timestamp` 为 RFC 3339；`provider` / `level` / `targets` / `title` / `body` / `raw` 为空时省略
- 能力声明支持 `raw` payload 透传与 `json` 内容格式

## 响应与重试

- HTTP 2xx 即视为成功（响应体仅记录日志，不做解析）
- 非 2xx：`unexpected status code: {code}, body: …`；其中 **408 / 429 / 5xx 会被包装为可重试错误**走统一重试，其余 4xx 视为确定性失败直接返回
- 请求超时 30s

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `webhook: url is required` | 未配置 `url` |
| `method GET not yet implemented` | `method` 配了非 POST/PUT 值，改回 POST |
| `unexpected status code: 401, …` | 接收端要求认证——注意 headers 当前不生效，用 URL query token 过渡 |
| `unexpected status code: 429, …` | 接收端限流，该错误可重试，会自动退避重发 |
| `failed to send request: …` | URL 不可达 / DNS 解析失败 / 连接超时，检查网络与端点 |

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Email](./email.md) - SMTP 邮件通道
- [Worker SDK](/runtime/sdk) - 需要复杂处理逻辑时改用 Worker
