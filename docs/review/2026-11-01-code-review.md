# Herald 代码深审报告（安全静态扫描 + 隐藏 bug 猎捕 + 质量门）

- 日期：2026-11-01（收到指派，2026-11-02 执行落库）
- 范围：本仓全量 Go 面——`cmd/heraldd`、`api/`、`core/`（31 包）、`config/`、`protocol/`、`providers/builtin/`（18 渠道 + worker + registry）、`apps-sdk/go`、`worker-sdk/go`、`examples/quickstart`、根包；生产代码 28,519 行 + 测试 57,670 行。Dashboard 前端不在本轮（2026-10-31 已有 UI 审计报告）。
- 方法：①安全静态扫描（硬编码密钥 / shell 注入 / SQL 拼接 / eval / 反序列化 / 路径穿越 / TLS 校验 / SSRF / 头部注入）→ ②隐藏 bug 猎捕（错误处理缺失 / 竞态 / 越界 / 资源泄漏 / off-by-one / 并发）→ ③质量门（gofmt / go vet / go build / go test / go test -race / golangci-lint / docs build）。
- 工具链：go1.26.6（go.mod 钉定，与 CI `go-version: '1.26'` 一致）；golangci-lint v2.14.0；注意本地 `GOTOOLCHAIN=auto` 会让 lint 选 go1.27 与 v2.14.0 二进制错配而 panic，须 `GOTOOLCHAIN=go1.26.6` 运行（CI 用 `version: latest` + goinstall 不受影响）。

## 结论一览

| 严重度 | 数量 | 明细 |
| --- | --- | --- |
| P0 | 0 | — |
| P1 | 0 | — |
| P2 | 1 | F-1 回调 URL 未拒私网/环回段（SSRF 加固面） |
| P3 | 2 | F-2 limiter.Wait 计时器 churn；F-3 websocket 关闭窄竞态 |

**发现总数 3；已修 0（无 P0/P1，按处置规则 P2/P3 记 todo.md）；剩余 3（SEC-CALLBACK / BUG-LIMITER / BUG-WSSTOP 已入 todo）。**

## 发现明细（按严重度排序）

### F-1 · P2 · 安全 · 回调 URL 未拒私网/环回段（SSRF 加固面）

- 证据：`core/apps/callback.go:27-30`——`SetCallback` 对 URL 仅校验「解析成功 + scheme 是 http/https + Host 非空」，未检查 Host 是否落在私网/环回段（127.0.0.1、::1、10.0.0.0/8、172.16.0.0/12、192.168.0.0/16、169.254.0.0/16 链路本地元数据地址）。
- 场景：多租户部署下，持 `config` scope 的集成方应用 token 可把回调设为 `http://169.254.169.254/...` 或 `http://localhost:8080/api/v1/...`；emitter（`core/callback/emitter.go:110-135`）随后把签名事件 POST 到该地址——herald 进程成为向内网发请求的代理。缓解因素：单组织自托管是主部署形态、集成方本就是半可信内部团队、回调 secret 是集成方自己的（无权限提升）、响应体不回传给调用方（只进投递日志）。
- 处置：P2 记 todo（SEC-CALLBACK）。修法：SetCallback 增加 CIDR 拒绝列表（默认拒私网+环回+链路本地，`callback_allow_private_ranges` 之类的配置豁免单组织部署）+ 测试。不动现有行为默认语义之外的东西。

### F-2 · P3 · 并发 · limiter.Wait 计时器 churn

- 证据：`core/limiter/limiter.go:169`——`Wait` 的循环体里 `case <-time.After(waitTime)`：每轮分配一个新计时器；`ctx.Done()` 分支返回时无法 `Stop` 尚未触发的计时器，只能等它自然到期回收。
- 场景：高并发投递被限速 + 调用方频繁取消（如 HTTP 客户端超时）时，产生「已取消但仍在倒计时」的计时器短期堆积。量级：每个计时器最长存活 `waitTime`（令牌桶场景下通常亚秒到秒级），影响是有界的。
- 处置：P3 记 todo（BUG-LIMITER）。修法：`timer := time.NewTimer(waitTime)` + `defer timer.Stop()`（或循环外复用单计时器），`select` 后 `if !timer.Stop() { <-timer.C }` 惯用法。

### F-3 · P3 · 并发 · websocket 关闭路径窄竞态

- 证据：`core/websocket/server.go:187-207` `Stop()` 顺序是「持锁关闭所有已注册连接 → `s.cancel()` → `server.Shutdown(ctx)` → `s.wg.Wait()`」；而 `handleWebSocket`（:212-247）→ `handleConnection`（:249-250）里有 `s.wg.Add(1)`。若一条连接在 `Shutdown` 生效前完成升级，`wg.Add` 就可能发生在 `wg.Wait` 已经启动之后——WaitGroup 语义上 Add 与 Wait 并发是禁止的（文档：并发调用 Add 与 Wait 有竞态）。
- 场景：仅在停机窗口内命中：升级握手已过、`handleConnection` 尚未执行、`Shutdown` 还没收尾。`-race` 全量门禁（62 包）未触发；影响限于停机日志里一条 race 警告或 wg 计数异常，无数据破坏面。
- 处置：P3 记 todo（BUG-WSSTOP）。修法：加关闭门闩——`stopped atomic.Bool`，`handleWebSocket` 在 Upgrade 前检查，`Stop()` 先置位再 Shutdown；或把连接 goroutine 的生命周期挂到 `s.ctx` 上。

## 已核查且干净的面（负面结论均先验证了搜索路径）

**① 安全静态扫描矩阵**：

| 向量 | 结论 | 证据 |
| --- | --- | --- |
| 硬编码密钥 | 0 处 | 全仓 `password/secret/api_key/token = "字面量"` 模式 grep，非测试代码零命中（.env.example 全部占位值、config.yaml 全部 `$VAR` 引用） |
| shell 注入 | 0 处 | `exec.Command`/`syscall.Exec` 全仓零命中——heraldd 不起子进程 |
| SQL 拼接 | 不适用 | 全仓无 `database/sql`、无 SQL 字面量（存储面是内存 + Redis Stream + JSON 文件） |
| eval/动态执行 | 0 处 | `eval(`/`reflect.`/`unsafe.` 非测试代码零命中；规则引擎表达式走 expr-lang/expr 的沙箱求值器（无 os 访问） |
| 反序列化 | 安全 | 30 处 `json.Unmarshal` 全部进类型化 struct（`protocol/message.go:151`、`api/handler_sources.go:86` 等）；yaml 仅 1 处且只解析本地配置文件（`config/config.go:366`） |
| 路径穿越 | 0 处 | 全部 `os.ReadFile/WriteFile` 路径来自启动配置（`config.Load` 的 path、五个 file store 的 `s.path`），无 HTTP 输入拼接路径 |
| TLS 校验 | 干净 | `InsecureSkipVerify` 仅出现在测试（`tencentsms_extra_test.go:54`），生产 `core/httpclient/client.go:57` 用默认校验 |
| 头部注入 | 0 处 | `Header.Set` 全部固定值或配置值；`core/httpclient/client.go:207` 的自定义头来自 provider 配置而非 HTTP 请求 |
| 请求体上限 | 覆盖完整 | 全局中间件 `api/server.go:353-374`（`limitRequestBodies`，默认 1 MiB，config.go:383-384 兜底）包住整个 mux；`handler_notify.go:72` 虽无自身 LimitReader 但被该中间件覆盖；apps 面 7 处 + sources/callback 4 处另有 `io.LimitReader(1<<20)` 双保险 |
| 认证旁路 | 符合设计 | `api/server.go:539-543` `withAuth` 在 auth 关闭时直通——这正是「关闭鉴权只许 loopback」门禁（config.go:550-554）守护的合法路径；`/status`、`/feeds/**` 匿名是文档承诺行为 |

**② 隐藏 bug 猎捕**：

- 竞态：`go test -race -count=1 ./...` 62 包全绿、0 DATA RACE、0 panic；websocket server（`s.mu` 护 `workers` map，GetWorkers 走 RLock + cloneWorkerInfo 深拷贝 :552-556）、digest 聚合器（单 `mu` + 注入时钟）、memory 队列（`mu.RLock` 与 chan 的分离有注释论证，memory.go:18-19）、audience 注册表均加锁正确。
- 越界/off-by-one：分页参数默认值正确（`api/handler_delivery.go:17-23` 解析失败归零→默认 50、封顶 500，与文档一致；logs 同型）；`plan.Stages[0]` 有空计划守卫（2026-10-11 #25 已补）；未发现 `[len()-1]` / `i<=len` 型索引。
- 资源泄漏：全部 `http.Client` 带超时（tencentsms.go:250、apps-sdk/go/client.go:33、core/httpclient）；`resp.Body` 全部 defer Close；goroutine 生命周期由 `wg`/`ctx` 收口（worker pool :72、websocket :206、worker-sdk :378）；`time.After` 仅 F-2 一处。
- 错误处理：非测试代码的忽略错误（`_ =`）逐条看过——全部是清理性 `Close`/响应编码/注册后的降级路径，属惯用；`api/handler_alerts.go:74` 的 `_ = Cancel` 是「尽力取消」语义。
- panic 面：非测试代码仅 2 处（`core/audience/sources.go:27`、`reconcile.go:59`），都是构造器不变量（双注册表缺失即 fail-fast），非用户输入可达。

**③ 质量门**：

| 门 | 结果 |
| --- | --- |
| `gofmt -l` | 0 文件 |
| `go vet ./...` | 0 告警 |
| `go build ./...` | 通过 |
| `go test ./...` | 62 包 ok、0 FAIL |
| `go test -race -count=1 ./...` | 62 包 ok、0 DATA RACE（go1.26.6，-count=1） |
| `golangci-lint run ./...` | 0 issues（GOTOOLCHAIN=go1.26.6；auto 下 v2.14.0 二进制与 go1.27 选择错配会 panic，是本地工具链问题非代码问题） |
| docs build（vitepress） | 绿（21.75s / 18.61s 两轮） |

## 处置与提交

- 本轮无 P0/P1，零代码修改；P2/P3 三项按处置规则记入 `todo.md`（SEC-CALLBACK / BUG-LIMITER / BUG-WSSTOP，各带本报告 F-x 指针）。
- 同批落库的文档审计（第十七轮）提交：`0fcca93`（zh readme 补 events 链接）、`6d4bafe`（todo 叙述精简）、`78b9490`（审计报告追加），已推送 origin/main。
- 本报告提交：见 git log `docs: add 2026-11-01 code review report`。
