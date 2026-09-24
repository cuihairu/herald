# 测试覆盖率口径

本文说明 Herald 的覆盖率统计口径、当前实测水位、CI 门禁，以及所有未覆盖分支的逐处定性。原则只有一条：**以测试工具实测为准，不写凑数字的假覆盖**。

## 口径定义

- 统计工具为 `go test -coverprofile`，以 Go coverprofile 的**基本块（statement block）**为最小单位，而不是行。
- Go 编译器把 `if`/`for`/`select` 的每个出口、每个 `case` 都切成独立的基本块，每块记录执行次数。**所有块的计数非零等价于分支覆盖已满**：一个条件分支只要两侧都真实执行过，两个块都会非零；任一侧没走到，就会留下一个计数为零的块。
- 因此我们的验收标准是：**profile 中每一个计数为零的块，要么补上确定性触发它的测试，要么在源码就地注释说明它为什么不可达**。没有第三种处理。
- 禁止用无断言的"路过式"测试或伪造调用路径制造覆盖数字。

## 实测水位与门禁

| 项目 | 值 |
| --- | --- |
| 语句覆盖率（CI 口径，`go test -coverprofile` ./...） | **98.7%** |
| CI 门禁 | `.github/workflows/ci.yml` 的 `Enforce coverage gate`，低于 98.7% 直接失败 |
| 未覆盖语句的构成 | 全部为下文清单所列的不可达/防御分支，以及各包不可从测试调用的进程入口 `main()` |

覆盖率每提高都只能通过两种方式：新增真实触发路径的测试，或删除死代码。任何"不可达"定性都必须在零块旁边就地留下注释（关键词 `Defensive` / `Unreachable` / `not callable`），说明该错误分支为何不会发生、保留它的价值是什么（通常是为了未来重构时大声失败，而不是静默吞掉）。

## 三个不可测的进程入口

`main()` 通过 `os.Exit` 终止进程，且不可被测试进程递归调用，三个入口均为 0% 覆盖并以注释定性：

- `cmd/heraldd/main.go` — `main` 调 `run()` 并以其返回码退出进程；`run()` 是可测入口。
- `examples/quickstart/main.go` — 同上，`run()` 可测且已有测试。
- `worker-sdk/go/example/main.go` — 演示程序入口，阻塞到信号，退出路径 `os.Exit(1)` 不可测。

## 不可达/防御分支清单

以下每一条都对应 profile 中一个真实存在的零块，注释就在该分支旁。

### core/rules（规则引擎）

| 位置 | 原因 |
| --- | --- |
| `engine.go` `Engine.Put` 的 `compileRule` 错误分支 | `Validate` 刚刚编译过同一批表达式，此分支不可能触发；保留是为了未来 `Validate` 重构时大声失败 |
| `engine.go` `compileRule` 的 `ParseFor` / `GroupInterval` / `inhibit ttl` / `ack_timeout` 解析错误分支 | `Validate` 已解析过同一字段（同一实现），从 `Put` 进入不可达 |
| `engine.go` `compileRule` 的 `ParseSilenceWindow` 错误分支 | `Validate`（rule.go）调用的是同一个解析函数 |
| `engine.go` `compileRule` 的 silence match 编译错误分支 | `Validate` 已编译过同一表达式 |
| `engine.go` `runProgram` 的非 bool 返回值分支 | `AsBool` 在编译期就拒绝非布尔表达式；保留以防运行时行为意外变化 |
| `for.go` `ForGroupKey` 的 JSON 序列化失败分支 | 参数来自已解码的 JSON，必然可再序列化；对手工构造的 `Env` 回退为稳定字符串而不是 panic |
| `file.go` `save` 的 `json.MarshalIndent` 错误分支 | `Rule` 只含可序列化字段 |
| `state_redis.go` `Put` 的序列化错误分支 | `RuleState` 只含可序列化字段 |

### core

| 位置 | 原因 |
| --- | --- |
| `core/limiter/limiter.go` `GetOrCreate` 的 `factory.Create` 错误分支 | 工厂对未知类型回退到 `token_bucket`，不返回错误；守卫未来的限流器实现 |
| `core/runtime/manager.go` `Deliver` 的 `GetProvider` 错误分支 | 仅当 provider 在上方 `IsEnabled` 检查与本查询之间（两把独立锁）被并发注销时可达——守卫被刻意容忍的停机竞态 |
| `core/user/user.go` 三处 `hashPassword` 错误分支 | 当前的 bcrypt 参数下 `hashPassword` 不会失败 |
| `core/escalation/escalation.go` `saveLocked` 的序列化错误分支 | `Pending` 只含可序列化字段 |

### core/websocket

| 位置 | 原因 |
| --- | --- |
| `server.go` `handleWebSocket` 的 `SetReadDeadline` 错误分支 | `Upgrade` 刚移交一条活连接，只有并发关闭竞态才可能失败 |
| `server.go` `handleWebSocket` 的 ping handler 设置分支 | handler 在 `ReadMessage` 内同步运行，连接已断则读循环本身退出 |
| `server.go` `handleRegister` 的 `SetWriteDeadline` 错误分支 | gorilla 的 `SetWriteDeadline` 只记录时间戳，不触网络，恒成功 |

### core/api 与根包

| 位置 | 原因 |
| --- | --- |
| `api/handler_callbacks.go` `decryptFeishuCallback` 的 `aes.NewCipher` 错误分支 | `aes.NewCipher` 仅在密钥长度非 16/24/32 字节时失败；此处密钥恒为 SHA-256 摘要（32 字节） |
| `api/handler_callbacks.go` `pkcs7Unpad` 长度守卫 | 调用方已拒绝空/非对齐密文，CBC 输出保持输入长度；守卫让 helper 自包含 |
| `herald.go` `New` 的 `RegisterProvider` 错误分支 | 管理器是新建的，名字来自同一个 map，重名不可能；守卫未来的注册路径 |
| `herald.go` `New` 的 `SetProviderLimiter` 错误分支 | 限流器工厂不返回错误（见 `core/limiter`） |

### providers / worker-sdk / 命令行

| 位置 | 原因 |
| --- | --- |
| `providers/builtin/wechat`、`wechatmp` `NewProvider` 的 `parseConfig` 错误分支 | `parseConfig` 只做类型断言，不失败；保留错误以统一构造函数签名 |
| `worker-sdk/go/client.go` `register` 的写失败分支 | 对端 RST 只在下一次网络操作浮现，首次写存在竞态窗口，属"实践中防御" |
| `worker-sdk/go/client.go` `register` 的 `SetWriteDeadline` / `SetReadDeadline` 错误分支 | 连接刚拨号建立（`SetReadDeadline`）/ gorilla 只记录时间戳（`SetWriteDeadline`），均恒成功 |
| `worker-sdk/go/client.go` `writeControl` 的 `SetWriteDeadline` 错误分支 | 同 gorilla 行为 |
| `cmd/heraldd/main.go` `serveCmd`/`workerCmd` 的 `RegisterProvider` 重名守卫 | 内置注册表只注册工厂，每个配置名只注册一次，重名不可达 |
| `cmd/heraldd/main.go` `serveCmd` 的 `SetProviderLimiter` 错误分支 | 限流器工厂回退而非失败 |
| `cmd/heraldd/main.go` `serveCmd` 的 `wsServer.Start` 错误分支 | `Start` 在内部 goroutine 里 `ListenAndServe`，恒返回 nil，绑定失败在 `Start` 内部记日志 |
| `cmd/heraldd/main.go` `registerRemoteWorker` / `heartbeatRemoteWorker` 的序列化与 deadline 分支 | 消息只含具体字段（序列化恒成功）；gorilla deadline 恒成功；读 deadline 在刚拨号的连接上恒可设置 |
| `examples/quickstart/main.go` `run` 的 `herald.New` 错误分支 | `run` 构造的是固定内存配置，`New` 恒接受 |

## 如何维护这份水位

1. 给新代码写测试时以「触发每个分支」为目标，而不是「跑过函数」。
2. 若某分支确实不可达，先怀疑它是不是死代码——是就删掉；确需保留（API 兼容、未来重构防静默），就地写 `// Defensive: ...` 并说明原因。
3. 阈值只随实测水位上调，不预留缓冲；任何人引入未测代码，CI 会立即拦下。
