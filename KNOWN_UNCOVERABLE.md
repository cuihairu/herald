# KNOWN_UNCOVERABLE — 已登记的不可覆盖块

本文件登记 coverprofile 中**确实无法用测试覆盖**的零计数块，并写明原因。登记是验收契约的一部分：`tools/zero_check.py` 把这里的条目视为豁免，任何**不在本文件**且无就地定性注释的零块都会让核对器以非零码退出。

登记格式（核对器解析）：`- \`<import路径>:<块起始行>\` — 原因`。

排除已登记项后的语句覆盖率为 **100%**（go tool cover -func 口径 99.5%，差额即下表各块）。

## cmd/heraldd/main.go

- `github.com/cuihairu/herald/cmd/heraldd/main.go:47` — `main()` 函数体入口块。main 只能由 OS 启动进程调用，`go test` 二进制永远不会执行它；此为 Go 工具链原理性限制。成功路径已由 `TestMainProcessSuccessPath`（`go build -cover` 子进程 + GOCOVERDIR 转储断言）实测覆盖，只是不计入 go-test profile。
- `github.com/cuihairu/herald/cmd/heraldd/main.go:48` — `if code := run(os.Args); code != 0` 的失败分支块（`os.Exit(code)`）。`os.Exit` 跳过 GOCOVERDIR 转储，任何以 exit 结尾的路径都无法留下覆盖数据；同一原理性限制。错误退出码语义已由 `run()`/`serveCmd` 返回码的单测覆盖（main 只是转发该返回码）。

## examples/quickstart/main.go

- `github.com/cuihairu/herald/examples/quickstart/main.go:20` — `main()` 函数体入口块，同上：进程入口不可被 `go test` 调用；成功路径已由该包 `TestMainProcessSuccessPath` 子进程口径实测。
- `github.com/cuihairu/herald/examples/quickstart/main.go:23` — `if err := run(ctx); err != nil` 失败分支块（`os.Exit(1)`），同上：exit 路径跳过 GOCOVERDIR 转储。失败语义已由 `run()` 层的 `TestRunCanceledContext` 覆盖。

## worker-sdk/go/example/main.go

- `github.com/cuihairu/herald/worker-sdk/go/example/main.go:29` — `main()` 函数体入口块，同上：进程入口不可被 `go test` 调用；成功路径已由该包 `TestMainProcessSuccessPath` 子进程口径实测。
- `github.com/cuihairu/herald/worker-sdk/go/example/main.go:35` — `if err := run(ctx, demoConfig()); err != nil` 失败分支块（`os.Exit(1)`），同上：exit 路径跳过 GOCOVERDIR 转储。失败语义已由 `run()` 层的 `TestRunFailsFastWhenCoreUnreachable` 覆盖。
