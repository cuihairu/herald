# 测试覆盖率口径

本文说明 Herald 的覆盖率统计口径、当前实测水位、CI 门禁，以及残余零块的逐处定性。本轮攻坚的过程与方法论见[覆盖率攻坚总结（2026-09）](../coverage-initiative-2026-09.md)，任务链最终状态见[覆盖率任务收官报告（2026-09）](../coverage-final-report-2026-09.md)。原则只有一条：**以测试工具实测为准，不写凑数字的假覆盖**。

## 口径定义

- 统计工具为 `go test -coverprofile`，以 Go coverprofile 的**基本块（statement block）**为最小单位，而不是行。
- Go 编译器把 `if`/`for`/`select` 的每个出口、每个 `case` 都切成独立的基本块，每块记录执行次数。**所有块的计数非零等价于分支覆盖已满**：一个条件分支只要两侧都真实执行过，两个块都会非零；任一侧没走到，就会留下一个计数为零的块。
- 因此我们的验收标准是：**profile 中每一个计数为零的块，要么补上确定性触发它的测试，要么按两条通道之一如实登记原因**——源码就地定性注释（`Defensive` / `Unreachable` / `not callable` / `Coverage note`），或仓库根 [`KNOWN_UNCOVERABLE.md`](https://github.com/cuihairu/herald/blob/main/KNOWN_UNCOVERABLE.md) 登记表。没有第三种处理。排除已登记不可达项后，语句覆盖率为 100%。
- CI 口径带 `-coverpkg=./...`：所有包的语句都计入 profile，新增一个没有测试的包会直接拉低门禁，而不是被静默排除在统计之外。
- 禁止用无断言的"路过式"测试或伪造调用路径制造覆盖数字。
- **子进程口径（补充）**：`main()` 不在 `go test` 语句内执行，go-test 口径永远是零。这类入口用 `go build -cover` 把包编译成带插桩的二进制，作为子进程在 `GOCOVERDIR` 下真实运行，断言 `go tool covdata textfmt` 转储中 `main()` 区间内所有块非零。`os.Exit` 会跳过 profile 转储，因此成功路径必须经 `return` 退出（三个入口的 `main` 均已如此改写），失败路径 `os.Exit(n)` 属于 Go 工具链原理性不可测，见下文。
- **门禁 profile 是合并口径**：子进程守卫测试在 `HERALD_MAIN_COVERDIR` 下持久化各自的 textfmt 转储，[`tools/covermerge.py`](https://github.com/cuihairu/herald/blob/main/tools/covermerge.py) 把这些**实测计数**合并回 go-test profile——入口块从"登记豁免"升级为真实非零。合并器对假数据硬失败：空 dump、dump 中入口文件（按 basename == `main.go` 识别）无任何非零块、dump 出现主 profile 没有的块（工具链/代码漂移），任一命中即拒绝合并退出。

## 实测水位与门禁

| 项目 | 值 |
| --- | --- |
| 语句覆盖率（go-test 口径原始值） | 99.72%（5022/5036，2026-10-01 实测；此前 2026-09-28 为 99.64%（5019/5037）） |
| 语句覆盖率（**门禁口径**：合并三个 `main()` 子进程实测后） | **99.86%**（5029/5036，2026-10-01 复测（provider 重复名守卫两处经 seam 实测，零块 7→5）；2026-09-28 为 99.78%（5026/5037，影子统计切片，+7 条语句全命中）；期 5 收官时为 99.7%，功能代码增长摊低后，台账条目经 seam 逐轮升格实测（`main.go:556`、`client.go:197`、`handler_groups.go:71/:88`、`main.go:130/:381`），回到并越过原水位；门禁等效口径不受影响） |
| CI 门禁 | `tools/zero_check.py coverage.merged.out --gate 100`：排除 [`KNOWN_UNCOVERABLE.md`](https://github.com/cuihairu/herald/blob/main/KNOWN_UNCOVERABLE.md) 登记块后等效语句覆盖率必须为 **100%**，且不存在未定性零块 |
| 残余零块 | **5 块**（2026-10-01 go1.26.6 实测，全部命中台账；上一轮 2026-09-28 为 7 块）。台账共登记 **6 条**（原 8 条）——累计六处登记已按 `writeControl` 先例提为包级 seam、由注入失败用例实测升格并移出台账：`cmd/heraldd` `registerRemoteWorker`（原 `main.go:556`）、worker-sdk `register` 清 ack 截止时间（原 `client.go:197`）、api `createGroup`/`getGroup` 的 `Manager.Get` 后 500 守卫（原 `handler_groups.go:71/:88`，seam 为 `groupsGet`）、2026-10-01 `cmd/heraldd` 两处 provider 重复名守卫（原 `main.go:130/:381`，seam 为 `registerProvider`）；其余条目中 wechatmp 写锁双检命中块偶发走到非零，非零轮次自然不参与豁免：3 块 `main()` 的 `os.Exit` 失败分支、2 块 gorilla `SetWriteDeadline` 恒 nil、1 块双检命中 |
| **前端分支覆盖（dashboard/）** | **100%**（262/262，2026-09-28 实测，规则页新增"影子命中"列的 `?? 0` 两个分支两侧均已测；补齐该列前的 2026-09-27 轮为 260/260，再上期为 97.69%，补齐 6 个分支后满口径）。CI 门禁阈值 `branches: 100` 与实测水位一致，**不含任何豁免** |

覆盖率每提高都只能通过两种方式：新增真实触发路径的测试，或删除死代码。任何"不可达"定性都必须在零块旁边就地留下注释（关键词 `Defensive` / `Unreachable` / `not callable` / `Coverage note`），说明该分支为何不会发生、保留它的价值是什么（通常是为了未来重构时大声失败，而不是静默吞掉）。

零块核对器已入库为 [`tools/zero_check.py`](https://github.com/cuihairu/herald/blob/main/tools/zero_check.py)：跑完覆盖率测试后执行 `python3 tools/zero_check.py coverage.out --gate 100`，零块按"就地注释或 KNOWN_UNCOVERABLE 登记"豁免，未定性零块以非零码退出；`--gate 100` 进一步要求"排除台账登记块后的等效语句覆盖率 ≥ 100%"——只做就地注释而不进台账的零块会拉低等效值、打红门禁，因此**每个豁免都必须走台账、留下书面原因**。

## 进程入口 main()：双口径实测

`main()` 只能由 OS 启动进程调用，`go test` 永远测不到；而 `main` 里的 `os.Exit(n)` 会跳过 GOCOVERDIR 转储。三个入口做了同样的处理：

1. `main` 改写为成功路径 `return`（`cmd/heraldd` 是 `if code := run(os.Args); code != 0 { os.Exit(code) }`），只有失败才退出进程；
2. 各包 `main_cover_test.go` 用 `go build -cover -coverpkg=./...` 编译子进程，真实运行（heraldd 起 HTTP 服务后 SIGTERM 优雅退出；quickstart 同步派发后自然退出；worker-sdk 示例对 `ws://localhost:8081` 完成注册握手后 SIGTERM）；
3. 断言 GOCOVERDIR 转储中 `main()` 区间内除 `os.Exit` 行（`mainExitAllow`，工具链原理性不可测）之外每个块计数非零；
4. 设了 `HERALD_MAIN_COVERDIR` 时，把 textfmt 转储持久化出来，由 `tools/covermerge.py` 合并进门禁 profile——入口块因此以**实测非零**进入门禁统计，不再依赖登记豁免。

因此这三个包的 `main()` 在 go-test 口径下仍是零块，但合并口径下成功路径全部实测非零；残余是各 `main()` 的 `os.Exit` 失败分支——exit 跳过 GOCOVERDIR 转储，无法留下任何覆盖数据，已在台账登记。

## 台账登记的两条纪律

**登记行号 = 块的起始行，且以 go1.26.6（`go.mod` 钉定的最低工具链，CI 的 `go-version: '1.26'` 浮动版本恒 ≥ 它）发出的 coverprofile 为准。** Go 把 `if` 的条件与分支体切成独立块，而块边界标注随工具链版本漂移：go1.26 对单行 `if` 把分支体块也标在 `if` 那一行起（如 `main.go:49.37,51.3`），go1.27 起才改标到分支体首行——本地默认 go1.27.1 与 CI 整体错一行，照本地 profile 抄行号登记，在 CI 的 go1.26 之下一条都匹配不上，`--gate 100` 立即变红。同时**零块按完整块区间 `(文件, 起, 止)` 聚合**（核对器与 covermerge 同口径）：若按起始行聚合取 max，单行 `if` 恒走过的条件块会把同起始行的零块 max 成非零，八处真缺口就是这样被藏出门禁的。完整纪律见[台账页首](https://github.com/cuihairu/herald/blob/main/KNOWN_UNCOVERABLE.md)。

**登记项按 `文件:起始行` 匹配，注释一改行号就漂。** 给 `os.Exit`/deadline 块附近增删注释会把后续块的起始行顶掉几位，登记随即失配、门禁打回"未定性"。所以改过那几个文件里这类块附近的注释后要重跑门禁，不能只看测试是否通过。

**先查被调方实现，再判断可达性；不要按"形状像"登记。** 本轮就有一个反例：`worker-sdk/go/client.go` 里两个 `SetReadDeadline`/一个 `SetWriteDeadline` 失败分支形状完全一致，但 gorilla v1.5.3 中 `SetWriteDeadline` 是纯字段赋值恒返回 nil（不可达），而 `SetReadDeadline` 转发给 `net.Conn`、连接关闭即失败（可达，且因 `writeControl` 是包级变量而能确定性构造）。查证依据：该版本 `conn.go` 的函数体。

## 原防御分支的归处

早期清单里几十处"不可达/防御分支"，经三轮攻坚后只剩三种结局，profile 中已无一处防御性零块：

1. **直调实测**：包级函数直接调用，不走生产入口（`compileRule` 六类字段错误、`pkcs7Unpad` 非法输入、`runProgram` 超时与取消、`evalProgram` seam 打桩超时等）。
2. **重构消灭**：分支本身是坏结构的产物，直接改代码删掉——
   - `Manager.Deliver` 的 `IsEnabled`+`GetProvider` 双锁竞态分支 → 合并为单锁 `lookupEnabled`，错误分支可实测；
   - `Engine.Put` 把 `compileRule` 前置到 `Validate` 之前，编译错误分支从"不可达守卫"变为可实测；
   - `runProgram` 的非 bool 返回值分支 → 单值断言 `r.out.(bool)`（不变量破坏必须 panic）。
3. **恒 nil 显式接受**：`x, _ := f(...)` 加注释说明为什么错误恒为 nil——gorilla 的 `SetWriteDeadline` 只记录时间戳；对可序列化结构的 `json.Marshal`；回退式限流器工厂；base64 密码摘要；SHA-256 摘要上的 `aes.NewCipher`；类型断言式 `parseConfig`。若契约变化，注释要求恢复为显式错误处理。

注解关键词 `Defensive` / `Unreachable` / `not callable` / `Coverage note` 在源码中检索即可定位每一处说明。

## Dashboard 前端：vitest + RTL，行覆盖 + 分支覆盖双 100% 门禁

前端（`dashboard/`）的口径与 Go 侧平行：vitest + Testing Library，`@vitest/coverage-v8` 统计，`dashboard/vitest.config.ts` 中 `thresholds: { lines: 100, branches: 100 }` 作为 CI 门禁（`.github/workflows/ci.yml` 的 dashboard job）——**行覆盖或分支覆盖低于 100% 直接失败**。测试共 15 个文件 144 个用例，覆盖 API 层（axios 实例 seam + adapter 注入走真实拦截器链）、zustand store、WebSocket hook（FakeWebSocket 手动驱动）、全部 9 个页面组件与应用入口。四项指标（语句 / 分支 / 函数 / 行）实测均为 100%，但**门禁阈值只守分支与行两项**——为什么这两项仍不等于"测到了"，见[下一节](#前端分支覆盖补齐的最后一轮-9779--100)。

测试搭建过程中顺带修掉两个真实生产缺陷：

1. **React 19 下 antd 静态 message 静默不弹**：antd v5 静态方法依赖 `ReactDOM.render`（React 19 已移除），必须调用 `unstableSetRender` 注入基于 `createRoot` 的渲染器（`src/main.tsx` + `src/test/setup.ts` 双处）。
2. **SendPage 渲染期副作用死循环**：`if (providers.length === 0) fetchProviders()` 写在渲染体内，zustand 每次 `set` 都换 state 引用，形成「set → 重渲染 → 再 fetch」无限循环；已移入 `useEffect` 空依赖数组。

`src/test/setup.ts` 另有一处不漏工作就测不干净的地方：注入的静态渲染器若不把 `createRoot().render()` 包进 `act()`，这次调度会漏到用例之外，由 scheduler 的 `processImmediate`（宏任务）在 **vitest 销毁 jsdom 之后**才冲刷，触发处 `window` 已不存在，抛 unhandled `ReferenceError`。它发生在覆盖率统计**之后**，所以行覆盖仍是 100%、门禁照样通过，vitest 却以 exit code 1 收场——**覆盖门禁抓不到这类泄漏**，只有 unhandled-error 报告能。

修法分两层，缺一层都会漏：

1. **`render`/`unmount` 都包 `act`**——否则这次 `createRoot().render()` 的调度工作从一开始就在用例作用域之外；
2. **注入渲染器登记每个 root，`afterAll` 逐个 `root.unmount()`（同样包 `act`）**——unmount 同步冲刷并作废该 root 名下所有已排程工作，之后 scheduler 再无可为该 root 触发的回调，收尾是**确定性**的。

只做第 1 层加 `afterEach` 定时排空（两轮 `act` + `0`/`20ms` 定时器，跨 rc-motion 的 rAF 兜底窗口）是**赌帧时机**：CI 上调度慢一点，immediate 就排在 flush 之后、环境销毁之后，照样抛 `window is not defined`——`23e8dc8`/`2c9f164` 两期 CI 红（3 处 unhandled error）正是这么来的，本地三轮并行复跑全绿也没能提前发现。`afterEach` 的定时排空仍保留（按用例回收动画帧，避免跨用例残留），但**不再承担收尾职责**；收尾只认第 2 层的确定性卸载。

### 前端分支覆盖补齐的最后一轮（97.69% → 100%）

上一期把分支水位记为 97.69%，并把未覆盖的 6 个分支整体定性为「工具口径残余 + 契约防御」。本轮逐个重查后，**这 6 个分支全部是可测的业务路径，无一需要台账登记**，已补齐到 100%：

| 位置 | 分支 | 之前的错误定性 | 实际处理 |
| --- | --- | --- | --- |
| `src/pages/GroupsPage.tsx:56` | `values.members \|\| []` 右支 | 契约防御，当下限不到 | 把换算提为导出的纯函数 `toGroupPayload`，直测 `members: undefined` 把右支语义固定成可断言行为；组件内只留调用 |
| `src/pages/LogsPage.tsx:104` | `l ? <Tag> : '-'` 的 `:` 支 | **v8 分支归属偏差**（误判） | 真凶是**用例自己没等数据**：`render()` 后同步 `querySelector`，行尚未渲染，`find` 返回 `undefined` 而 `not.toBeNull()` 对 `undefined` 也通过——用例一直是空转。改用 `waitFor` 真等出短横线。该判定已提为模块级导出的 `renderLevel`（语义不变，两支由这两条渲染用例真执行） |
| `src/pages/ProviderConfigPage.tsx:30,33` | `schema \|\| {}` 右支 | 契约防御 | 原有用例注释写「omits the schema field」，实际传的是 `schema: {}`（有键但空，走 `||` 左支）。改为真正省略该键 |
| `src/pages/ProviderConfigPage.tsx:71,96` | 保存/测试连接的 `token ? ... : {}` 假支 | 契约防御 | 原用例只覆盖了加载请求，没点按钮。补「无 token 时点保存 / 点测试通知」，断言请求头为空对象 |

教训有两条，都写进维护清单：

- **不要把「用例没真跑起来」归因成工具问题。** 那条 `v8 归属偏差` 结论是错的：用例断言在 `undefined` 上也会通过，等于零断言的假覆盖，工具口径只是替它背了锅。判定分支不可达之前，先确认该路径**真的被执行过一次**。
- **`not.toBeNull()` 断言不了「元素存在」。** 它对 `undefined` 同样通过。查 DOM 必须用 `waitFor` + `not.toBeUndefined()`，或直接 `findBy*`。

因此前端不再有残余未覆盖分支，`branches: 100` 作为门禁与实测水位一致，不含任何豁免。

顺带补掉一处**行覆盖门禁看不见的函数缺口**：`LogsPage` 的 10 秒自动刷新回调（`src/pages/LogsPage.tsx:88`）从未被任何用例执行过，报表里 `% Lines` 一栏仍显示 100%（该回调的语句不带行信息，被 v8 的 Lines 折算跳过），只有 `% Funcs` 95.45% 暴露了它。用例改为抓 `setInterval` 注册的回调直接调用并断言日志与统计双双重拉，`LogsPage` 四个指标随之全满（100/100/100/100）。

教训：**阈值只能守住它自己那一列**。`lines: 100` 满不代表函数都跑到，本轮就是靠交叉看 `% Funcs` 才发现的；补分支覆盖时顺手核对 `% Funcs` / `% Stmts`，是比只盯 Lines 便宜得多的自查。

由于四项指标同时打满，本轮**没有任何分支或函数需要登记进台账**——前端不需要 `KNOWN_UNCOVERABLE` 式的豁免清单，这是它与 Go 侧最大的口径差异：Go 侧的零块来自工具链原理性不可测路径（`os.Exit` 跳过 GOCOVERDIR 转储、gorilla 的恒 nil setter），前端这类缺口都能用确定性用例补上。补不上时（确有条件不可达）再照 Go 侧纪律登记。

## 如何维护这份水位

1. 给新代码写测试时以「触发每个分支」为目标，而不是「跑过函数」。
2. 若某分支确实不可达，先怀疑它是不是死代码——是就删掉；确需保留（API 兼容、未来重构防静默），就地写 `// Defensive: ...` 并说明原因。
3. 断言某个元素/分支真的出现过，就用 `waitFor` + `not.toBeUndefined()` 或 `findBy*`；`not.toBeNull()` 对 `undefined` 也通过，等于没断言。**判定「不可达」前先确认路径真的被执行过**——本轮就有一处把「用例空转」误判成「v8 归属偏差」。
4. 新增 `main()` 或常驻进程入口时，同步补 GOCOVERDIR 子进程守卫测试。
5. 阈值只随实测水位上调，不预留缓冲；任何人引入未测代码，CI 会立即拦下。
6. 覆盖率门禁只管「测没测到」，**不管「测干不干净」**：逃到环境销毁之后的异步工作（未被 `act` 收口的渲染、动画帧回调）会以 unhandled error 让 vitest 非零退出，但行覆盖仍显示 100%。给异步副作用补用例时，同步确认它落在 `act` 作用域内；否则以 unhandled-error 报告为准，别被绿色覆盖率骗过去。新增渲染 seam 沿用 `setup.ts` 的既有模式：**登记每个创建的 root、`afterAll` 确定性卸载**——定时排空只是辅助，别把收尾职责交还给帧时机。
