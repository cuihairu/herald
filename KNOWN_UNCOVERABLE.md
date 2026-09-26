# KNOWN_UNCOVERABLE — 已登记的不可覆盖块

本文件登记 coverprofile 中**确实无法用测试覆盖**的零计数块，并写明原因。登记是验收契约的一部分：`tools/zero_check.py` 把这里的条目视为豁免，任何**不在本文件**且无就地定性注释的零块都会让核对器以非零码退出；`--gate 100` 门禁下，只有**本文件登记的块**才按已覆盖计（就地注释不算）。

登记格式（核对器解析）：`- \`<import路径>:<块起始行>\` — 原因`。

排除本文件登记项后的等效语句覆盖率为 **100%**（`go test ... -coverprofile` + `tools/covermerge.py` 合并子进程口径后的门禁 profile，`tools/zero_check.py coverage.out --gate 100` 强制）。三个 `main()` 的成功路径入口块已由 `TestMainProcessSuccessPath`（`go build -cover` 子进程 + GOCOVERDIR 转储）实测非零并合并进门禁 profile，不再登记；唯一残余是各 `main()` 的 `os.Exit` 失败分支——exit 跳过 GOCOVERDIR 转储，是 Go 工具链原理性不可测路径。

**登记的是块的起始行，不是 `if` 所在行。** Go 把 `if` 的条件与两侧切成独立的块：条件表达式属于**成功路径**块（起始行 = `if` 行），失败分支体是另一个块（起始行 = 分支体第一行）。三个 `main()` 里 `os.Exit` 都在分支体首行，所以登记 `os.Exit` 那一行而不是 `if` 那一行——登记成 `if` 行等于把已被子进程合并救活的成功路径块登记成"不可覆盖"，真正的 `os.Exit` 块反而裸露在门禁外。

## api/handler_groups.go

- `github.com/cuihairu/herald/api/handler_groups.go:72` — `createGroup` 中 `else if !errors.Is(err, groups.ErrNotFound)` 的 500 分支。到达条件要求 `groups.Manager.Get` 返回一个**非** `ErrNotFound` 的错误，但 `Manager.Get` 只读内存快照、找不到时**只**返回 `ErrNotFound`（`core/groups/manager.go` 的 `Get` 没有其他错误来源，也不碰 store）。给定当前 `Manager` 契约该分支不可达；保留它是防御 `Get` 的契约日后放宽——否则一次真实故障会被静默当成"不存在"而放过重复创建。
- `github.com/cuihairu/herald/api/handler_groups.go:89` — `getGroup` 中 `if err != nil` 的 500 分支。依据同上：`Manager.Get` 只能给出 `ErrNotFound`，紧邻的 404 已经处理了这种情况。保留理由同上——宁可返回 500，也不要在契约变化后把故障报成"群组不存在"。

> 对照：同一文件 `deleteGroup` 里那个形似的 500 分支**不是**死代码。`Manager.Delete` 把 store 的错误原样返回（不像 `Put`/`Reload` 那样包一层上下文），存储故障会真的走到那里，已由 `TestHandleGroupStoreFailureIs500` / `TestHandleGroupStoreFailureStays500OnRetry` 用注入的失败 store 实测覆盖。同形状的分支，一个可达一个不可达，差别在下游契约而不在代码写法。

## cmd/heraldd/main.go

- `github.com/cuihairu/herald/cmd/heraldd/main.go:50` — `if code := run(os.Args); code != 0` 的失败分支块（`os.Exit(code)`）。`os.Exit` 跳过 GOCOVERDIR 转储，任何以 exit 结尾的路径都无法留下覆盖数据。错误退出码语义已由 `run()`/`serveCmd` 返回码的单测覆盖（main 只是转发该返回码）。
- `github.com/cuihairu/herald/cmd/heraldd/main.go:131` — `serve` 里 `manager.RegisterProvider` 的重复名守卫（`// Defensive: the duplicate-name guard cannot fire`）。配置遍历的是 `map[string]ProviderConfig`，键即 provider 名，map 本身保证每个名字只出现一次；且此处 manager 全新、builtin 只注册工厂。构造重复名需要同一 map 键出现两次，与数据结构矛盾。
- `github.com/cuihairu/herald/cmd/heraldd/main.go:382` — `serveCmd` 里同一守卫的第二处副本，依据同上（该处 manager 由 `serveCmd` 新建）。
- `github.com/cuihairu/herald/cmd/heraldd/main.go:557` — `registerRemoteWorker` 里 `conn.SetReadDeadline` 的失败分支。它转发给底层 `net.Conn`，只在连接已关闭时失败；而要走到它必须先通过上一行的 `WriteMessage`，那一步在同一个已关闭连接上必然先失败并 return。要构造"写得出去但设不了 deadline"的连接需要在两者之间加 seam，此处没有。

## providers/builtin/wechatmp/wechatmp.go

- `github.com/cuihairu/herald/providers/builtin/wechatmp/wechatmp.go:312` — `GetToken` 写锁内双检的命中分支。到达条件：并发调用方的读检查落在「缓存已过期且无人持写锁」的窗口内，且其写锁申请排在刷新胜者之后——窗口是读检查到加锁之间的微秒级间隙，能否命中完全取决于调度，确定性构造不可行（RWMutex 下读者会被持写锁者挡住，无法从外部制造该窗口）。分支**行为**已由 `TestTokenCacheConcurrentSingleFetch` 与 `TestTokenCacheDoubleCheckUnderContention` 每轮确定性断言：并发下恰好一次取 token、败者复用胜者结果；仅有语句命中是偶发的。

## core/websocket/server.go

- `github.com/cuihairu/herald/core/websocket/server.go:371` — `state.conn.SetWriteDeadline` 的失败分支。gorilla v1.5.3 的 `SetWriteDeadline` 是纯字段赋值 `c.writeDeadline = t; return nil`，**任何**输入下都不返回错误（与 `SetReadDeadline` 不同，后者才转发给 `net.Conn`）。这一行是恒 nil，不需要任何测试；真实的发送失败在紧随其后的 `WriteMessage` 里报出，那里已有实测覆盖。

## examples/quickstart/main.go

- `github.com/cuihairu/herald/examples/quickstart/main.go:24` — `if err := run(ctx); err != nil` 失败分支块（`fmt.Fprintln` + `os.Exit(1)`），同上：exit 路径跳过 GOCOVERDIR 转储。失败语义已由 `run()` 层的 `TestRunCanceledContext` 覆盖。

## worker-sdk/go/client.go

- `github.com/cuihairu/herald/worker-sdk/go/client.go:198` — `conn.SetReadDeadline(time.Time{})`（清 ack 截止时间）的失败分支。gorilla 的 `SetReadDeadline` 直接转发给底层 `net.Conn`，只在**客户端自己的** conn 已关闭时失败；而 register 在读完 ack 与清 deadline 之间不关闭任何连接（此处也无包级 seam 可注入）。要到它必须先在那个位置加 seam，属于改设计而非补测试。
- `github.com/cuihairu/herald/worker-sdk/go/client.go:276` — `writeControl` 里 `conn.SetWriteDeadline` 的失败分支。gorilla v1.5.3 的 `SetWriteDeadline` 是纯字段赋值 `c.writeDeadline = t; return nil`，**任何**输入下都不会返回错误（与 `SetReadDeadline` 不同，后者才转发给 `net.Conn`）。真实传输失败由 `WriteMessage` 返回，已由 `TestRegisterWriteControlFailure` 覆盖。

> 登记行号会被注释改动顶掉：本文件里 `client.go:198` 的块起始行曾随上方注释增删从 `:195` 漂到 `:198`，`client.go:276` 从 `:271` 漂到 `:276`，`main.go:557` 从 `:556` 漂到 `:557`，`server.go:371` 从 `:370` 漂到 `:371`。`zero_check` 按 `文件:起始行` 匹配，行号一对不上块就退回"未定性"并让门禁变红——所以**改动这两个文件里 `os.Exit`/deadline 块附近的注释后要重跑门禁**，别只看测试是否通过。

> 对照：`client.go:175` 那个**形状看起来一样**的 `SetReadDeadline` 失败分支一度也躺在本台账里，其实是可达的——`writeControl` 是包级变量，测试可以注入"报告成功但先 Close 掉连接"的实现，它转发的 `net.Conn` 随即返回 `use of closed network connection`。已由 `TestRegisterReadDeadlineFailsOnClosedConn` 实测覆盖并移出台账。教训是别按"形状像"登记：先查被调方在目标版本下的真实实现（`SetWriteDeadline` 恒 nil、`SetReadDeadline` 转发），再判断有没有可达路径。

## worker-sdk/go/example/main.go

- `github.com/cuihairu/herald/worker-sdk/go/example/main.go:36` — `if err := run(ctx, demoConfig()); err != nil` 失败分支块（`fmt.Printf` + `os.Exit(1)`），同上：exit 路径跳过 GOCOVERDIR 转储。失败语义已由 `run()` 层的 `TestRunFailsFastWhenCoreUnreachable` 覆盖。
