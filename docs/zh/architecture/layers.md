# 源码分层分析

> 以源码为先、以抽象为先的 Herald 代码库阅读方式（先读后写，禁止靠目录名猜）。
> 范围：非测试 Go 代码、TypeScript 仪表盘、协议与配置 schema 文件；排除 `deps`、
> `node_modules`、`dist`、`build`、`.git`、`.cache`、`Testing`、`vendor`、覆盖率产物与
> 生成的二进制。分层指*职责*，不是目录名。

## 1. 术语表

| 术语 | 英文 | 定义 | File:line |
|------|---------|------------|-----------|
| Event | Gate-bound candidate | 去重门判定所面对的 §11 候选对象：事件身份、去重键、状态机维度、品类，以及折叠台账指纹。 | core/dedup/gate.go:37 |
| Notification | Normalized notification | 规则、去重、路由共同读取的流水线标准在途记录。 | core/types.go:18 |
| Delivery Task | One channel delivery unit | 一次绑定到具体服务商的投递尝试：任务 id、provider、渲染后的载荷、接收方、重试预算。 | core/types.go:81 |
| Queue | Task transport | 可靠消费队列（内存 channel 或 Redis Streams），在规划与 worker 消费之间承接任务。 | core/types.go:169 |
| Scheduler | Delayed re-enqueue capability | 可选队列能力：把任务暂存到未来某个时刻（异步重试的等待发生在队列里，不在 worker 里）。 | core/types.go:189 |
| Worker | Delivery consumer | 一个 worker 池 goroutine（local）或独立进程（remote），负责取任务并驱动 provider 投递。 | core/worker/pool.go:17 |
| Provider | Channel adapter | 每个投递渠道都要实现的那一个接口：`Deliver` 外加名称/类型/状态。 | core/provider.go:8 |
| Provider Factory | Provider constructor | 把 provider 类型字符串 + 配置 map 映射到一个具体的 Provider 实例。 | core/provider.go:43 |
| Runtime Manager | Provider registry | 持有已注册的 provider 实例、工厂、按 provider 的限流器，以及投递/重试的结算状态。 | core/runtime/manager.go:20 |
| Router | Static routing table | 把通知类型/级别解析成渠道列表，再经 channels 块展开为 provider 列表。 | core/route/router.go:8 |
| Rule Engine | Declarative policy table | expr 编译的 match/route/allow/suppress 规则，外加 for/group/inhibit/silence 语义，在路由之前求值。 | core/rules/engine.go:241 |
| Rule Store | Rule persistence | 按顺序排列的规则列表（内存或 JSON 文件）；顺序即求值优先级。 | core/rules/store.go:14 |
| Rule State Store | Stateful rule state | for 窗口 / group / inhibit 的在场状态（内存或 Redis），跨重启留存。 | core/rules/state.go:26 |
| Dedup Gate | §11 four-stage gate | 事件幂等 → 状态机 → 频控档位 → 投递，外加折叠台账。 | core/dedup/gate.go:87 |
| Template Manager | Template registry | 持有渠道无关的模板，把它们渲染成语义化的 RenderedData。 | core/template/manager.go:10 |
| Template Engine | Text rendering | 对 title/fields 做 `text/template` 替换，按内容 hash 缓存。 | core/template/engine.go:13 |
| Delivery Planner | Channel→task planning | 展开引用（plain/group/user/channel），并按 provider 渲染载荷、生成任务。 | core/service/planner.go |
| Notification Service | Pipeline orchestrator | 规则 → 路由 → 模板渲染 → 去重 → digest/feed 投影 → 入队；唯一的 Process() 路径。 | core/service/notification.go:98 |
| Delivery Store | Lifecycle state machine | 按任务维度的 accepted/queued/delivering/retrying/delivered/failed/dead 生命周期，含重试演进。 | core/deliverystore/deliverystore.go:16 |
| Log Store | Factual event log | 每条任务一行：发送/结果/耗时/错误；status 是事件结果，不是生命周期。 | core/logstore/logstore.go:24 |
| Retryer | Retry policy | 对已分类错误做指数/固定退避；异步 Defer 重新入队并盖上 NextRetryAt。 | core/retry/retryer.go:101 |
| Limiter | Per-provider rate limit | 挂在 provider 实例上的令牌桶限流器，每次 Deliver 之前都要等它。 | core/limiter/limiter.go:10 |
| Error Class | Provider error vocabulary | Temporary/permanent/rate_limited/authentication/invalid_request/timeout；重试只凭它决策。 | core/errclass/errclass.go:13 |
| Ack Store | Acknowledgement records | 以告警 id 为键的确认记录，升级触发前会先查它。 | core/ack/ack.go:27 |
| Escalation Manager | Ack-gated upgrade | 为规则投递设定时钟；ack 取消它，超时则向计划的 "to" 渠道触发；待升级记录持久化为 JSON。 | core/escalation/escalation.go:55 |
| Incident Ledger | Alert episode record | 每个告警回合的 open/acked/resolved 生命周期，同时以引擎 group key 与调用方 alert id 为键。 | core/incident/incident.go:26 |
| Digest Aggregator | Window fold | 把低频事件按受众×品类×模式收进日/周窗口；窗口翻转时产出一条摘要。 | core/digest/digest.go:128 |
| Leader Lock | Multi-instance lease | Redis SET NX PX 租约，让 digest 翻转与对账扫掠在整个集群每轮只跑一次。 | core/digest/lock.go:19 |
| Feeds Store | RSS pull projection | 把 rss 类投递存成 feed 条目；公共与 token 私有的 feed 都渲染它。 | core/feeds/store.go |
| Surface Registry | Contact-surface table | 受众×渠道的处理句柄，带 pending/active/invalid 生命周期与一次性绑定 token。 | core/audience/surfaces.go:114 |
| Relation Registry | Subscription table | 受众×品类×渠道的授权关系（订阅 vs 指派）及其策略位与审计。 | core/audience/relations.go:88 |
| Audience Manager | user: resolution | 具名受众/接收人；"user:" 引用展开为 provider 端点。 | core/audience/audience.go:77 |
| Group Manager | group: resolution | 具名投递群组；"group:" 引用展开为成员（单层）。 | core/groups/groups.go:45 |
| Roster Manager | Duty roster | 由外部推送的绝对时间段，可被规则的 silence 当作其时间表指名。 | core/roster/roster.go |
| Source Adapter | Platform entry points | Bot/公众号回调与应用内动作，把关注/取关收口到各注册表。 | core/audience/sources.go |
| Reconciler | External-state sweep | 定期探测存活的联系面；失效句柄置为 invalid，其上的订阅随之终止。 | core/audience/reconcile.go:46 |
| Delivery Policy | Intensity×urgency×mode | L0–L5 渠道侵扰度、品类紧急度，以及 fixed/escalation/parallel 模式规划。 | core/audience/intensity.go |
| Preference Registry | §7 frequency table | 按受众×品类×渠道选择 realtime/daily/weekly/none，并有静默下限。 | core/audience/preferences.go |
| Delivery Filter | §14 intersection | 目标 ∩ 关系权限 ∩ 活跃联系面，按受众/渠道/品类逐项校验。 | core/audience/filter.go:8 |
| Audit Trail | §12 event stream | 关系/联系面变更与去重折叠落在同一存储，读取时再按类型分开过滤。 | core/audit/audit.go:52 |
| Apps Registry | §13.1 integration namespaces | app token 鉴权，带 config/trigger/query 作用域；命名空间内独立的品类、策略、模板。 | core/apps/apps.go:158 |
| Callback Provider | §13.5 webhook callback | 把签名的投递结果/退订事件经同一套 queue→worker 管道发出。 | core/callback/provider.go:27 |
| WebSocket Server | Management channel | 远程 worker 经 WebSocket 注册/心跳/确认/收事件，并校验 origin 与 token。 | core/websocket/server.go:93 |
| Worker SDK | Remote worker client | 经 WebSocket 注册并消费队列任务的客户端；内嵌模式跳过控制面。 | worker-sdk/go/client.go:30 |
| Apps SDK | Integrator client | §13 各面（categories/policies/templates/dispatch/events/query）的类型化 Go 客户端。 | apps-sdk/go/client.go:19 |
| Auth | Operator authentication | API key + JWT 会话 + bcrypt 管理员登录，带按客户端的登录限流。 | core/auth/auth.go:23 |
| Credential Store | SQLite credential persistence | 仪表盘签发的 API key（hash + 掩码前缀）与轮转后的密码 hash。 | internal/store/store.go:37 |
| Metrics Sampler | Process sampling | 把 CPU/RSS/goroutines/在线 worker 写进有界环形缓冲，供仪表盘趋势图。 | core/metrics/metrics.go:46 |
| HTTP Client | Shared transport | 带超时与连接池的 HTTP 客户端，外加固定 IP 拨号与重试分类辅助。 | core/httpclient/client.go:56 |
| Config Schema | Startup configuration | YAML 配置结构体、环境变量展开、默认值与校验；provider schema 映射表。 | config/config.go:56 |
| Protocol | Wire messages | worker↔core 的 register/heartbeat/dispatch/ack/event 消息，带类型化信封。 | protocol/message.go:20 |
| CLI Entry | Process composition | `heraldd serve`/`worker` 依次构建队列、路由、runtime、各存储、API 与 WebSocket，然后阻塞到收到信号。 | cmd/heraldd/main.go:79 |
| Library Facade | Embedded App | 零配置的可内嵌派发器，为进程内使用而组合同一批组件。 | herald.go:49 |
| Dashboard | Operator UI | React SPA（Vite + TS），只对接 REST API；状态集中在一个 store 模块。 | dashboard/src/stores/herald.ts |
| Docs Site | VitePress documentation | 英文文档在 docs/，中文镜像在 docs/zh/；每个板块各有一条 sidebar。 | docs/.vitepress/config.mts:1 |

## 2. 分层地图

### 2.1 组合 / 入口层

- **实现：** `cmd/heraldd/main.go`（serve + worker 命令）、`herald.go`（内嵌 `App` 门面）。
- **配置模式：** `config/config.go`（`Config` 在 :56；子模式 `ServerConfig` :166、`WebSocketConfig` :174、`AuthConfig` :203、`ProviderConfig` :231、`QueueConfig` :247、`DedupConfig` :289、`DigestConfig` :305、`FeedsConfig` :332、`SourcesConfig` :352、`DeliveryConfig` :28、`RulesStateConfig` :156）。
- **协议文件：** `protocol/message.go`（worker↔core 线类型），`worker-sdk` 与 `core/websocket` 共用。
- **文档：** 文档站 `docs/`（英文）+ `docs/zh/`（中文镜像）；`docs/guide/configuration.md` 描述 YAML schema。
- **生命周期：** `serveCmd`（cmd/heraldd/main.go:79）加载并校验配置，再由 `assembleServe`（cmd/heraldd/serve.go:89）构建队列/路由/runtime/各存储/规则/群组/受众/花名册/注册表/worker 池，并接通 API 与 WebSocket 服务器；`serveDeps.start`（serve.go:617）启动所有 goroutine。`serveCmd` 阻塞等待 SIGINT/SIGTERM，随后 `serveDeps.shutdown`（serve.go:667）按启动的逆序逐个停止。`workerCmd`（main.go:140）启动远程 worker：只用共享队列，然后 `runRemoteWorkerControlPlane`（main.go:238）。

### 2.2 HTTP / API 层

- **实现：** `api/server.go`（`NewServer` :143，路由表 :270-365）、`api/handler.go` 与按资源的 `api/handler_*.go`（notify、logs、deliveries、providers、templates、rules、groups、audiences、rosters、incidents、feeds、apps、sources、callbacks、workers、keys、overview、metrics）。
- **配置模式：** `api.Config`（api/server.go:64）——所有接线依赖都是注入进来的，不在这里加载。
- **协议文件：** `docs/api/openapi.yaml`（OpenAPI 3 规范）；`docs/api/rest.md` + `docs/api/apps.md` 描述端点。
- **文档：** `docs/api/`（英文）；中文镜像在 `docs/zh/api/`。
- **生命周期：** HTTP 服务器在 `NewServer` 中绑定；`Start`（api/server.go:409）跑 `ListenAndServe`；`Shutdown`（:425）排空它。鉴权中间件（`withAuth`/`withApp`）守护受保护的端点。

### 2.3 投递流水线层

- **实现：** `core/service/notification.go`（`Process` :233）、`core/service/planner.go`（渠道→任务规划）、`core/service/dispatch.go`（app 派发面）。
- **配置模式：** 无——服务由 `api.Config` 接线（规则、去重、digest、feeds、升级、事件账本、群组/用户/渠道解析器、联系面）。
- **协议文件：** 无；它消费 `core.Notification`，产出 `core.DeliveryTask`。
- **文档：** `docs/architecture/modules.md` + `docs/guide/events.md`。
- **生命周期：** 无状态编排器；每次调用 `Process` 依次跑 规则 → 路由 → 渲染 → 去重门 → digest/feed 投影 → 入队。已结算的任务经 runtime 回调接收端回流（`core/callback/emitter.go`）。

### 2.4 路由 / 规则层

- **实现：** `core/route/router.go`（静态类型/级别/渠道解析）、`core/rules/engine.go`（求值）、`core/rules/rule.go`（Rule 模型）、`core/rules/for.go`、`group.go`、`inhibit.go`、`silence.go`、`shadow.go`、`escalation.go`（语义）。
- **配置模式：** `config.Config` 里的 `routes`、`level_routes`、`channels`；`rules: []rules.Rule`；`rules_store`、`rules_default_policy`、`rules_state`。
- **协议文件：** 无。
- **文档：** `docs/design-rule-engine.md`、`docs/architecture/core-design.md`。
- **生命周期：** 引擎持有编译好的规则，并按 `Rule.Priority` 保持活表有序（`sortRulesLocked`，core/rules/engine.go:341；高优先在前，同分保持插入序）；存储（内存/文件）在 `Reload` 时重载；状态存储（内存/redis）跨重启保留 for 窗口/group/inhibit 判定。

### 2.5 去重 / 折叠层

- **实现：** `core/dedup/dedup.go`、`core/dedup/gate.go`（四段门）、`core/dedup/policy.go`（档位）。
- **配置模式：** `dedup` 块（`enabled`、`window`、`category_tiers`、`category_windows`）。
- **协议文件：** 无。
- **文档：** `docs/architecture/modules.md` §11。
- **生命周期：** 内存 FIFO 有界表（4096），键为事件 id / 去重键 / 状态；重启即清空（口径是「重新投」，绝不「静默丢弃」）。

### 2.6 模板层

- **实现：** `core/template/manager.go`（注册表）、`core/template/engine.go`（text/template 执行）、`core/template/renderer.go`（HTML/Markdown/纯文本/JSON 渲染器）、`core/template/types.go`（Template/Field/Binding）。
- **配置模式：** `templates: map[string]template.TemplateConfig`。
- **协议文件：** 无。
- **文档：** `docs/guide/templates.md`、`docs/architecture/templates.md`。
- **生命周期：** 内存注册表；模板惰性解析并按内容 hash 缓存（engine.go:45-60）。

### 2.7 队列 / Worker 层

- **实现：** `core/queue/queue.go`（接口）、`factory.go`（memory|redis）、`memory.go`、`redis.go`；`core/worker/pool.go`（池）、`registry.go`（worker 注册表）。
- **配置模式：** `queue` 块（`type`、`size`、`workers`、`timeout`、`redis`）。
- **协议文件：** 队列本身无；远程 worker 控制面用 `protocol/message.go`。
- **文档：** `docs/runtime/worker.md`、`docs/runtime/sdk.md`。
- **生命周期：** 池启动 N 个本地 worker 并注册它们，另跑一个失效 worker 清扫器；取消时 worker 反注册并返回。Redis 队列在启动时创建消费者组；定时任务在 `XREADGROUP` 之前从 ZSET 移到 stream。

### 2.8 Runtime / Provider 层

- **实现：** `core/runtime/manager.go`（注册表 + 投递）、`core/provider.go`（Provider/Factory 接口）、`providers/builtin/registry/registry.go`（20 个内置工厂）、`providers/builtin/*`（具体渠道）。
- **配置模式：** `providers: map[string]ProviderConfig`（type、config、enabled、rate_limit）；类型→字段 schema 映射表在 `core/runtime/manager.go:50-69`。
- **协议文件：** 无（provider 是 HTTP/SDK 适配器）；worker runtime 用 `protocol/message.go`。
- **文档：** `docs/runtime/builtin.md`、`docs/providers/overview.md`、`docs/providers/*.md`。
- **生命周期：** 启动时 `RegisterFactory`/`RegisterProvider`；`Deliver`/`DeliverOnce`（同步 vs 异步重新入队）；`settle`（:558）把结果映射到投递存储 + 日志存储；`Close`（:632）关闭全部 provider。

### 2.9 状态 / 持久化层

- **实现：** `internal/store/store.go`（SQLite 凭据）、`core/rules/file.go`（JSON 规则文件）、`core/groups/file.go`、`core/audience/file.go`、`core/roster/file.go`、`core/escalation/escalation.go`（待升级 JSON）、`core/rules/state_redis.go`（Redis 规则状态）、`core/queue/redis.go`（Redis Streams）、`core/digest/lock.go`（Redis 租约）；其余都在内存。
- **配置模式：** `auth.store_path`、`rules_store`、`groups_store`、`audiences_store`、`rosters_store`、`escalation_store`、`rules_state`、`queue.redis`、`digest.redis_*`。
- **协议文件：** 无。
- **文档：** `docs/guide/configuration.md`。
- **生命周期：** 存储启动时打开（文件格式损坏 / Redis 不可达即快速失败），关闭时落盘；JSON 存储走 temp+rename 原子写；SQLite 用 WAL 与单连接。

### 2.10 受众 / 关系层

- **实现：** `core/audience/surfaces.go`、`relations.go`、`audience.go`、`store.go`、`sources.go`、`filter.go`、`intensity.go`、`mode.go`、`preferences.go`、`reconcile.go`、`matrix.go`、`overlay.go`；`core/groups/*`；`core/roster/*`。
- **配置模式：** `audiences`、`recipients`、`audiences_store`、`groups`、`groups_store`、`rosters_store`、`sources`、`delivery`。
- **协议文件：** 无。
- **文档：** `docs/design-audience-model.md`、`docs/design-audience-relations.md`、`docs/design-audience-boundaries.md`、`docs/design-audience-audit.md`、`docs/design-notification-groups.md`。
- **生命周期：** 注册表都在内存，可选 JSON 持久化；入口适配器与对账器只在配置开启的条目上跑；审计记录器始终挂上。

### 2.11 Apps / 回调层

- **实现：** `core/apps/apps.go`（注册表 + 作用域）、`policies.go`、`templates.go`、`callback.go`（回调 URL 策略）；`core/callback/provider.go`（队列侧 provider）、`emitter.go`（结算/退订钩子）、`event.go`（事件种类）。
- **配置模式：** `apps: map[string]AppConfig`（带作用域的 token）；`card_callback`（飞书加密 key）。
- **协议文件：** /api/v1/apps/** 面用 `docs/api/openapi.yaml`；`apps-sdk/go/faces.go` 是类型化客户端。
- **文档：** `docs/api/apps.md`、`docs/guide/integration.md`。
- **生命周期：** 注册表从配置播种（校验点）；回调发射器把签名事件入同一队列；provider 经投递管道带重试地发出它们。

### 2.12 鉴权 / 运维面层

- **实现：** `core/auth/auth.go`（API key + 登录 + 密码）、`core/jwt/jwt.go`（会话）、`core/user/user.go`（bcrypt 用户）、`internal/store/store.go`（持久化凭据）。
- **配置模式：** `auth` 块（`enabled`、`api_keys`、`secret_key`、`admin_user`、`login_rate`、`login_burst`、`store_path`）。
- **协议文件：** `docs/api/openapi.yaml`（鉴权端点）。
- **文档：** `docs/guide/configuration.md`（鉴权一节）。
- **生命周期：** 内存用户 + 可选 SQLite 存储；JWT secret 未配置时每进程随机；动态 key hash 缓存并在写入时失效。

### 2.13 可观测层

- **实现：** `core/metrics/metrics.go`（采样器）、`core/logstore/logstore.go`（投递日志）、`core/deliverystore/deliverystore.go`（生命周期）、`core/audit/audit.go`（审计痕迹）、`internal/logger/logger.go`。
- **配置模式：** `metrics` 块（采样间隔、保留时长）。
- **协议文件：** 无；API 暴露 `/api/v1/logs`、`/deliveries`、`/metrics/history`、`/audit`。
- **文档：** `docs/architecture/coverage.md`、`docs/runtime/overview.md`。
- **生命周期：** 采样器 goroutine 由 `newMetricsSampler`（cmd/heraldd/main.go:118）启动，关闭时停止；日志/投递/审计存储都是有界内存环。

### 2.14 Worker 控制面 / SDK 层

- **实现：** `core/websocket/server.go` + `hub.go`；`worker-sdk/go/client.go`；`protocol/message.go`。
- **配置模式：** `websocket` 块（`addr`、超时、`allowed_origins`、`auth_token`、`max_message_size`）；`worker` 块（客户端侧）。
- **协议文件：** `protocol/message.go` 是唯一的线契约（register/register_ack/heartbeat/dispatch/ack/error/event）。
- **文档：** `docs/runtime/worker.md`、`docs/runtime/sdk.md`、`docs/architecture/protocols.md`。
- **生命周期：** 服务器升级 WebSocket，校验 origin + token，注册 worker；hub 把心跳/断连转发给注册表；worker SDK 连接、注册、心跳并消费队列。

### 2.15 前端层

- **实现：** `dashboard/src/`（Vite + React + TS）：`main.tsx`、`App.tsx`、`api/index.ts`、`stores/herald.ts`、`pages/*.tsx`、`layouts/DashboardLayout.tsx`、`theme/*`。
- **配置模式：** 无（前端只读 REST API）；构建配置在 `vite.config.js`、`vitest.config.ts`。
- **协议文件：** 无；它对接 `api/server.go` 的路由。
- **文档：** 仓库内无（仪表盘是独立产物）；文档覆盖它消费的 API。
- **生命周期：** SPA 引导；单一 Zustand 风格 store 持有鉴权 + 数据；按页面拉取/刷新。

### 2.16 文档层

- **实现：** `docs/.vitepress/config.mts`（站点配置 + sidebar）、`docs/**/*.md`（英文）、`docs/zh/**/*.md`（中文镜像）、`docs/public/images/architecture-{en,zh}.svg`。
- **配置模式：** VitePress 配置在 `docs/.vitepress/config.mts`。
- **协议文件：** 无。
- **文档：** 文档本身就是文案面（英文为主，中文镜像在 `docs/zh/`）。
- **生命周期：** 静态站点由 `pnpm --dir docs build` 构建（GitHub workflow `docs.yml`）。

## 3. 分层为什么存在（证据）

1. **唯一的标准流水线。** `NotificationService.Process`（core/service/notification.go:233）是 规则→路由→渲染→去重→digest/feed→入队 唯一发生的地方，因此每个入口（HTTP notify、app 派发、digest 翻转、升级、恢复）都继承同一套语义；把路由从投递中拆出是显式的（core/service/planner.go，`expandRefs` 在 core/service/refs.go:26）。
2. **投递状态与事实日志分离。** `deliverystore` 持有 accepted/queued/delivering/retrying/delivered/failed/dead 生命周期（core/deliverystore/deliverystore.go:16），`logstore` 只记录事件结果（core/logstore/logstore.go:24）；`runtime.settle` 独立地写这两者（core/runtime/manager.go:558），所以「dead 还是 failed」绝不泄漏进日志词汇。
3. **重试是策略，不是 provider 逻辑。** `core/errclass` 定义六类（core/errclass/errclass.go:13），`core/retry` 只凭这套词汇决策（core/retry/retryer.go:37 `isRetryable`），因此没有任何 provider 拥有退避；`Retry-After` 建议会被采纳但设了上限（retryer.go:196）。
4. **队列是一个带两种后端的接口。** `core.Queue`（core/types.go:169）外加可选 `Scheduler`（core/types.go:189），让内存（core/queue/memory.go）与 Redis Streams（core/queue/redis.go）互换而不必动 worker；池按能力选择异步重新入队（core/worker/pool.go:31）。
5. **Runtime 注册表把 provider 身份与配置解耦。** `Manager` 把工厂与实例分开持有（core/runtime/manager.go:20），所以配置里的 provider 名绝不会与内置工厂冲突，`ReplaceProvider` 可以热替换实例（manager.go:710）。
6. **规则只编译一次，求值开销很低。** `compileExpr` 是唯一从表达式到程序的路径（core/rules/engine.go:79），带 AST 守卫、节点上限与 2s 超时（engine.go:45-66）。优先级来自 `Rule.Priority`（core/rules/rule.go:113），而不是列表位置：`sortRulesLocked`（core/rules/engine.go:341）在每次写入时按优先级重排活表——高优先在前、同分保持插入序——（Put/Delete 路径，engine.go:399/:405/:464），因此求值时只是按已排好序的表走，不再排序。
7. **有状态的规则语义有可换的存储。** `StateStore`（core/rules/state.go:26）默认用内存，多实例时用 Redis（`NewRedisStateStore`，core/rules/state_redis.go:21），因此 for 窗口/group/inhibit 判定跨重启存活；规则定义本身经 `Store`（core/rules/store.go:14）持久化为内存或原子 JSON 文件（core/rules/file.go:25）。
8. **去重是一把锁下的四段阶梯。** `Gate.Decide`（core/dedup/gate.go:173）依次跑 幂等 → 状态机 → 频控档位 → 投递，并维护折叠台账，全部 FIFO 有界（gate.go:87），这样审计行能展开「这条 ×N」而内存不会无限增长。
9. **受众身份是两张注册表，不是一张。** 联系面（core/audience/surfaces.go:114）与关系（core/audience/relations.go:88）分开，因为句柄可以存在而没有授权，反之亦然；`Filter`（core/audience/filter.go:8）在发送时按 §14 顺序取交集。
10. **拉式渠道不是 provider。** `rss` 归为 L0 侵扰度（core/audience/intensity.go:13），服务把 rss 类投递投影进 `feeds.Store`，而不是入队一个 provider 任务（core/service/enqueue.go:29），因此读者按自己的节奏来拉。
11. **Digest 与对账共用一把 leader 租约。** `digest.LeaderLock`（core/digest/lock.go:19）是一个 Redis SET NX PX 租约，翻转循环（cmd/heraldd/serve.go:643）与对账循环（cmd/heraldd/main.go:450，在 serve.go:658 启动）都用它，因此整个集群每轮只翻转/扫掠一次，各功能不必各写一份锁代码。
12. **升级由时间驱动，不靠事件。** 待升级记录持久化为 JSON，重启后恢复（core/escalation/escalation.go:55），因为它的意义就在于没人确认时仍然出手；ack 存储是独立接口（core/ack/ack.go:27），升级在触发前先查它。
13. **Provider 错误只在边缘分类一次。** `httpclient` 把 HTTP 408/429/5xx 包成可重试（core/httpclient/client.go），provider 代码用 `errclass.New`，因此重试层从不需要窥探 provider 内部。
14. **Webhook 回调借投递管道走。** `callback.Emitter`（core/callback/emitter.go:34）把已结算的投递与退订变成签名事件，作为普通任务入队（core/callback/provider.go:27），因此回调拥有与任何投递相同的重试预算与至少一次语义。
15. **Worker 控制面只有一个协议。** `protocol/message.go`（protocol/message.go:20）是 core 与远程 worker 之间唯一的线契约；服务器（core/websocket/server.go:93）与 SDK（worker-sdk/go/client.go:30）都 import 它，所以改一条消息就是两端同时的编译期断裂。
16. **凭据持久化藏在接口后面。** `core/auth` 依赖 `PasswordStore`/`APIKeyHashes`（core/auth/auth.go:46-59），`internal/store` 在 SQLite 之上实现它们（internal/store/store.go:37），使鉴权包不必依赖 sqlite。
17. **配置在启动时校验，绝不悄悄收敛。** `config.Validate`（config/config.go:632）拒绝解析不了的档位/窗口与坏绑定；`expandRefs` 与 `Gate.tierFor` 只对跳过校验的调用方降级（core/dedup/gate.go:140），因此坏配置是响亮地失败，而不是悄悄重定级。
18. **内嵌门面复用同一套组合。** `herald.App`（herald.go:49）与 `serveCmd`（cmd/heraldd/main.go:79）一模一样地构建队列/路由/runtime/服务/池，因此库与 CLI 的行为不可能分叉。
19. **仪表盘是纯粹的 API 客户端。** `dashboard/src/stores/herald.ts` 持有全部状态，`dashboard/src/api/index.ts` 承载 HTTP 调用；页面从不触碰传输细节，因此前端可以整体替换而不改 REST 契约。
20. **文档按语言与板块切分。** 英文在 `docs/`，中文镜像在 `docs/zh/`（docs/.vitepress/config.mts:1），让每类读者都有稳定路径；architecture/runtime/api/guide/providers 各有自己的 sidebar 板块。