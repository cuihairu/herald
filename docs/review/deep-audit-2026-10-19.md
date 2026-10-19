# Herald 深度独立审核（2026-10-19）

## 1. 模块清单

本清单由 `go list ./...` 生成；测试文件是否存在另在第 3 节按包统计。

| 模块/包 | 主要职责（基于源码目录命名，详细职责待逐包确认） |
|---|---|
| 根模块 `github.com/cuihairu/herald` | 公共入口：`herald.go` |
| `api` | HTTP API 与 handler |
| `apps-sdk/go`、`apps-sdk/go/example` | Apps Go SDK 与示例 |
| `cmd/heraldd` | 服务命令行入口 |
| `config` | 配置解析与校验 |
| `core` | 核心域代码（另拆分以下子包） |
| `core/ack` | ack |
| `core/apps` | apps |
| `core/audience` | audience |
| `core/audit` | audit |
| `core/auth` | auth |
| `core/callback` | callback |
| `core/dedup` | dedup |
| `core/deliverystore` | deliverystore |
| `core/digest` | digest |
| `core/dispatch` | dispatch |
| `core/errclass` | errclass |
| `core/escalation` | escalation |
| `core/feeds` | feeds |
| `core/groups` | groups |
| `core/httpclient` | httpclient |
| `core/incident` | incident |
| `core/jwt` | jwt |
| `core/limiter` | limiter |
| `core/logstore` | logstore |
| `core/queue` | queue |
| `core/retry` | retry |
| `core/roster` | roster |
| `core/route` | route |
| `core/rules` | rules |
| `core/runtime` | runtime |
| `core/service` | service |
| `core/template` | template |
| `core/user` | user |
| `core/websocket` | websocket |
| `core/worker` | worker |
| `examples/quickstart` | quickstart 示例 |
| `internal/logger` | 内部日志封装 |
| `protocol` | 消息协议 |
| `providers/builtin/{aliyunsms,apns,dingtalk,discord,email,fcm,feishu,getui,jpush,log,neteasesms,registry,slack,telegram,tencentsms,webhook,wechat,wechatmp,wecom,worker}` | 内置渠道/provider 实现 |
| `worker-sdk/go`、`worker-sdk/go/example` | Worker Go SDK 与示例 |

**工作区约束**：检查时 `git status` 已有 171 条已暂存或未暂存改动，含大量文档重命名/翻译；这些不属于本次审核，报告提交只暂存本文件。证据：`git status --short --branch`（命令输出，无固定源码行）。

## 2. 代码质量

审核中；逐条发现将注明源码路径及行号。


### 2.1 代码质量发现

| 风险 | 发现与证据 |
|---|---|
| 高 | provider 通用 HTTP 客户端在 JSON、表单、GET 三条路径上对上游响应使用无界 `io.ReadAll`，上游或被配置的恶意端点可令进程持续分配内存；三处分别位于 [core/httpclient/client.go:204](/home/cui/workspaces/herald/core/httpclient/client.go:204)、[core/httpclient/client.go:245](/home/cui/workspaces/herald/core/httpclient/client.go:245)、[core/httpclient/client.go:276](/home/cui/workspaces/herald/core/httpclient/client.go:276)。错误状态还会把完整响应体放入错误字符串 [core/httpclient/client.go:93](/home/cui/workspaces/herald/core/httpclient/client.go:93)。 |
| 中 | API 层确有忽略错误，但其中几个写了前置不变量注释：`UpdatePolicies` 后忽略错误、继而回读 [api/handler_apps.go:209](/home/cui/workspaces/herald/api/handler_apps.go:209)-[api/handler_apps.go:216](/home/cui/workspaces/herald/api/handler_apps.go:216)，`ClearCallback` 同理 [api/handler_apps.go:402](/home/cui/workspaces/herald/api/handler_apps.go:402)-[api/handler_apps.go:408](/home/cui/workspaces/herald/api/handler_apps.go:408)。不过 `handleAppShow` 又无注释地忽略 `Authenticate` 的 bool [api/handler_apps.go:73](/home/cui/workspaces/herald/api/handler_apps.go:73)-[api/handler_apps.go:83](/home/cui/workspaces/herald/api/handler_apps.go:83)；当前路由先经 `withApp(ScopeQuery)` 校验，故正常路由不可触发失败结果，但直接调用 handler 的可测试性和未来路由改动容易破坏此假设。 |
| 中 | `Auth.New` 对配置的 admin 用户逐个调用 `CreateDefaultUser` 并忽略错误 [core/auth/auth.go:42](/home/cui/workspaces/herald/core/auth/auth.go:42)-[core/auth/auth.go:48](/home/cui/workspaces/herald/core/auth/auth.go:48)。用户名密码已做非空筛选；内部创建失败会静默令配置用户未生效。当前 Manager 新建时失败条件需进一步证明，影响定为中。 |
| 低 | HTTP 响应体 close 错误在三条请求路径均被忽略 [core/httpclient/client.go:201](/home/cui/workspaces/herald/core/httpclient/client.go:201)、[core/httpclient/client.go:243](/home/cui/workspaces/herald/core/httpclient/client.go:243)、[core/httpclient/client.go:274](/home/cui/workspaces/herald/core/httpclient/client.go:274)；通常不影响调用结果，但不应与业务错误吞噬混为一谈。 |

模块边界总体以 `core/*` 领域包、`api` transport、`cmd/heraldd` composition root、`providers/builtin/*` 适配器划分；组装依赖集中在 [api/server.go:132](/home/cui/workspaces/herald/api/server.go:132)-[api/server.go:238](/home/cui/workspaces/herald/api/server.go:238)。`api.Server` 配置对象携带多个领域 Manager，适合当前单体组装但会让 handler 依赖面较宽；目前没有证据显示跨层循环依赖。并发安全与 goroutine 泄漏：源代码结构可见锁/上下文和 worker 生命周期，但本次未执行 race/压力测试，不能据静态阅读证明无竞争或泄漏，标记为未核实；关键入口 [core/worker/pool.go:68](/home/cui/workspaces/herald/core/worker/pool.go:68)、[core/websocket/server.go:170](/home/cui/workspaces/herald/core/websocket/server.go:170)。

## 3. 测试覆盖

测试文件分布检查显示，`go list ./...` 结果中未发现缺少 `*_test.go` 的 Go 包；每个包至少有包内测试文件。代表性目录包括 `api`、`core/audience`、`core/queue`、`core/websocket`、`providers/builtin/*`、两个 SDK。仅凭存在测试文件无法证明风险路径覆盖。高风险缺口：外部 HTTP 响应体大小上限/超限行为目前没有限流实现可测 [core/httpclient/client.go:204](/home/cui/workspaces/herald/core/httpclient/client.go:204)-[core/httpclient/client.go:206](/home/cui/workspaces/herald/core/httpclient/client.go:206)；鉴权凭据 query 参数的代理/访问日志泄露没有集成测试 [core/auth/auth.go:240](/home/cui/workspaces/herald/core/auth/auth.go:240)-[core/auth/auth.go:265](/home/cui/workspaces/herald/core/auth/auth.go:265)；本次未运行测试，实际覆盖率数值和 race 结果未核实。

### 3.1 KNOWN_UNCOVERABLE 核验

台账声明以 Go 1.26.6、covermerge 合并子进程入口 profile、按完整块区间匹配豁免 [KNOWN_UNCOVERABLE.md:1](/home/cui/workspaces/herald/KNOWN_UNCOVERABLE.md:1)-[KNOWN_UNCOVERABLE.md:7](/home/cui/workspaces/herald/KNOWN_UNCOVERABLE.md:7)。CI 确实配置 Go 1.26、`-race -coverpkg=./...`、子进程合并和 `zero_check --gate 100` [.github/workflows/ci.yml:33](/home/cui/workspaces/herald/.github/workflows/ci.yml:33)-[.github/workflows/ci.yml:76](/home/cui/workspaces/herald/.github/workflows/ci.yml:76)。台账所述技术理由（`os.Exit` 不执行进程退出后的 profile 转储）与流程一致；但本次未用固定 Go 1.26.6 重跑 profile，也未核对实际合并产物，所以“排除豁免后 100%”及每个登记行号当前是否匹配均为**未核实**。豁免覆盖竞态调度窗口、恒 nil 分支、编码器 writer 契约以及多个 os.Exit 分支，理由逐项详述，暂未发现明显以常规可达业务分支冒充不可覆盖的条目 [KNOWN_UNCOVERABLE.md:15](/home/cui/workspaces/herald/KNOWN_UNCOVERABLE.md:15)-[KNOWN_UNCOVERABLE.md:55](/home/cui/workspaces/herald/KNOWN_UNCOVERABLE.md:55)。


## 4. 文档一致性（README.md / README.zh.md）

README 两份描述逐项对照路由、构建入口和 provider registry：使用形态、匿名 status/feed、app 独立 token、SDK、Docker Compose 命令均与代码/配置一致 [README.md:20](/home/cui/workspaces/herald/README.md:20)、[README.md:60](/home/cui/workspaces/herald/README.md:60)、[README.md:99](/home/cui/workspaces/herald/README.md:99)、[README.zh.md:20](/home/cui/workspaces/herald/README.zh.md:20)、[README.zh.md:60](/home/cui/workspaces/herald/README.zh.md:60)、[README.zh.md:99](/home/cui/workspaces/herald/README.zh.md:99)。README 所称 18 个内置通知渠道，表格列了 19 个 provider，但其中 Worker 被明确说明为本地占位/远端调度节点而非通知渠道，因此计数自洽 [README.md:65](/home/cui/workspaces/herald/README.md:65)-[README.md:95](/home/cui/workspaces/herald/README.md:95)；中文对应 [README.zh.md:65](/home/cui/workspaces/herald/README.zh.md:65)-[README.zh.md:95](/home/cui/workspaces/herald/README.zh.md:95)。未发现明确的 README 功能陈述与实现冲突。

README 的一些宽泛表述（例如“完整核心管道共用”）依赖具体配置和调用方式，不能据单一 README 例子证明所有功能在 Go Library 模式均已启用；这是说明边界不够精确，不作为已证实的不符项 [README.md:20](/home/cui/workspaces/herald/README.md:20)、[README.md:30](/home/cui/workspaces/herald/README.md:30)、[README.zh.md:20](/home/cui/workspaces/herald/README.zh.md:20)、[README.zh.md:30](/home/cui/workspaces/herald/README.zh.md:30)。

## 5. 安全

| 风险 | 发现与证据 |
|---|---|
| 高 | 同第 2 节，上游响应体无大小限制，既有内存耗尽面也可能把超大错误体传播到日志 [core/httpclient/client.go:93](/home/cui/workspaces/herald/core/httpclient/client.go:93)、[core/httpclient/client.go:204](/home/cui/workspaces/herald/core/httpclient/client.go:204)、[core/httpclient/client.go:245](/home/cui/workspaces/herald/core/httpclient/client.go:245)、[core/httpclient/client.go:276](/home/cui/workspaces/herald/core/httpclient/client.go:276)。 |
| 中 | operator JWT 与 API key 支持 URL 查询参数 [core/auth/auth.go:240](/home/cui/workspaces/herald/core/auth/auth.go:240)-[core/auth/auth.go:265](/home/cui/workspaces/herald/core/auth/auth.go:265)，测试也把兼容行为锁定 [core/auth/auth_test.go:442](/home/cui/workspaces/herald/core/auth/auth_test.go:442)-[core/auth/auth_test.go:448](/home/cui/workspaces/herald/core/auth/auth_test.go:448)。REST 文档确认该能力 [docs/api/rest.md:5](/home/cui/workspaces/herald/docs/api/rest.md:5)-[docs/api/rest.md:8](/home/cui/workspaces/herald/docs/api/rest.md:8)。查询串可能被反向代理、浏览器历史、分析平台记录；建议弃用并移除 query 凭据，只留 Authorization/X-API-Key header 与 cookie（JWT）。 |
| 中 | 管理面 `auth.enabled` 关闭时 middleware 直接放行 [core/auth/auth.go:83](/home/cui/workspaces/herald/core/auth/auth.go:83)-[core/auth/auth.go:105](/home/cui/workspaces/herald/core/auth/auth.go:105)，且无 API key 可用时仍允许匿名访问。这符合 README 的“可选鉴权”描述，但生产配置若误关会开放管理 API；目前部署门禁是否强制鉴权未核实 [README.md:60](/home/cui/workspaces/herald/README.md:60)、[README.zh.md:60](/home/cui/workspaces/herald/README.zh.md:60)。 |
| 低 | 主 API 多数 JSON handler 直接从 `r.Body` 解码且未统一限长（例如 notify、模板、组、规则）[api/handler_notify.go:69](/home/cui/workspaces/herald/api/handler_notify.go:69)-[api/handler_notify.go:75](/home/cui/workspaces/herald/api/handler_notify.go:75)、[api/handler_template.go:53](/home/cui/workspaces/herald/api/handler_template.go:53)、[api/handler_groups.go:74](/home/cui/workspaces/herald/api/handler_groups.go:74)、[api/handler_rules.go:102](/home/cui/workspaces/herald/api/handler_rules.go:102)。Apps、feeds/source 入口已有 1 MiB 限制作对照 [api/handler_apps.go:101](/home/cui/workspaces/herald/api/handler_apps.go:101)、[api/handler_sources.go:37](/home/cui/workspaces/herald/api/handler_sources.go:37)。需给整个公网 JSON 面统一请求体上限。 |

路径处理：本轮静态检索没有发现用户输入拼接文件路径后直接打开的证据；配置文件 store 的路径来自 operator 配置，具体部署权限未核实。敏感信息日志：HTTP 错误会带原始 body [core/httpclient/client.go:93](/home/cui/workspaces/herald/core/httpclient/client.go:93)，若上游错误响应回显 token/手机号等，会进入调用链错误日志；已见 worker 错误日志记录 err [core/worker/pool.go:123](/home/cui/workspaces/herald/core/worker/pool.go:123)。其余凭据日志泄露情况未核实。

## 6. 工程治理

| 项目 | 结论 |
|---|---|
| Makefile | `build`、`test`、Dashboard/docs、Docker 目标有对应目录或文件 [Makefile:3](/home/cui/workspaces/herald/Makefile:3)-[Makefile:70](/home/cui/workspaces/herald/Makefile:70)。`test-integration` 明确需要本地 Redis [Makefile:39](/home/cui/workspaces/herald/Makefile:39)-[Makefile:42](/home/cui/workspaces/herald/Makefile:42)。本次未执行目标，执行结果未核实。 |
| 依赖版本 | 模块要求 Go 1.26.6，理由是修复 stdlib 安全问题 [go.mod:3](/home/cui/workspaces/herald/go.mod:3)-[go.mod:5](/home/cui/workspaces/herald/go.mod:5)。CI 使用浮动 Go 1.26 [.github/workflows/ci.yml:33](/home/cui/workspaces/herald/.github/workflows/ci.yml:33)-[.github/workflows/ci.yml:37](/home/cui/workspaces/herald/.github/workflows/ci.yml:37)，Docker 构建也使用 `golang:1.26-alpine` [Dockerfile:1](/home/cui/workspaces/herald/Dockerfile:1)-[Dockerfile:2](/home/cui/workspaces/herald/Dockerfile:2)。浮动标签能跟进补丁但构建不可完全复现；依赖没有 `replace` 强制本地覆盖（`go.mod` 全文）。 |
| CI | Go race + 全仓 coverage、独立 main 覆盖合并、100% gate、lint、dashboard typecheck/coverage/build、Go build、Docker build 均有配置 [.github/workflows/ci.yml:49](/home/cui/workspaces/herald/.github/workflows/ci.yml:49)-[.github/workflows/ci.yml:76](/home/cui/workspaces/herald/.github/workflows/ci.yml:76)、[.github/workflows/ci.yml:87](/home/cui/workspaces/herald/.github/workflows/ci.yml:87)-[.github/workflows/ci.yml:105](/home/cui/workspaces/herald/.github/workflows/ci.yml:105)、[.github/workflows/ci.yml:107](/home/cui/workspaces/herald/.github/workflows/ci.yml:107)-[.github/workflows/ci.yml:145](/home/cui/workspaces/herald/.github/workflows/ci.yml:145)。actions 与 linter `latest` 具浮动性，可能导致同一 commit 后续 CI 行为变化 [.github/workflows/ci.yml:100](/home/cui/workspaces/herald/.github/workflows/ci.yml:100)-[.github/workflows/ci.yml:104](/home/cui/workspaces/herald/.github/workflows/ci.yml:104)。 |
| Dockerfile | 多阶段构建、静态 binary、复制 CA 与时区数据，整体合理 [Dockerfile:1](/home/cui/workspaces/herald/Dockerfile:1)-[Dockerfile:27](/home/cui/workspaces/herald/Dockerfile:27)。最终镜像采用 `alpine:latest` 且未设置非 root 用户 [Dockerfile:19](/home/cui/workspaces/herald/Dockerfile:19)-[Dockerfile:24](/home/cui/workspaces/herald/Dockerfile:24)，影响供应链可复现性和容器内权限隔离。 |

## 7. API 契约

REST 路由实现覆盖管理 API、回调、feeds、app namespace 等路径 [api/server.go:240](/home/cui/workspaces/herald/api/server.go:240)-[api/server.go:330](/home/cui/workspaces/herald/api/server.go:330)。`docs/api/overview.md` 的 API 表覆盖上述端点并标记配置依赖 [docs/api/overview.md:42](/home/cui/workspaces/herald/docs/api/overview.md:42)-[docs/api/overview.md:129](/home/cui/workspaces/herald/docs/api/overview.md:129)；REST 文档提供对应字段细节 [docs/api/rest.md:17](/home/cui/workspaces/herald/docs/api/rest.md:17)-[docs/api/rest.md:1053](/home/cui/workspaces/herald/docs/api/rest.md:1053)。路由登记与文档路径对照未发现明确的 method/path 漏配。

`docs/api/openapi.yaml` 自述为集成者 API，但同时包含 `/api/v1/notify` 和 app namespace 契约 [docs/api/openapi.yaml:1](/home/cui/workspaces/herald/docs/api/openapi.yaml:1)-[docs/api/openapi.yaml:8](/home/cui/workspaces/herald/docs/api/openapi.yaml:8)。其 paths 只覆盖 notify + app 集成者端点，不含 `docs/api/overview.md` 中的 operator、callback、RSS、audience 等 API [docs/api/openapi.yaml:31](/home/cui/workspaces/herald/docs/api/openapi.yaml:31)-[docs/api/openapi.yaml:30](/home/cui/workspaces/herald/docs/api/openapi.yaml:30)，而完整路由范围更广 [api/server.go:248](/home/cui/workspaces/herald/api/server.go:248)-[api/server.go:330](/home/cui/workspaces/herald/api/server.go:330)。这对“集成者 OpenAPI”定位可接受；若对外称为全量 API 契约则不完整。`/api/v1/notify` 描述全部失败时 HTTP 200、body code 422，与 handler 以 JSON 业务错误处理相符 [docs/api/openapi.yaml:32](/home/cui/workspaces/herald/docs/api/openapi.yaml:32)-[docs/api/openapi.yaml:39](/home/cui/workspaces/herald/docs/api/openapi.yaml:39)、[api/handler_notify.go:104](/home/cui/workspaces/herald/api/handler_notify.go:104)-[api/handler_notify.go:121](/home/cui/workspaces/herald/api/handler_notify.go:121)。OpenAPI schema 没有从实现自动生成或契约测试保护，本次未核实请求/响应全部字段逐个一致。


## 8. 汇总

### 8.1 缺陷（按风险排序）

| 排序 | 风险 | 缺陷 | 证据 |
|---:|---|---|---|
| 1 | 高 | 上游 HTTP 响应体读取无上限，可能造成内存耗尽；错误响应完整写入错误文本。 | [core/httpclient/client.go:93](/home/cui/workspaces/herald/core/httpclient/client.go:93)、[core/httpclient/client.go:204](/home/cui/workspaces/herald/core/httpclient/client.go:204)、[core/httpclient/client.go:245](/home/cui/workspaces/herald/core/httpclient/client.go:245)、[core/httpclient/client.go:276](/home/cui/workspaces/herald/core/httpclient/client.go:276) |
| 2 | 中 | 管理凭据允许放在 query string，存在代理/访问日志泄露面。 | [core/auth/auth.go:240](/home/cui/workspaces/herald/core/auth/auth.go:240)-[core/auth/auth.go:265](/home/cui/workspaces/herald/core/auth/auth.go:265) |
| 3 | 中 | 多个管理 API JSON body 无统一大小限制，可被大请求消耗内存。 | [api/handler_notify.go:69](/home/cui/workspaces/herald/api/handler_notify.go:69)-[api/handler_notify.go:75](/home/cui/workspaces/herald/api/handler_notify.go:75)、[api/handler_template.go:53](/home/cui/workspaces/herald/api/handler_template.go:53)、[api/handler_groups.go:74](/home/cui/workspaces/herald/api/handler_groups.go:74)、[api/handler_rules.go:102](/home/cui/workspaces/herald/api/handler_rules.go:102) |
| 4 | 低 | Docker runtime 使用浮动 `alpine:latest` 且默认 root。 | [Dockerfile:19](/home/cui/workspaces/herald/Dockerfile:19)-[Dockerfile:24](/home/cui/workspaces/herald/Dockerfile:24) |

### 8.2 文档不符

| 文档 | 不符项 | 证据 |
|---|---|---|
| README.md / README.zh.md | 未确认到功能陈述与实现存在明确冲突；“18 个通知渠道”不计入 worker 占位 provider，数量吻合。 | [README.md:65](/home/cui/workspaces/herald/README.md:65)-[README.md:95](/home/cui/workspaces/herald/README.md:95)、[README.zh.md:65](/home/cui/workspaces/herald/README.zh.md:65)-[README.zh.md:95](/home/cui/workspaces/herald/README.zh.md:95) |
| OpenAPI | 仅描述 notify + integrator app API，不是全量 server API；以集成者规范定位可接受。 | [docs/api/openapi.yaml:1](/home/cui/workspaces/herald/docs/api/openapi.yaml:1)-[docs/api/openapi.yaml:8](/home/cui/workspaces/herald/docs/api/openapi.yaml:8)、[docs/api/openapi.yaml:31](/home/cui/workspaces/herald/docs/api/openapi.yaml:31)、[api/server.go:248](/home/cui/workspaces/herald/api/server.go:248)-[api/server.go:330](/home/cui/workspaces/herald/api/server.go:330) |

### 8.3 值得补的能力

| 优先级 | 能力 | 证据/动机 |
|---|---|---|
| P0 | HTTP 响应与请求体统一设定大小上限，并为超限返回确定错误。 | [core/httpclient/client.go:204](/home/cui/workspaces/herald/core/httpclient/client.go:204)、[api/handler_notify.go:72](/home/cui/workspaces/herald/api/handler_notify.go:72)、[api/handler_groups.go:74](/home/cui/workspaces/herald/api/handler_groups.go:74) |
| P1 | 弃用 URL query 凭据，迁移到 Authorization/X-API-Key header。 | [core/auth/auth.go:240](/home/cui/workspaces/herald/core/auth/auth.go:240)-[core/auth/auth.go:265](/home/cui/workspaces/herald/core/auth/auth.go:265)、[docs/api/rest.md:5](/home/cui/workspaces/herald/docs/api/rest.md:5)-[docs/api/rest.md:8](/home/cui/workspaces/herald/docs/api/rest.md:8) |
| P1 | 用契约测试或生成流程锁定 OpenAPI 与 handler 路由/schema 一致性。 | [docs/api/openapi.yaml:31](/home/cui/workspaces/herald/docs/api/openapi.yaml:31)、[api/server.go:240](/home/cui/workspaces/herald/api/server.go:240)-[api/server.go:330](/home/cui/workspaces/herald/api/server.go:330) |
| P2 | Docker 镜像固定可复现基础镜像并以非 root 身份运行。 | [Dockerfile:19](/home/cui/workspaces/herald/Dockerfile:19)-[Dockerfile:27](/home/cui/workspaces/herald/Dockerfile:27) |

## 9. 结论

**结论：质量未达标（安全加固前不建议按默认配置直接暴露公网）。** 领域拆包、错误传播、文档主干、CI 与测试体系建设较完整；但响应/请求大小无限制和 URL 凭据传播风险是可直接被外部输入触发的安全边界问题。全量测试文件存在和 CI 配置不等于本次已验证覆盖质量；本次没有运行测试或 race 检查。按优先级先解决三件事：

1. 为 provider HTTP 响应体和服务端 JSON 请求体加硬上限，并处理超限错误 [core/httpclient/client.go:204](/home/cui/workspaces/herald/core/httpclient/client.go:204)、[api/handler_notify.go:72](/home/cui/workspaces/herald/api/handler_notify.go:72)。
2. 停止接受 query-string 管理凭据并给旧客户端迁移窗口，避免密钥进入 URL 日志 [core/auth/auth.go:240](/home/cui/workspaces/herald/core/auth/auth.go:240)-[core/auth/auth.go:265](/home/cui/workspaces/herald/core/auth/auth.go:265)。
3. 为公网部署建立显式安全配置门（至少要求鉴权启用），并补对应配置/HTTP 集成验证 [core/auth/auth.go:83](/home/cui/workspaces/herald/core/auth/auth.go:83)-[core/auth/auth.go:105](/home/cui/workspaces/herald/core/auth/auth.go:105)。

## 修复记录

- HTTP 体积限制（commit `53a3a7194678de856efcedcab9bb39c30c245ff7`）：[api/server.go](/home/cui/workspaces/herald/api/server.go:354) 所有入站请求默认 1 MiB、`server.max_request_body_bytes` 与 `server.request_body_limits` 可覆盖；超限返回 413 并仅记录请求元数据。[core/httpclient/client.go](/home/cui/workspaces/herald/core/httpclient/client.go:99) 上游响应限制 1 MiB，超限截断读取并拒绝，状态错误不携带响应正文；日志/投递分页已有 500 条上限。
- 测试：`TestServer_RequestBodyLimit`、`TestClientRejectsOversizedResponseBody`、`TestStatusCodesRetryableClassification`。
- 查询凭据（commit `7bfa67c7b918a0e65c697f1094d524102245bcbe`）：[core/auth/auth.go](/home/cui/workspaces/herald/core/auth/auth.go:229) 原读取点为 `getJWTToken` 中 `?token=`（原 245 行）和 `getAPIKey` 中 `?api_key=`（原 265 行），现只接受 Authorization/X-API-Key header 与 token cookie；全仓搜索未发现其它服务端从 query 取 token/secret/key。策略：直接拒绝 query 凭据，不留兼容版。服务端没有记录请求 URL 的 access-log middleware；超限日志也不记录 URL/body。
- 测试：`TestAuthMiddlewareRejectsQueryCredentials`、`TestAuth_getJWTToken`、`TestAuth_getAPIKey`。


- 公网鉴权门禁（commit `d0cb1ef52538e4ee69dffb162a07328c4eca212a`）：[config/config.go](/home/cui/workspaces/herald/config/config.go:553) 启动校验拒绝公网/通配绑定且 auth 未启用；loopback 可关闭鉴权。README 与配置指南说明公网必配 `auth.enabled: true` 和 `auth.api_keys`。
- 测试：`TestValidate/rejects_public_bind_without_auth`、`TestValidate/allows_loopback_bind_without_auth`、`TestServer_AuthRejectsMissingCredentials`。
