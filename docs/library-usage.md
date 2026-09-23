# 作为 Go 库使用

Herald 有两种使用形态，二者共享同一套核心管道（队列、路由、去重、模板、Provider）：

- **CLI 网关**（`cmd/heraldd`）：独立进程，REST API + Dashboard，适合作为组织级通知网关部署，见[快速开始](/guide/getting-started)。
- **Go 库**（本篇）：在你的进程里 `import "github.com/cuihairu/herald"`，用一个 `App` 实例完成入队与投递，适合把通知能力直接嵌进自己的服务。

库形态不需要任何外部依赖即可运行：默认使用内存队列（in-memory queue）加本地 worker 池，进程退出时优雅排空。

## 安装

Herald 是单一 Go module（single module），一个依赖即可获得 facade 与全部内置渠道：

```bash
go get github.com/cuihairu/herald
```

## 60 秒上手

```go
package main

import (
    "context"
    "fmt"

    "github.com/cuihairu/herald"
    "github.com/cuihairu/herald/config"
    "github.com/cuihairu/herald/core"
)

func main() {
    cfg := config.Default()
    cfg.Providers["log"] = config.ProviderConfig{Type: "log"}

    app, err := herald.New(cfg)
    if err != nil {
        panic(err)
    }
    defer func() { _ = app.Close() }()

    res, err := app.DispatchSync(context.Background(), &core.Notification{
        Type:     "quickstart",
        Level:    "info",
        Channels: []string{"log"},
        Content:  &core.DirectContent{Title: "Hello from herald"},
    })
    if err != nil {
        panic(err)
    }
    fmt.Println("accepted:", res.Accepted) // [log]
}
```

可运行的最小工程见仓库目录 `examples/quickstart`；标准库 Example 形式的示例见根包的 `ExampleNew`。

## Facade API

`github.com/cuihairu/herald`（根包）是库用户的推荐入口，只暴露六个成员：

| API | 语义 |
| --- | --- |
| `New(cfg *config.Config) (*App, error)` | 构建 App 并在后台启动投递池。`cfg` 为 `nil` 时全部走默认值；`cfg.Providers` 中声明的渠道会在返回前创建并注册，任一失败则中止构建（已创建的资源会被回收） |
| `(*App).Dispatch(ctx, n *core.Notification) (*service.ProcessResult, error)` | 异步投递：渠道解析、模板渲染、去重在调用内同步完成并在此报错，实际发送由后台池执行 |
| `(*App).DispatchSync(ctx, n) (*service.ProcessResult, error)` | 同步投递：在 `Dispatch` 语义之上，阻塞直到该通知产生的每个任务都被 Ack（成功）或 Nack（失败），或 `ctx` 结束 |
| `(*App).Runtime() *core/runtime.Manager` | Provider 管理器，用于注册自定义渠道、启停渠道、查询投递日志 |
| `(*App).Queue() core.Queue` | 底层队列，一般仅在需要旁路观测（如积压深度）时使用 |
| `(*App).Close() error` | 优雅关闭：停止投递池并等待其排空，再关队列、关渠道。关闭后 `Dispatch` 返回错误 |

`App` 对并发 `Dispatch` 安全；`Close` 只应调用一次。

## 默认值

`New` 对零值字段填充默认（只补零值，不覆盖显式配置）：

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| `Queue.Type` | `memory` | 内存队列，无需外部依赖 |
| `Queue.Size` | `10000` | 队列容量 |
| `Queue.Workers` | `NumCPU*2+1` | 本地投递并发度 |
| `Dedup.Window` | `5m` | 去重窗口（去重开关 `Dedup.Enabled` 取自传入配置，`config.Default()` 中为 `true`） |
| `Retry.Max` | `3` | 重试上限 |
| `Retry.Backoff` | `exponential` | 退避策略 |
| `Retry.InitialDelay` / `MaxDelay` | `1s` / `1m` | 退避区间 |

## 配置方式

两种等价入口：

```go
cfg := config.Default()   // 代码内改字段
cfg, err := config.Load("herald.yaml") // 从 YAML 文件读取
```

`config.Config` 的字段与 CLI 形态的配置文件完全一致（`Server`、`Providers`、`Routes`、`Queue`、`Retry`、`Dedup`、`Templates` 等），因此同一份 YAML 既可用于 `cmd/heraldd` 也可用于库形态。库形态下 `Server`/`WebSocket`/`Auth` 等面向 HTTP 服务的字段不生效，忽略即可。另有 `cfg.ExpandEnv()`（展开 `${VAR}` 环境变量）与 `cfg.Validate()` 可按需调用。

渠道声明示例：

```go
cfg.Providers["feishu-ops"] = config.ProviderConfig{
    Type:   "feishu",
    Config: map[string]interface{}{"webhook": "..."},
    // Enabled: nil 等价于启用
}
```

## 路由与渠道选择

一条 `core.Notification` 到达目标渠道的解析顺序：

1. `n.Channels` 非空：直接使用列出的渠道名（必须与 `Providers` 键或 `Runtime()` 注册名一致）。
2. `n.Channels` 为空：查静态路由 `cfg.Routes[n.Type]`。
3. 两者皆无：`Dispatch` 返回 `no route found` 错误——**注册了渠道不等于会被路由到**。

## 去重

`Dedup.Enabled`（`config.Default()` 中开启）时，窗口期内内容相同的重复通知会被抑制：返回的 `ProcessResult.TaskIDs` 为空、无错误，且不会产生投递。业务侧可据此区分「已接受」与「被去重」。

## 投递语义与错误分层

`ProcessResult` 的字段把结果分为三层，理解这一点可以避免误报错误：

| 层 | 字段 | 时机 |
| --- | --- | --- |
| 接受成功 | `Accepted`、`TaskIDs` | 渠道解析通过、任务已入队（`Dispatch` 返回即入队完成） |
| 渠道级失败 | `Failed []ChannelError` | **入队前**的失败：渠道不存在、渠道未启用等 |
| 投递失败 | `DispatchSync` 返回的 `error` | **入队后**发送失败（Provider `Deliver` 返回错误）；此层不进 `Failed` |

对 `Dispatch`（异步）而言，投递失败发生在后台，错误只记录在投递日志中，可通过 `app.Runtime().GetLogs(...)` 观测；需要拿到投递结果请用 `DispatchSync`。

## 自定义 Provider

实现 `core.Provider` 四个方法，注册即用：

```go
type echoProvider struct{}

func (echoProvider) Deliver(_ context.Context, task *core.DeliveryTask) error {
    fmt.Println("delivered:", task.Payload.Content.Title)
    return nil
}
func (echoProvider) Name() string                 { return "echo" }
func (echoProvider) Type() string                 { return "echo" }
func (echoProvider) Status() *core.ProviderStatus { return &core.ProviderStatus{Name: "echo", Type: "echo", Status: "ok"} }

// 构建后注册
_ = app.Runtime().RegisterProvider("echo", echoProvider{}, true)
```

若希望自定义渠道也走 `cfg.Providers` 配置化创建，实现 `core.ProviderFactory` 并 `Runtime().RegisterFactory(...)`，随后在配置里声明 `Type` 指向它。

## 生命周期与并发

- `New` 返回即代表投递池已在后台运行，可直接 `Dispatch`。
- `Close` 的顺序是先停池、等排空，再关队列、关渠道——保证不会丢失已接受的任务，也不会在关闭中的队列上取到零值任务。
- 典型嵌入模式：`New` 一次、随服务进程存活，`http.Server` 关闭后 `defer app.Close()`。

## 导出面清单

库形态的稳定面由以下公开包构成（`internal/` 目录不属于任何承诺）：

| 包 | 角色 | 关键导出 |
| --- | --- | --- |
| `github.com/cuihairu/herald` | facade（推荐入口） | `App`、`New`、`Dispatch`、`DispatchSync`、`Runtime`、`Queue`、`Close` |
| `config` | 配置模型 | `Config`、`Default`、`Load`、`Validate`、`ExpandEnv`；`ProviderConfig`、`QueueConfig`、`RetryConfig`、`DedupConfig` 等 |
| `core` | 领域模型与接口 | `Notification`、`DirectContent`、`DeliveryTask`、`DeliveryPayload`、`RenderedContent`、`Provider`、`ProviderFactory`、`ProviderStatus`、`ProviderCapability`、`Queue`、`BuiltinProvider`、`CapableProvider`、`WorkerProvider`、`MaskConfig` |
| `core/service` | 处理管道 | `NotificationService`、`ProcessResult`、`ChannelError`、`DeliveryPlanner` |
| `core/runtime` | Provider 管理 | `Manager`（`RegisterProvider`、`RegisterFactory`、`Enable`/`Disable`、`GetLogs` 等投递日志查询） |
| `core/queue` | 队列实现 | `NewQueue`、`NewMemoryQueue`、`NewRedisQueue`、`QueueConfig` |
| `core/route` | 静态路由 | `Router` |
| `core/dedup` | 内容去重 | `Dedup` |
| `core/retry` | 重试策略 | `Config` |
| `core/logstore` | 投递日志存储 | `TaskLog`、`Filter`、`Stats` |
| `core/template` | 模板系统 | `Manager`、`TemplateConfig` |
| `core/worker` | 本地投递池 | `Pool`、`Registry` |
| `providers/builtin/registry` | 内置渠道注册 | `RegisterBuiltinProviders` |
| `providers/builtin/*` | 内置渠道实现 | `log`、`webhook`、`email`、`slack`、`telegram`、`discord`、`feishu`、`dingtalk`、`wecom`、`wechat`、`wechatmp`、`aliyunsms`、`tencentsms`、`neteasesms`，以及转发远程 Worker 的 `worker` 类型 |
| `worker-sdk/go`（包 `sdk`） | 远程 Worker 客户端 | 自定义 Runtime 侧对接 Herald 的 SDK |

版本兼容承诺：facade（根包）与上表公开包的导出签名遵循 Go 模块语义化版本；`internal/` 与各包未导出成员随时可能调整。
