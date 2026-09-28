# Log (本地日志输出)

把通知直接写入标准输出 / 结构化日志，用于开发调试、测试验证、本地回环。

## 作用

`log` Builtin Provider 不发送任何网络请求，仅将通知内容通过 `logger.Info` 结构化记录并 `fmt.Printf` 打印到 stdout。适合：

- 本地开发/单测验证通知流转
- CI 流水线中捕获通知内容做断言
- 作为「兜底 Provider」配合规则引擎 `inhibit`/`silence` 观察被抑制的通知
- Worker 模式下的本地回环测试

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `name` | ❌ | Provider 实例名称（用于日志区分多实例） | `log` |

无必填项，配置为空也能创建成功。

## 配置示例

```yaml
providers:
  log:
    type: log
    enabled: true
    config:
      name: "debug-log"   # 可选：区分多个 log provider
```

```yaml
# 最小配置
providers:
  log:
    type: log
    enabled: true
    config: {}
```

## 环境变量

无敏感配置项，通常直接写死。如需通过环境变量指定名称：

```yaml
config:
  name: "$LOG_PROVIDER_NAME"  # 可选
```

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

## 消息模板与限制

### 输出格式

**结构化日志** (`logger.Info`，JSON 格式，配合 `zap`/生产日志系统)：

```json
{
  "level": "info",
  "ts": "2026-09-28T12:34:56.789+0800",
  "msg": "delivering task",
  "provider": "debug-log",
  "task_id": "uuid-xxx",
  "title": "服务器告警",
  "body": "CPU 使用率超过 90%",
  "level": "warning"
}
```

**控制台打印** (`fmt.Printf`，人类可读)：

```
[warning] 服务器告警: CPU 使用率超过 90%
```

格式：`[{task.Level}] {title}: {body}`

### 限制

- **不发送任何网络请求**，无网络错误、超时、重试
- 无持久化、无去重、无限流
- `Deliver` 永远返回 `nil`（除非 `task == nil` 返回 `task is nil`）

### 能力声明

- `PayloadKinds`: `Content`
- `ContentFormats`: `plain`

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `task is nil` | 传入的 `DeliveryTask` 为 nil（调用方 bug） |

除 `task is nil` 外无其他错误返回。

## 典型用法

### 1. 本地开发验证

```yaml
providers:
  log:
    type: log
    enabled: true
```

启动 Herald，发送通知，终端直接看到输出。

### 2. 多实例区分

```yaml
providers:
  log-debug:
    type: log
    enabled: true
    config:
      name: "debug"
  log-audit:
    type: log
    enabled: true
    config:
      name: "audit"
```

日志中 `provider` 字段分别为 `debug` / `audit`，便于过滤。

### 3. 规则引擎观察模式

配合 `shadow` 模式或 `inhibit` 规则，把被抑制/静默的通知也路由到 log provider，做审计留痕：

```yaml
rules:
  - id: "silence-night"
    inhibit:
      time: "22:00-08:00"
    route:
      - provider: "log"   # 夜间通知不发真实渠道，仅落日志
```

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Webhook](./webhook.md) - 自定义 HTTP 接收端
- [Email](./email.md) - SMTP 邮件通道