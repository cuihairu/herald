# KNOWN_UNCOVERABLE — 已登记的不可覆盖块

本文件登记 coverprofile 中**确实无法用测试覆盖**的零计数块，并写明原因。登记是验收契约的一部分：`tools/zero_check.py` 把这里的条目视为豁免，任何**不在本文件**且无就地定性注释的零块都会让核对器以非零码退出；`--gate 100` 门禁下，只有**本文件登记的块**才按已覆盖计（就地注释不算）。

登记格式（核对器解析）：`- \`<import路径>:<块起始行>\` — 原因`。

排除本文件登记项后的等效语句覆盖率为 **100%**（`go test ... -coverprofile` + `tools/covermerge.py` 合并子进程口径后的门禁 profile，`tools/zero_check.py coverage.merged.out --gate 100` 强制）。三个 `main()` 的成功路径入口块已由 `TestMainProcessSuccessPath`（`go build -cover` 子进程 + GOCOVERDIR 转储）实测非零并合并进门禁 profile，不再登记；唯一残余是各 `main()` 的 `os.Exit` 失败分支——exit 跳过 GOCOVERDIR 转储，是 Go 工具链原理性不可测路径。

**登记的行号以 go1.26.6（`go.mod` 钉定的最低工具链；CI 的 `go-version: '1.26'` 浮动版本恒 ≥ 它）发出的 coverprofile 为准。** Go 把 `if` 的条件与分支体切成独立的块，而块边界标注随工具链版本变化：go1.26 对单行 `if` 把分支体块也标在 `if` 那一行起（如 `main.go:49.37,51.3`），go1.27 起才改标到分支体首行。本地默认工具链是 go1.27.1，两边行号整体错一位——本台账一度按 go1.27 的行号登记，在 CI 的 go1.26 之下一条都匹配不上，台账形同虚设，`--gate 100` 立即变红。1.26 线内的补丁升级（1.26.2→1.26.6，2026-09-27 因 govulncheck 的 stdlib 漏洞修复抬版）不改块边界，已用整条门禁链复测实证。所以登记或核对前，用 `GOTOOLCHAIN=go1.26.6 go test -count=1 -race -coverpkg=./... -coverprofile=coverage.out ./...` 重新生成 profile，照它抄起始行，别按源码"看起来是哪行"猜。

**零块按完整块区间 `(文件, 起, 止)` 聚合，起始行相同的块不会互相掩盖。** 核对器曾按 `(文件, 起始行)` 聚合取 max：单行 `if` 的条件块与分支体块起始行相同（`x.go:71.8,71.48` 是条件、`x.go:71.48,74.3` 是分支体），恒被走过的条件块把同起始行的零块 max 成非零，八处真缺口被静默藏出门禁之外。核对器现与 `covermerge` 同口径按完整区间聚合，被掩盖的块重新现身后，本台账据此补齐了 handler_groups、serve 守卫与各 deadline 条目。

## api/handler_groups.go

> 本文件曾登记 `createGroup`/`getGroup` 里两处 `Manager.Get` 之后的 500 守卫（原 `handler_groups.go:71`、`:88`）——`Manager.Get` 只读内存快照、唯一错误是 `ErrNotFound`，当时判定为契约守卫。2026-09-27 按 `writeControl` 先例提为包级 seam `groupsGet`，由 `TestHandleGetGroupInternalErrorIs500` / `TestHandleCreateGroupLookupErrorIs500` 注入非 `ErrNotFound` 错误实测（500 与紧邻 404/409 均有断言），已移出台账。

> 对照：同一文件 `deleteGroup` 里那个形似的 500 分支**不是**死代码。`Manager.Delete` 把 store 的错误原样返回（不像 `Put`/`Reload` 那样包一层上下文），存储故障会真的走到那里，已由 `TestHandleGroupStoreFailureIs500` / `TestHandleGroupStoreFailureStays500OnRetry` 用注入的失败 store 实测覆盖。同形状的分支，一个可达一个不可达，差别在下游契约而不在代码写法。

## api/handler_surfaces.go

- `github.com/cuihairu/herald/api/handler_surfaces.go:82` — DELETE 臂里 `Unfollow` 之后转 422 的守卫。到达条件：handler 前置校验放行后 `Unfollow` 仍拒绝——但两者校验完全同口径（`checkIDAndChannel` 内部就是 `idPattern`（≡ `audience.ValidID`）+ channel 1-64 界，handler 已先行同判），`source` 是常量 `audience.SourceAdmin` 恒合法。两层校验互为镜像时恒不可达的契约守卫（同 `handler_sources.go:237` 先例）；若日后 `Unfollow` 新增校验维度，此处应随之变为可达并补 422 实测。行号 2026-10-08 随阶段③集成方对接批次（联系面绑定面落地）登记。

## api/handler_sources.go

- `github.com/cuihairu/herald/api/handler_sources.go:237` — `HandleSubscriptions` POST 臂里 `Toggle` 之后转 422 的守卫。到达条件：`checkSourceFields` 放行后注册表仍拒绝——但两者校验完全同口径（受众 id 模式、品类/渠道 1-64 字符，`validateRelation` 之外无其他约束），动作方来源是常量 `preference_center`，且开启路径走 `Subscribe`（强制可退订策略位，不存在 must-deliver 拒绝）。两层校验互为镜像时恒不可达的契约守卫；若日后注册表新增校验维度，此处应随之变为可达并补 422 实测。

## cmd/heraldd/main.go

- `github.com/cuihairu/herald/cmd/heraldd/main.go:60` — `if code := run(os.Args); code != 0` 的失败分支块（`os.Exit(code)`）。`os.Exit` 跳过 GOCOVERDIR 转储，任何以 exit 结尾的路径都无法留下覆盖数据。错误退出码语义已由 `run()`/`serveCmd` 返回码的单测覆盖（main 只是转发该返回码）。行号 2026-10-08 随文档一致性审计批次核对（原 49，main() 定义随批次 10-11 的 import 与注释增长整体下移到 :59，块起始行现 60）。

- `github.com/cuihairu/herald/cmd/heraldd/main.go:416` — §13.1 集成者命名空间注册表构建失败分支（`apps.NewRegistry` 报错即拒起）。到达条件：某个 apps 种子通过了 `config.Validate` 却在 `NewRegistry` 失败——不可能：Validate 内部调用的就是同一个构造器（config.go 的校验循环把同一批种子原样喂给 `apps.NewRegistry`），种子在 Validate 阶段就会被同一条校验拒掉（`main_cover_test.go` 错误表 `app scope invalid` 已实测拒起点在 Load）；到达本分支需要同一构造器对同一输入先过后拒。两次喂入仅顺序不同（Validate 按 map 迭代、main 按 sort 后列表），而构造器的全部约束都是逐 app 或全局集合性的，与顺序无关。行号 2026-10-07 随批次 11 增量六的审计流水常开 hunk 下移 1 行（原 400），同日增量七的回调 dispatcher 注册 hunk 再下移 10 行（411），2026-10-10 验收走查批的 auth 接线 hunk（AdminUser/SecretKey 透传）再下移 5 行（现 416）。

- `github.com/cuihairu/herald/cmd/heraldd/main.go:468` — 来源对账探测件构建失败分支（§8，`NewProbe` 报错即拒起）。到达条件：某个 telegram/wechatmp provider 通过了工厂创建、其探测件却构建失败——不可能：探测件对 provider config 的要求是工厂要求的严格子集（telegram 探测件要 `token` ⊂ 工厂的 `token`+`chat_id`；wechatmp 探测件要 `app_id`+`app_secret` ⊂ 工厂的 `app_id`+`app_secret`+`template_id`），而扫描循环只在工厂全部成功之后运行。探测件配置残缺的拒起在 provider 创建阶段先行发生（`main_cover_test.go` 错误表已实测）。行号 2026-10-07 随批次 10 配置透传 hunk 下移 4 行（原 418），同日批次 9 的投递策略构建块再下移 12 行（434），批次 11 的 app 注册表构建块与 sort import 再下移 11 行（445），同日增量六的审计流水常开 hunk 再下移 1 行（446），增量七的回调 dispatcher 注册与 emitter 接线 hunk 再下移 17 行（463），2026-10-10 验收走查批的 auth 接线 hunk 再下移 5 行（现 468，合计自 418）

> 本文件曾登记 `serveCmd`/`workerCmd` 两个注册循环里的 `manager.RegisterProvider` 重复名守卫（原 `main.go:130`、`main.go:381`）——按结构不可达（factories 与 providers 分属两个 map、`cfg.Providers` 键唯一）。2026-10-01 按 `writeControl` 先例提为包级 seam `registerProvider`，由 `TestDuplicateNameGuardAbortsRegistration` 注入失败实测两处 return 1 分支（均在绑端口/起 worker 前退出），已移出台账。

## providers/builtin/wechatmp/wechatmp.go

- `github.com/cuihairu/herald/providers/builtin/wechatmp/wechatmp.go:312` — `GetToken` 写锁内双检的命中分支。到达条件：并发调用方的读检查落在「缓存已过期且无人持写锁」的窗口内，且其写锁申请排在刷新胜者之后——窗口是读检查到加锁之间的微秒级间隙，能否命中完全取决于调度，确定性构造不可行（RWMutex 下读者会被持写锁者挡住，无法从外部制造该窗口）。分支**行为**已由 `TestTokenCacheConcurrentSingleFetch` 与 `TestTokenCacheDoubleCheckUnderContention` 每轮确定性断言：并发下恰好一次取 token、败者复用胜者结果；语句命中是偶发的（部分轮次的 profile 里该块非零，此时本条目自然不参与豁免）。

## core/audience/reconcile.go

- `github.com/cuihairu/herald/core/audience/reconcile.go:127` — 对账纠正扫尾里 `TerminateFor` 的失败分支。同 `sources.go:150`:扫尾只迭代 subscription 关系（must-deliver 拒绝不可达），剩余到达条件是 `RelationsByType` 快照到终止之间的并发删除——调度窗口；守卫保证单条失败不中断本轮纠正。

- `github.com/cuihairu/herald/core/audience/reconcile.go:118` — 对账纠正路径里 `InvalidateFor` 的失败分支。到达条件：快照读到 `active` 之后、调用失效之前的窗口内，另一个入口（并发的取关事件或对账轮次）已把同一联系面置为 `invalid`——窗口是微秒级读写间隙，能否命中完全取决于调度，确定性构造不可行（与 wechatmp `GetToken` 双检同型）。分支语义（已失效的联系面不重复纠正）由 `Unfollow` 幂等测试与对账快照过滤测试共同保证。

## core/audience/sources.go

- `github.com/cuihairu/herald/core/audience/sources.go:140` — `Unfollow` 里 `InvalidateFor` 的失败分支。守卫刚读过联系面状态（非 `invalid` 才调用），到达条件是读与调用之间落进一次并发失效——调度窗口，同上型。

- `github.com/cuihairu/herald/core/audience/sources.go:150` — `Unfollow` 全停扫尾里 `TerminateFor` 的失败分支。扫尾只迭代 subscription 关系，而 `Subscribe` 强制可退订策略位，must-deliver 拒绝不可达；剩余到达条件是快照到终止之间一次并发删除（`ErrRelationNotFound`）——调度窗口。守卫语义（单条失败不中断全停）由 `/stop` 全停测试保证。

## core/feeds/rss.go

- `github.com/cuihairu/herald/core/feeds/rss.go:73` — `RenderRSS` 里 `xml.MarshalIndent` 的失败分支。被序列化的 `rssXML` 只含 string/bool 字段与同构嵌套 struct/slice；`encoding/xml` 对字符串值里的非法 XML 字符（`\x00`、孤立 `\xff`、U+FFFE）是**转义成字符引用**而不是报错（2026-10-07 用 go1.26.6 实测：含全部三类字符的文档 `MarshalIndent` 恒 nil），编码器唯一错误源是底层 writer——这里是 `bytes.Buffer`，写入恒成功。恒不可达的防御分支，不需要任何测试。

## core/websocket/server.go

- `github.com/cuihairu/herald/core/websocket/server.go:370` — `state.conn.SetWriteDeadline` 的失败分支。gorilla v1.5.3 的 `SetWriteDeadline` 是纯字段赋值 `c.writeDeadline = t; return nil`，**任何**输入下都不返回错误（与 `SetReadDeadline` 不同，后者才转发给 `net.Conn`）。这一行是恒 nil，不需要任何测试；真实的发送失败在紧随其后的 `WriteMessage` 里报出，那里已有实测覆盖。

## examples/quickstart/main.go

- `github.com/cuihairu/herald/examples/quickstart/main.go:23` — `if err := run(ctx); err != nil` 失败分支块（`fmt.Fprintln` + `os.Exit(1)`），同上：exit 路径跳过 GOCOVERDIR 转储。失败语义已由 `run()` 层的 `TestRunCanceledContext` 覆盖。

## worker-sdk/go/client.go

- `github.com/cuihairu/herald/worker-sdk/go/client.go:275` — `writeControl` 里 `conn.SetWriteDeadline` 的失败分支。gorilla v1.5.3 的 `SetWriteDeadline` 是纯字段赋值 `c.writeDeadline = t; return nil`，**任何**输入下都不会返回错误（与 `SetReadDeadline` 不同，后者才转发给 `net.Conn`）。真实传输失败由 `WriteMessage` 返回，已由 `TestRegisterWriteControlFailure` 覆盖。

> 登记行号会被注释改动顶掉：本文件里 `client.go:275`、`server.go:370` 各块上方都压着多行注释，增删一行整块位移，而 `zero_check` 按 `文件:起始行` 匹配，一对上路块就退回"未定性"并让门禁变红——所以**改动这些块附近的注释后要重跑门禁**（且必须用 go1.26.6，见页首纪律），别只看测试是否通过。

> 对照：`client.go:175` 那个**形状看起来一样**的 `SetReadDeadline` 失败分支一度也躺在本台账里，其实是可达的——`writeControl` 是包级变量，测试可以注入"报告成功但先 Close 掉连接"的实现，它转发的 `net.Conn` 随即返回 `use of closed network connection`。已由 `TestRegisterReadDeadlineFailsOnClosedConn` 实测覆盖并移出台账。教训是别按"形状像"登记：先查被调方在目标版本下的真实实现（`SetWriteDeadline` 恒 nil、`SetReadDeadline` 转发），再判断有没有可达路径。同款模式已应用两次：`cmd/heraldd/main.go` 的 `registerRemoteWorker` 读 ack 的 `SetReadDeadline` 失败分支（原登记 `main.go:556`）与本条目同族——本文件原登记的 `client.go:197`（清 ack 截止时间）已于 2026-09-27 各提出包级 seam `setReadDeadline`、由 `TestRegisterRemoteWorkerReadDeadlineFailure` / `TestRegisterClearDeadlineFailure` 注入失败实测覆盖并移出台账。"缺 seam"从来不是死因，是待办。

## worker-sdk/go/example/main.go

- `github.com/cuihairu/herald/worker-sdk/go/example/main.go:35` — `if err := run(ctx, demoConfig()); err != nil` 失败分支块（`fmt.Printf` + `os.Exit(1)`），同上：exit 路径跳过 GOCOVERDIR 转储。失败语义已由 `run()` 层的 `TestRunFailsFastWhenCoreUnreachable` 覆盖。
