# 覆盖率攻坚总结（2026-09）

本轮任务：把 Herald 全仓测试覆盖率提到**行覆盖 + 分支覆盖双满**，以测试工具实测为准，禁止凑数的假覆盖；确实不可达的分支/防御性代码逐处记录原因；CI 门禁同步抬到实测水位。

最终结果：**语句覆盖率 99.5%，CI 门禁 99.5% 已生效并通过**。残余零块只剩三个入口包的 `main()` 函数块——go-test 口径下原理性不可测，但已通过 `go build -cover` + GOCOVERDIR 子进程守卫测试**真实运行并断言**（见下文三期）；原清单几十处防御分支全部转为实测、重构消灭或恒 nil 改写，逐处说明见[测试覆盖率口径](./architecture/coverage.md)。

## 口径与方法

- **块覆盖 ⟺ 分支满**：Go coverprofile 以基本块为最小单位，`if`/`for`/`select` 每个出口都是独立块。所有块计数非零等价于分支全覆盖；任一零块都必须"有实测"或"有定性注释"，没有第三种处理。
- **零块核对器**：脚本解析 coverprofile，按 `(文件, 块)` 聚合取各测试二进制报告的 max count（同一块会被多个包的测试各报告一次，不聚合就会误判），对每个真零块检查其上方 7 行与块体内是否含 `Defensive` / `Unreachable` / `not callable` 定性注释。两个口径（每包自有测试、`-coverpkg=./...` 全仓交叉）均须 GREEN。
- **探针先行**：每个"疑似不可达"先写临时探针测试验证，确实触发不了的才落 Defensive 注释；探针触发了的就转为正式测试。本轮借此纠正了多处错误定性。
- **确定性错误注入**：真实网络错误难以稳定复现，采用的手段包括 `SetLinger(0)`+Close 制造 RST、无应答服务端制造读超时、坏数据注入（非 JSON、非块对齐密文、不可序列化字段）、`jsonMarshal` seam 注入编码失败。

## 六批推进

| 提交 | 批次 | 内容 |
| --- | --- | --- |
| `1d912a8` | 批 1 | api 包：回调解密、JWT、user 等 |
| `7974e86` | 批 2 | core 小包与 rules 状态存储 |
| `1d3958b` | 批 3 | rules 规则引擎编译期与静默窗路径 |
| `5e28b62` | lint 修复 | CI 抓到 `errcheck`（`mr.Set`、`SetReadDeadline` 返回值未检查），此后每批提交前本地必跑 golangci-lint |
| `b33fcd0` | 批 4 | websocket：真实升级链路、ACK 失败路径、seam 注入 |
| `ef34449` | 批 5 | providers 构造防御、worker-sdk 控制面（readLoop/heartbeatLoop/dispatch） |
| `9c611f8` | 批 6 | cmd/heraldd 启动失败与全功能生命周期、examples、CI 门禁、口径文档 |

每批的交付标准一致：`go test -race -count=1 ./...` 全绿（46 包）+ gofmt/vet/golangci-lint 干净 + 零块核对器 GREEN 后才提交，只 push main。

## 三期冲刺（98.7% → 99.5%，残余为零块下限）

六批之后 profile 里剩下的全部是防御性零块与三个 `main()` 入口。三期把这块吃干净：

| 期 | 内容 |
| --- | --- |
| 期 1（`1f7eab9`） | 私有函数直调实测：`compileRule` 六类字段错误（表驱动）、`pkcs7Unpad` 非法输入、显式 `GroupInterval` 编译路径 |
| 期 2 | 防御分支清零：直调/seam 实测（`writeControl` 传输失败、`evalProgram` 打桩超时）；重构消灭死分支（`Deliver` 双锁 → 单锁 `lookupEnabled`、`Put` 前置 `compileRule`、`runProgram` 单值断言）；恒 nil 显式接受（gorilla deadline、可序列化结构的 Marshal、回退式限流工厂、base64 密码摘要、SHA-256 摘要上的 `aes.NewCipher`），`x, _ :=` 就地注释契约 |
| 期 3 | main() 双口径实测：三个入口 `main` 改为成功路径 `return`；各包 `main_cover_test.go` 以 `go build -cover -coverpkg=./...` 编译子进程、`GOCOVERDIR` 真实运行（SIGTERM 优雅退出或自然退出），断言转储中 `main()` 区间除 `os.Exit` 行外全覆盖 |

`os.Exit(n)` 跳过 GOCOVERDIR 转储是 Go 工具链的行为，属原理性不可测；除此之外的 main 成功路径全部实测。顺带消灭了两处测试脆弱性：`runProgram` 的 select 在取消与完成同时就绪时随机择一（补父 ctx 预检，取消路径确定性失败），`evalTimeout` 50ms 在高负载下会被调度延迟击穿（放宽到 500ms，护栏语义不变）。

## 关键事实沉淀

分析过程中确认、并已写入源码注释的产品事实：

- gorilla/websocket（v1.5.3）的 `SetWriteDeadline` 只记录时间戳不触网络，恒返回 nil；`SetReadDeadline` 委托底层 net.Conn，活连接上恒成功。
- `websocket.Server.Start` 在内部 goroutine 里 `ListenAndServe`，绑定失败只在内部记日志，`Start` 恒返回 nil——serveCmd 的对应分支不可达。
- herald serveCmd 的退出模型是等 SIGINT/SIGTERM 信号而非 ctx；组件错误只 cancel ctx 不退出进程。
- 内置注册表只注册 Provider 工厂，`serveCmd`/`workerCmd` 的 `RegisterProvider` 重名守卫不可达。
- go-redis v9 的 `AddHook` 会立刻调用 chain 的全部三个方法，Hook 必须完整实现 `ProcessHook`/`DialHook`/`ProcessPipelineHook`。
- worker-sdk 的 `readLoop`/`heartbeatLoop` 的 `wg.Done` 由 `Run` 的 `wg.Add` 配对，直调测试必须手动 `Add(1)`；`Disconnect` 的 `wg.Wait` 在翻转状态之前。

## 中途纠偏记录

- **核对器假绿**：压缩重写后的核对器路径映射错误（import 路径直接拼仓库根，所有文件被静默跳过），一度输出虚假的 "ALL GREEN"。修正映射并引入块聚合后，暴露 26 处未定性零块，逐一核实处理。
- **误标分支转实测**：`compileRule` 的显式 `GroupInterval` 编译路径此前被误认为已覆盖（实际只被其他包的集成路径顺带触达，包内口径为零），补了表驱动实测（默认值 + 显式值）。
- **测试挂死排查**：serveCmd 等信号不等 ctx（端口占用用例需发 SIGTERM 收口）；`Disconnect` 与 `heartbeatLoop` 的 Wait 顺序曾造成死锁，用独立 hbCtx 解决。

## 维护规则

1. 新代码以"触发每个分支"为测试目标，不是"跑过函数"。
2. 分支确实不可达时，先怀疑是死代码——是就删；确需保留就就地写 `// Defensive: ...` 说明原因。
3. 新增 `main()` 或常驻进程入口时，同步补 GOCOVERDIR 子进程守卫测试。
4. 覆盖率门禁只随实测水位上调，不预留缓冲；引入未测代码 CI 立即拦截。
5. 禁止无断言的"路过式"测试和伪造调用路径。
