# 覆盖率攻坚总结（2026-09）

本轮任务：把 Herald 全仓测试覆盖率提到**行覆盖 + 分支覆盖双满**，以测试工具实测为准，禁止凑数的假覆盖；确实不可达的分支/防御性代码逐处记录原因；CI 门禁同步抬到实测水位。

最终结果：**语句覆盖率 98.7%，CI 门禁 98.7% 已生效并通过**。剩余 1.3% 全部为就地定性的不可达/防御分支与三个不可测的进程入口 `main()`，逐处清单见[测试覆盖率口径](./architecture/coverage.md)。

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
3. 覆盖率门禁只随实测水位上调，不预留缓冲；引入未测代码 CI 立即拦截。
4. 禁止无断言的"路过式"测试和伪造调用路径。
