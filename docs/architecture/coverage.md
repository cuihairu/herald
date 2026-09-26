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
| 语句覆盖率（go-test 口径原始值） | 99.5% |
| 语句覆盖率（**门禁口径**：合并三个 `main()` 子进程实测后） | **99.7%** |
| CI 门禁 | `tools/zero_check.py coverage.merged.out --gate 100`：排除 [`KNOWN_UNCOVERABLE.md`](https://github.com/cuihairu/herald/blob/main/KNOWN_UNCOVERABLE.md) 登记块后等效语句覆盖率必须为 **100%**，且不存在未定性零块 |
| 残余零块 | **11 块**，全部经台账登记豁免：3 块 `main()` 的 `os.Exit` 失败分支、3 块 provider 重复名守卫、4 块 gorilla deadline setter 失败分支、1 块清 ack 截止时间 |

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

**登记行号 = 块的起始行，不是 `if` 所在行。** Go 把 `if` 的条件与两侧切成独立块：条件属于成功路径块（起始行 = `if` 行），分支体是另一个块（起始行 = 分支体第一行）。登记成 `if` 行等于把已被子进程合并救活的成功路径块登记成"不可覆盖"，真正的目标块反而裸露在门禁外——`--gate 100` 会立刻变红。

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

## Dashboard 前端：vitest + RTL，行覆盖 100% 门禁

前端（`dashboard/`）的口径与 Go 侧平行：vitest + Testing Library，`@vitest/coverage-v8` 统计，`dashboard/vitest.config.ts` 中 `thresholds: { lines: 100 }` 作为 CI 门禁（`.github/workflows/ci.yml` 的 dashboard job）——**行覆盖低于 100% 直接失败**。测试共 15 个文件 117 个用例，覆盖 API 层（axios 实例 seam + adapter 注入走真实拦截器链）、zustand store、WebSocket hook（FakeWebSocket 手动驱动）、全部 9 个页面组件与应用入口。

测试搭建过程中顺带修掉两个真实生产缺陷：

1. **React 19 下 antd 静态 message 静默不弹**：antd v5 静态方法依赖 `ReactDOM.render`（React 19 已移除），必须调用 `unstableSetRender` 注入基于 `createRoot` 的渲染器（`src/main.tsx` + `src/test/setup.ts` 双处）。
2. **SendPage 渲染期副作用死循环**：`if (providers.length === 0) fetchProviders()` 写在渲染体内，zustand 每次 `set` 都换 state 引用，形成「set → 重渲染 → 再 fetch」无限循环；已移入 `useEffect` 空依赖数组。

`src/test/setup.ts` 另有一处不漏工作就测不干净的地方：注入的静态渲染器若不把 `createRoot().render()` 包进 `act()`，这次调度会漏到用例之外，由 scheduler 的 `processImmediate`（宏任务）在 **vitest 销毁 jsdom 之后**才冲刷，触发处 `window` 已不存在，抛 unhandled `ReferenceError`。它发生在覆盖率统计**之后**，所以行覆盖仍是 100%、门禁照样通过，vitest 却以 exit code 1 收场——**覆盖门禁抓不到这类泄漏**，只有 unhandled-error 报告能。因此 `render`/`unmount` 都包 `act`，`afterEach` 再用两轮 `act` + 定时器（`0` 与 `20ms`，后者跨过 rc-motion 的 rAF 兜底窗口）把 scheduler 与动画帧排空后才交还环境。

### 前端残余的未覆盖分支定性

行覆盖 100%（门禁）；分支覆盖 95%，未达满的分支逐类定性如下——均为「兜底文案/环境性分支/契约防御」，不是未测的业务路径：

| 位置 | 分支 | 未覆盖侧 | 原因 |
| --- | --- | --- | --- |
| `src/hooks/useWebSocket.ts:18` | `https:` → `wss:` | wss 支 | jsdom 页面协议恒为 `http://localhost/`，环境性不可达；生产 HTTPS 下自然走 wss |
| `src/pages/LoginPage.tsx:20` | `values.remember && res.user` | 假支 | 已测两真支（记住登录）；「不记住/无 user」组合是同一 setItem 调用的否路径 |
| `src/pages/SendPage.tsx:26-27` | `err.message \|\| '发送失败'` | `\|\|` 右支 | 兜底文案：err 无 message 时显示默认提示；已有用例覆盖带 message 的错误路径 |
| `src/pages/ProvidersPage.tsx:37` | 卡片标题/Tag 同行多个三元 | 个别半支 | 名称映射（feishu→飞书）与原名回退、builtin/plugin 两色、available/down 徽标均各有用例；v8 按行报告，一行多个短路/三元表达式时无法指认残余的具体半支 |
| `src/pages/GroupsPage.tsx:56` | `values.members \|\| []` | `\|\|` 右支 | 契约防御：antd 的 `Form.List` 未增行时给的是 `[]`（空数组本身为真），右支当下走不到。已就地注释——保留它防 antd 哪天把未增行的 `Form.List` 改回 `undefined`，那会让整页崩在 `.map` 上。`RulesPage` / `GroupsPage` 其余分支（`errMsg` 三级兜底的最后一级、非 route 规则不带 `route` 步骤、省略零值字段的规则与空花名册群组、启停开关双向）均有确定性用例 |
| `src/pages/LogsPage.tsx` 其余 | `allowClear` 清除、`message.error(x \|\| '默认')` 兜底 | `\|\|` 右支等 | 兜底文案与「清除到空」路径；主路径（选值、带 message 的错误）均有确定性触发 |
| `src/pages/ProviderConfigPage.tsx:30-33,71,96` | `data.data.schema \|\| {}`、`token ? ... : {}` | `\|\|`/三元右支 | ① schema 字段整体缺失（`data.data` 无 `schema` 键）时回显走 `|| {}`；② 保存与测试通知在无 token 时 headers 走空对象。均为契约防御/兜底路径，防御性保留以应对 API 返回结构变化 |

## 如何维护这份水位

1. 给新代码写测试时以「触发每个分支」为目标，而不是「跑过函数」。
2. 若某分支确实不可达，先怀疑它是不是死代码——是就删掉；确需保留（API 兼容、未来重构防静默），就地写 `// Defensive: ...` 并说明原因。
3. 新增 `main()` 或常驻进程入口时，同步补 GOCOVERDIR 子进程守卫测试。
4. 阈值只随实测水位上调，不预留缓冲；任何人引入未测代码，CI 会立即拦下。
5. 覆盖率门禁只管「测没测到」，**不管「测干不干净」**：逃到环境销毁之后的异步工作（未被 `act` 收口的渲染、动画帧回调）会以 unhandled error 让 vitest 非零退出，但行覆盖仍显示 100%。给异步副作用补用例时，同步确认它落在 `act` 作用域内；否则以 unhandled-error 报告为准，别被绿色覆盖率骗过去。
