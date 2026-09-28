# Log (本地日志输出)

将通知内容直接写入标准输出/日志系统，适合开发调试、本地验证、CI 流水线观测。

## 作用

`log` Builtin Provider 在 `Deliver` 阶段把通知的标题、正文、级别等字段通过结构化日志（`logger.Info`）与 `fmt.Printf` 双重输出。不依赖任何外部服务，**零配置可用**，是开发联调、单元测试、临时通道的首选。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `name` | ❌ | Provider 实例名称（用于日志区分多实例） | `"log"` |

无任何必填项，`config` 可为空或省略。

## 配置示例

### 最小配置（零配置）

```yaml
providers:
  log:
    type: log
    enabled: true
    config: {}
```

### 多实例区分（开发/测试/生产同机部署时）

```yaml
providers:
  log-dev:
    type: log
    enabled: true
    config:
      name: "log-dev"
  log-test:
    type: log
    enabled: true
    config:
      name: "log-test"
```

## 环境变量

Herald 加载配置时会把 provider config 里**以 `$` 开头的字符串值**替换为同名环境变量的值（`$VAR` 写法，按 `VAR` 查找）。注意：`"${VAR}"` 带花括号的写法**不会被展开**（会按 `{VAR}` 查找并原样保留），请使用 `$VAR`。

| 环境变量 | 对应配置项 | 说明 |
|----------|-----------|------|
| `LOG_PROVIDER_NAME` | `name` | 实例名称（极少需要通过环境变量配置） |

## 消息模板与限制

### 输出格式

**结构化日志（JSON，含级别/时间/字段）：**
```
{"level":"info","msg":"delivering task","provider":"log","task_id":"abc-123","title":"CPU 告警","body":"负载过高","level":"warning"}
```

**标准输出（人类可读，同步打印）：**
```
[warning] CPU 告警: 负载过高
```

### 能力声明

- `PayloadKinds`: `content`（标准 title/body）
- `ContentFormats`: `plain`

### 限制说明

| 限制项 | 说明 |
|--------|------|
| **仅本地可见** | 不具备远程触达能力，不可用于生产告警 |
| **无持久化** | 随进程 stdout 消失，需配合日志采集（ELK/Loki/文件） |
| **无重试/限流** | 同步打印即返回，失败仅为 `fmt.Printf` 极端异常 |
| **无认证/授权** | 任意任务路由到该 Provider 均会打印 |

## 常见错误

| 错误 | 原因与处理 |
|------|-----------|
| `task is nil` | 内部调度异常，任务对象为空（极罕见，重启 Herald 核心） |

`log` Provider **不会返回业务错误**；唯一可能的 error 是入参 `task == nil`（防御性编程）。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [Webhook](./webhook.md) - 需要 HTTP 触达时的通用方案
- [Worker Runtime](/runtime/worker) - 复杂逻辑请用 Worker Provider