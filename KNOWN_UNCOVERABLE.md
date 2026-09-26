# KNOWN_UNCOVERABLE — 已登记的不可覆盖块

本文件登记 coverprofile 中**确实无法用测试覆盖**的零计数块，并写明原因。登记是验收契约的一部分：`tools/zero_check.py` 把这里的条目视为豁免，任何**不在本文件**且无就地定性注释的零块都会让核对器以非零码退出；`--gate 100` 门禁下，只有**本文件登记的块**才按已覆盖计（就地注释不算）。

登记格式（核对器解析）：`- \`<import路径>:<块起始行>\` — 原因`。

排除本文件登记项后的等效语句覆盖率为 **100%**（`go test ... -coverprofile` + `tools/covermerge.py` 合并子进程口径后的门禁 profile，`tools/zero_check.py coverage.out --gate 100` 强制）。三个 `main()` 的成功路径入口块已由 `TestMainProcessSuccessPath`（`go build -cover` 子进程 + GOCOVERDIR 转储）实测非零并合并进门禁 profile，不再登记；唯一残余是各 `main()` 的 `os.Exit` 失败分支——exit 跳过 GOCOVERDIR 转储，是 Go 工具链原理性不可测路径。

## cmd/heraldd/main.go

- `github.com/cuihairu/herald/cmd/heraldd/main.go:49` — `if code := run(os.Args); code != 0` 的失败分支块（`os.Exit(code)`）。`os.Exit` 跳过 GOCOVERDIR 转储，任何以 exit 结尾的路径都无法留下覆盖数据。错误退出码语义已由 `run()`/`serveCmd` 返回码的单测覆盖（main 只是转发该返回码）。

## providers/builtin/wechatmp/wechatmp.go

- `github.com/cuihairu/herald/providers/builtin/wechatmp/wechatmp.go:312` — `GetToken` 写锁内双检的命中分支。到达条件：并发调用方的读检查落在「缓存已过期且无人持写锁」的窗口内，且其写锁申请排在刷新胜者之后——窗口是读检查到加锁之间的微秒级间隙，能否命中完全取决于调度，确定性构造不可行（RWMutex 下读者会被持写锁者挡住，无法从外部制造该窗口）。分支**行为**已由 `TestTokenCacheConcurrentSingleFetch` 与 `TestTokenCacheDoubleCheckUnderContention` 每轮确定性断言：并发下恰好一次取 token、败者复用胜者结果；仅有语句命中是偶发的。

## examples/quickstart/main.go

- `github.com/cuihairu/herald/examples/quickstart/main.go:23` — `if err := run(ctx); err != nil` 失败分支块（`os.Exit(1)`），同上：exit 路径跳过 GOCOVERDIR 转储。失败语义已由 `run()` 层的 `TestRunCanceledContext` 覆盖。

## worker-sdk/go/example/main.go

- `github.com/cuihairu/herald/worker-sdk/go/example/main.go:35` — `if err := run(ctx, demoConfig()); err != nil` 失败分支块（`os.Exit(1)`），同上：exit 路径跳过 GOCOVERDIR 转储。失败语义已由 `run()` 层的 `TestRunFailsFastWhenCoreUnreachable` 覆盖。
