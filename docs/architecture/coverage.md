# 测试覆盖率口径

本文说明 Herald 的覆盖率统计口径、当前实测水位、CI 门禁，以及残余零块的逐处定性。本轮攻坚的过程与方法论见[覆盖率攻坚总结（2026-09）](../coverage-initiative-2026-09.md)。原则只有一条：**以测试工具实测为准，不写凑数字的假覆盖**。

## 口径定义

- 统计工具为 `go test -coverprofile`，以 Go coverprofile 的**基本块（statement block）**为最小单位，而不是行。
- Go 编译器把 `if`/`for`/`select` 的每个出口、每个 `case` 都切成独立的基本块，每块记录执行次数。**所有块的计数非零等价于分支覆盖已满**：一个条件分支只要两侧都真实执行过，两个块都会非零；任一侧没走到，就会留下一个计数为零的块。
- 因此我们的验收标准是：**profile 中每一个计数为零的块，要么补上确定性触发它的测试，要么在源码就地注释说明它为什么不可达**。没有第三种处理。
- CI 口径带 `-coverpkg=./...`：所有包的语句都计入 profile，新增一个没有测试的包会直接拉低门禁，而不是被静默排除在统计之外。
- 禁止用无断言的"路过式"测试或伪造调用路径制造覆盖数字。
- **子进程口径（补充）**：`main()` 不在 `go test` 语句内执行，go-test 口径永远是零。这类入口用 `go build -cover` 把包编译成带插桩的二进制，作为子进程在 `GOCOVERDIR` 下真实运行，断言 `go tool covdata textfmt` 转储中 `main()` 区间内所有块非零。`os.Exit` 会跳过 profile 转储，因此成功路径必须经 `return` 退出（三个入口的 `main` 均已如此改写），失败路径 `os.Exit(n)` 属于 Go 工具链原理性不可测，见下文。

## 实测水位与门禁

| 项目 | 值 |
| --- | --- |
| 语句覆盖率（CI 口径，`go test -coverpkg=./... -coverprofile` ./...） | **99.5%** |
| CI 门禁 | `.github/workflows/ci.yml` 的 `Enforce coverage gate`，低于 99.5% 直接失败 |
| 残余零块 | **仅 6 块**：三个入口包的 `main()` 函数块（见下节），全部经子进程口径实测覆盖，并就地注释定性 |

覆盖率每提高都只能通过两种方式：新增真实触发路径的测试，或删除死代码。任何"不可达"定性都必须在零块旁边就地留下注释（关键词 `Defensive` / `Unreachable` / `not callable` / `Coverage note`），说明该分支为何不会发生、保留它的价值是什么（通常是为了未来重构时大声失败，而不是静默吞掉）。

零块核对器已入库为 [`tools/zero_check.py`](../../tools/zero_check.py)：跑完覆盖率测试后执行 `python3 tools/zero_check.py coverage.out`，存在未定性零块时以非零码退出，可作为本地验收闸门。

## 进程入口 main()：双口径实测

`main()` 只能由 OS 启动进程调用，`go test` 永远测不到；而 `main` 里的 `os.Exit(n)` 会跳过 GOCOVERDIR 转储。三个入口做了同样的处理：

1. `main` 改写为成功路径 `return`（`cmd/heraldd` 是 `if code := run(os.Args); code != 0 { os.Exit(code) }`），只有失败才退出进程；
2. 各包 `main_cover_test.go` 用 `go build -cover -coverpkg=./...` 编译子进程，真实运行（heraldd 起 HTTP 服务后 SIGTERM 优雅退出；quickstart 同步派发后自然退出；worker-sdk 示例对 `ws://localhost:8081` 完成注册握手后 SIGTERM）；
3. 断言 GOCOVERDIR 转储中 `main()` 区间内除 `os.Exit` 行（`mainExitAllow`，工具链原理性不可测）之外每个块计数非零。

因此这三个包的 `main()` 在 go-test 口径下仍显示为零块（就地 `Coverage note` / `not callable` 注释定性），但**成功路径已被子进程口径真实测过**——不是"测不到所以算了"。

## 原防御分支的归处

早期清单里几十处"不可达/防御分支"，经三轮攻坚后只剩三种结局，profile 中已无一处防御性零块：

1. **直调实测**：包级函数直接调用，不走生产入口（`compileRule` 六类字段错误、`pkcs7Unpad` 非法输入、`runProgram` 超时与取消、`evalProgram` seam 打桩超时等）。
2. **重构消灭**：分支本身是坏结构的产物，直接改代码删掉——
   - `Manager.Deliver` 的 `IsEnabled`+`GetProvider` 双锁竞态分支 → 合并为单锁 `lookupEnabled`，错误分支可实测；
   - `Engine.Put` 把 `compileRule` 前置到 `Validate` 之前，编译错误分支从"不可达守卫"变为可实测；
   - `runProgram` 的非 bool 返回值分支 → 单值断言 `r.out.(bool)`（不变量破坏必须 panic）。
3. **恒 nil 显式接受**：`x, _ := f(...)` 加注释说明为什么错误恒为 nil——gorilla 的 `SetWriteDeadline` 只记录时间戳；对可序列化结构的 `json.Marshal`；回退式限流器工厂；base64 密码摘要；SHA-256 摘要上的 `aes.NewCipher`；类型断言式 `parseConfig`。若契约变化，注释要求恢复为显式错误处理。

注解关键词 `Defensive` / `Unreachable` / `not callable` / `Coverage note` 在源码中检索即可定位每一处说明。

## 如何维护这份水位

1. 给新代码写测试时以「触发每个分支」为目标，而不是「跑过函数」。
2. 若某分支确实不可达，先怀疑它是不是死代码——是就删掉；确需保留（API 兼容、未来重构防静默），就地写 `// Defensive: ...` 并说明原因。
3. 新增 `main()` 或常驻进程入口时，同步补 GOCOVERDIR 子进程守卫测试。
4. 阈值只随实测水位上调，不预留缓冲；任何人引入未测代码，CI 会立即拦下。
