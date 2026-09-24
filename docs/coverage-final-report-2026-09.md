# 覆盖率任务收官报告（2026-09）

任务：把 Herald 全仓测试覆盖率提升到 100%（行覆盖 + 分支覆盖双满），以测试工具实测为准，禁止凑数的假覆盖；确实不可达的分支如实记录原因；CI 门禁同步抬到实测水位。全程只提交推送 main，无任何发版动作。

方法与过程详见[覆盖率攻坚总结（2026-09）](./coverage-initiative-2026-09.md)，口径定义与残余零块清单见[测试覆盖率口径](./architecture/coverage.md)。本文是任务链的最终状态记录。

## 最终结果

| 项目 | 值 |
| --- | --- |
| 语句覆盖率（`go test -coverpkg=./... -coverprofile` 全仓口径） | **99.5%** |
| CI 门禁 | 99.5%，生效且通过 |
| 残余零块 | **仅 6 块** = 三个入口包的 `main()` 函数块 |
| 残余零块状态 | 全部就地注释定性，且经 `go build -cover` + GOCOVERDIR 子进程守卫测试**真实运行并断言** |
| CI | main 最新提交双 run 全绿 |
| 发版动作 | 无（未打 tag、未发 release、未改版本号） |

为什么不是字面上的 100%：`main()` 只能由 OS 启动进程调用，`go test` 永远测不到；`main` 里的 `os.Exit(n)` 会跳过 GOCOVERDIR 转储，是 Go 工具链的原理性限制。三个入口的 main 成功路径全部改为 `return` 退出并被子进程口径实测，失败路径 `os.Exit(n)` 就地注释定性——这是工具链允许的实测下限，不是"测不到就算了"。

## 提交链

| 提交 | 内容 |
| --- | --- |
| `1f7eab9` | 期 1：私有函数直调实测——`compileRule` 六类字段错误（表驱动）、`pkcs7Unpad` 非法输入、显式 `GroupInterval` 编译路径 |
| `b570ec0` | 期 2+3：防御分支清零（直调/seam 实测、重构消灭、恒 nil 改写）+ 三个 `main()` 双口径实测 + 门禁 98.7%→99.5% + 口径文档更新 |
| `c68b4a7` | 修复 CI 暴露的 `awaitingQueue.wait` select 双就绪竞态（取消优先预检） |
| `c9e9483` | 零块核对器入库 `tools/zero_check.py` |
| `89dd2e5` | 修复 vitepress 死链（docs 内不能链接站点根之外的文件） |

## 三期冲刺内容

### 期 1 —— 私有函数直调实测

包级私有函数不走生产入口也能测：同包测试直接调用，一次覆盖一类错误分支。`compileRule` 对 `for` / `group_interval` / `inhibit.ttl` / `silence.window` / `silence.match` / `ack_timeout` 六类字段错误的表驱动实测，`pkcs7Unpad` 的 nil / 空 / 非对齐 / 零填充非法输入实测。

### 期 2 —— 防御分支清零

原清单几十处防御性零块，三种结局，profile 中不再存在任何防御性零块：

1. **直调 / seam 实测**：`writeControl` 改包级变量注入传输失败（真实 dial）；`evalProgram` 打桩超时（真实表达式被安全护栏限制，无法自然超时）；`runProgram` 取消与超时路径直调。
2. **重构消灭死分支**：`Manager.Deliver` 的 `IsEnabled`+`GetProvider` 双锁竞态合并为单锁 `lookupEnabled`；`Engine.Put` 把 `compileRule` 前置到 `Validate` 之前，编译错误分支从"不可达守卫"变为可实测；`runProgram` 的非 bool 返回值分支改单值断言（不变量破坏必须 panic）。
3. **恒 nil 显式接受**：`x, _ := f(...)` 加注释说明契约——gorilla `SetWriteDeadline` 只记录时间戳、可序列化结构的 `json.Marshal`、回退式限流器工厂、base64 密码摘要、SHA-256 摘要上的 `aes.NewCipher`、类型断言式 `parseConfig`。若契约变化，注释要求恢复显式错误处理。

### 期 3 —— main() 双口径实测

三个入口（`cmd/heraldd`、`examples/quickstart`、`worker-sdk/go/example`）统一模式：

1. `main` 改写为成功路径 `return`，失败才 `os.Exit(code)`；
2. 各包 `main_cover_test.go` 以 `go build -cover -coverpkg=./...` 编译子进程（不加 `-coverpkg` 时 main 包本身不被插桩），在 `GOCOVERDIR` 下真实运行——heraldd 起 HTTP 服务后 SIGTERM 优雅退出、quickstart 同步派发后自然退出、worker-sdk 示例对 `ws://localhost:8081` 完成注册握手后 SIGTERM（端口被占则 skip）；
3. 断言 `go tool covdata textfmt` 转储中 `main()` 区间内除 `os.Exit` 行（`mainExitAllow` 豁免表）之外每个块计数非零。

## 顺带消灭的真问题

冲刺过程中暴露并修复了三处与覆盖率无关的缺陷：

- **`runProgram` 取消竞态**：`select` 在「评估完成」与「上下文取消」同时就绪时伪随机择一，canceled context 的评估可能误报成功。修复：进入等待前预检父 `ctx.Err()`，取消路径确定性失败。
- **`awaitingQueue.wait` 同款竞态**：任务已交付且 ctx 已取消时 `select` 双就绪随机返回，取消的 `DispatchSync` 可能误报成功。CI 上被 `TestRunCanceledContext` 抓住。修复：同款取消优先预检。
- **`evalTimeout` 过紧**：50ms 在高负载下会被调度延迟击穿（CI 上正常表达式评估报超时）。放宽至 500ms，对微秒级的正常评估无感知，护栏（防失控程序）语义不变。

## 验证清单

每批提交前的本地验收（全部通过后才 push）：

- `gofmt -l .` 无输出，`go vet ./...`、`golangci-lint run` 干净
- `go test -race -count=1 ./...` 至少两遍全绿（flake 修复类提交对目标包额外 -race 重复 3-5 遍）
- 零块核对器 `python3 tools/zero_check.py coverage.out` GREEN（全仓 `-coverpkg=./...` 口径）
- 涉及文档站时 `pnpm run build` 通过（无死链）
- CI 双 run（test / build）全绿确认

## 维护

新代码以"触发每个分支"为测试目标；新增 `main()` 或常驻进程入口时同步补 GOCOVERDIR 子进程守卫测试；门禁只随实测水位上调，不预留缓冲。
