# Audience 领域模型：Phase 0-9 改造审计（存档）

> 状态：**存档**。承接原「Audience 领域模型：架构改进计划书」（Phase 0-9，已收官；
> 该页 2026-10-07 已重写为[受众领域模型总纲](./design-audience-model)，计划原文由 git
> 历史保存）。本文按计划 Phase 0 的输出要求（当前实现 → 目标模型 → 差异 → 改动文件 →
> 兼容性风险）记录事实底稿与逐批执行记录，所有结论带 file:line。受众层扩展
> （统一订阅与投递中枢）不在此文跟踪，见[关系详设](./design-audience-relations)与 todo。

## 一、当前实现：通知的完整旅程

```
POST /api/v1/notify
  └─ api/handler_notify.go: handleNotify → core.Notification
      └─ core/service/notification.go: NotificationService.Process (147)
          ├─ 规则引擎求值 (rules.Evaluate) → 可改写通道/抑制/挂起/折叠/静默
          ├─ 无显式 channels 时静态路由 core/route/router.go: Route(type, level)
          ├─ 模板渲染 (template.Manager.Render)
          ├─ 去重 (dedup.Check，按内容 key)
          ├─ enqueue (340)：按通道引用逐个展开
          │    ├─ 普通通道 → provider 实例
          │    └─ group:<id> → core/groups 成员 (expandRef, 367)
          │    └─ enqueueOne (386) → service/planner.go: DeliveryPlanner.Plan
          │         每通道一个 core.DeliveryTask → queue.Push
          └─ 升级/事故台账旁路 (escalation / incident)
      └─ core/worker/pool.go: workerLoop (63)
          Pop → core/runtime/manager.go: Manager.Deliver (399)
              ├─ 限流等待 → 失败则 NewTaskLog+UpdateStatus("failed")
              ├─ logstore.Add(pending)  ← 唯一状态载体
              ├─ retry.Retryer.Execute（同步重试，max 3/指数退避）
              ├─ 失败 → UpdateStatus("failed", err)；成功 → UpdateStatus("success")
          → queue.Ack / queue.Nack
```

### 事实清单

| # | 环节 | 现状 | 证据 |
|---|------|------|------|
| 1 | Notification | `core.Notification`：ID/Type/Level/**Channels[]**/**Recipients map[key][]target**/TemplateRef/Params/Content{Title,Body}/CreatedAt。API 请求 `NotifyRequest` 同名同构（+title/body） | core/types.go:18-28；api/handler_notify.go:11-22 |
| 2 | Delivery Task | `core.DeliveryTask`：ID/**Provider**/Targets[]/Payload{Kind,Content,ProviderTemplate,Raw}/Level/AlertID/**RetryCount**/CreatedAt。一次 notify 每通道一个 task（group 成员逐通道展开），`ProcessResult.TaskIDs/Accepted/Failed` 汇总 | core/types.go:37-49；service/notification.go:340-418 |
| 3 | 状态模型 | **无 Delivery 显式状态机**。状态=`logstore.TaskLog.Status` 字符串：`pending / success / failed / shadow`（shadow 为规则观察条目）。写入点仅 runtime.Manager.Deliver 与规则观察路径；无枚举、无 attempts/next_retry_at | core/logstore/logstore.go:19-30,192；runtime/manager.go:423,444,448 |
| 4 | 重试 | `retry.Retryer.Execute`：默认 max=3、exponential、initial 1s、max_delay 1m；**同步循环**（一次 Deliver 调用内重试），task.RetryCount 累加；失败最终 Nack 后**不重新入队** | core/retry/retryer.go:51-97；worker/pool.go:90-101 |
| 5 | Queue | 接口 Push/Pop/Ack/Nack/Size/Close；实现 memory/redis | core/types.go:84-91；core/queue/memory.go, redis.go |
| 6 | Worker | local workerPool：Pop→Deliver→Ack/Nack；remote worker 同 interface；Registry 管心跳/过期 | core/worker/pool.go；core/worker/registry.go |
| 7 | Logs | **TaskLog 即投递状态与日志（同一份数据）**，日志不保存历史事件流；stats 按 level/provider/status 聚合；API：/logs、/logs/stats、/logs/{id} | logstore.go:169-211（Stats）；api/server.go:144-146 |
| 8 | 群组（Audience group: 形态） | `groups.Group{ID,Description,Members[{Channel,Recipients}]}`，**单层展开不嵌套**；Manager 实现 `GroupResolver.ExpandGroup`；"group:id" 在 expandRef 展开为逐成员通道引用；**引用不存在 group → 该通道显式失败，其余照发**；API：/groups CRUD + groups_store 持久化 | core/groups/groups.go:32-46；service/notification.go:367-383,142-144；api/handler_groups.go |
| 9 | user:/Recipient/Endpoint | **不存在**。`Recipients` 字段语义是按渠道 pin 目标列表（provider 期望的地址），非受众模型 | grep 全库无 user: 级概念；types.go:23 |
| 10 | Channel | 无独立配置块。静态路由 = `routes`（按 type）+ `level_routes`（按级别），`Route(type,level)` type 优先再 level；notify 显式 `channels` 覆盖两张表 | core/route/router.go；config/config.go:24 |
| 11 | Config | 顶层键：server/websocket/worker/auth/providers/routes/queue/retry/dedup/templates/rules(+store/policy/state)/groups(+store)/rosters_store/escalation_store/incident_limit/card_callback。**宽松 yaml 解析**（未知键静默忽略）；`ExpandEnv()` 支持 `$VAR` | config/config.go:18-60,181,299-317 |
| 12 | 渠道语义混乱残留 | 与目标模型的词汇混用点：`channels` 在 notify/规则/路由里都指"provider 实例引用"；Template Binding 按 channel 选格式（与渠道解耦部分存在）；ProviderCapability 已存在（format 选择用） | service/planner.go:57-76,247 |
| 13 | 测试底稿 | handler_*_test.go 齐全（notify/groups/rules/rosters/logs/worker）；service/planner/retryer/pool/logstore 均有测试；覆盖率门禁 100%（zero_check） | api/、core/ 各 *_test.go |

## 二、目标模型 → 现状差异总表

| 目标（原计划书） | 现状 | 差距 |
|----------------------------------|------|------|
| Notification ≠ Delivery Task | 已分离（core.Notification / core.DeliveryTask） | ✅ 语义成立；TaskLog 把状态挂到 task 上 ✓ |
| Delivery 独立状态机（accepted/queued/delivering/delivered/failed/retrying/dead）+ attempts/max_attempts/last_error/next_retry_at | Status 字符串 4 值（pending/success/failed/shadow）；RetryCount 已存在；重试同步不排队 | ❌ 状态机枚举化 + 状态挂 Delivery（或等价物）→ 另需 next_retry_at 才能支撑「Nack 重新入队」式异步重试 |
| Retry 属 Delivery、错误统一分类（Temporary/Permanent/RateLimited/Auth/InvalidRequest/Timeout） | 错误为裸 error；retryer 对所有错误一视同仁 | ❌ Provider 错误分类接口缺失 |
| Audience = group + user 两级 | 只有 group: 形态 | ❌ user:/Recipient/Endpoint 需新增（MVP：本地配置模型） |
| Channel 独立配置块（channels: {ci: {providers: [...]}}） | routes/level_routes map 承担静态映射 | ◑ 能力等价但模型不同；可选择性演进 |
| Logs 与 Delivery 状态分离（日志=历史事件流） | 日志即状态（一份数据） | ❌ 按「日志与状态分离」演进的成本最高点（需引入事件流或拆分存储） |
| Idempotency（idempotency_key） | dedup 按内容 key 窗口去重（非显式幂等键） | ◑ 语义不同：dedup 防重复投递，幂等键防重复创建 |
| Provider Capability | 已有 format 选择能力（selectFormat/getCapability） | ✅ 部分满足计划建议的 Provider Capability |
| 配置块：audiences/recipients/endpoints | 不存在（宽松解析静默忽略） | ❌ 新增 |

## 三、改动文件清单（Phase 1+ 参考，勿视为已承诺）

| 文件 | 改动方向 |
|------|---------|
| core/types.go | Delivery 状态枚举常量、TaskLog 字段扩展（attempts/next_retry_at）、Audience 引用类型（group:/user: 前缀统一） |
| core/logstore/logstore.go | 状态值枚举化（保持 JSON 序列化兼容）；如做「日志与状态分离」则拆事件流存储 |
| core/runtime/manager.go | 状态写入点对齐新枚举；错误分类转换（Temporary/Permanent/...） |
| core/retry/retryer.go | 按错误分类决定重试/放弃（RateLimited 退避、Permanent 直接 dead）；可选：异步重新入队 |
| core/service/notification.go、planner.go | expandRef 增加 user: 解析；Channel 解析层（显式 channels → Channel 块 → routes 兼容） |
| core/groups/ → core/audience/（新建） | 承载 recipients/endpoints 一级（group 保留为 audience 聚合） |
| config/config.go | channels/audiences/recipients 配置块（宽松解析现存语义：未落地前静默忽略） |
| api/handler_notify.go | **本次不强制改**（notify 的 channel/audience/idempotency_key 字段全部可选新增，向后兼容） |
| api/server.go、handler_groups.go | 如新增 audience/recipient 端点（第一期可只做配置化，不做 API） |

## 四、兼容性风险与护栏

1. **notify 请求/响应保持兼容**：新增字段全部可选；`channels` 显式语义、`code/message/data` 响应壳、`notification_id/task_ids/accepted/failed` 不变。Breaking Change 需按原计划的破坏性变更规程走（说明+迁移+文档+测试）。
2. **TaskLog Status 字符串**：从 4 值向枚举扩展时**只增不删不改名**（`pending/success/failed/shadow` 全保留，新增状态为补充），保证既有 dashboard 与 API 消费者不破。
3. **groups API 与 group: 引用**保持现状（`group:` 不改语义；`user:` 为全新前缀，各走各的解析路径）。
4. **routes/level_routes** 保留为静态路由的等价物，引入 channels 块时以「channel 显式 > channels 块 > routes 表」的优先级叠加，不删除既有键。
5. **重试语义**：当前同步重试对调用方可观测（一次 Deliver 阻塞完整个策略）；改为异步重新入队会改变端到端耗时与日志时序——需在 Phase 3 单独设计，不能顺手改。
6. **宽松 yaml 解析**是把双刃剑：新配置键落地前的静默忽略已在文档（configuration.md「领域模型与配置块」）注明，避免用户误以为已生效。
7. 每 Phase 完成必须过全量门禁（Go race + covermerge gate 100 + golangci-lint + dashboard 测试/构建 + docs 构建），与仓库既有纪律一致。
## 五、执行记录（2026-10-04：Phase 0-9 全批落地，计划收官）

前两批改动已交付并过全量门禁（race 全绿 / covermerge gate 100 GREEN / golangci-lint 0 issues / dashboard 100% / docs build 过）；第三批（Phase 6）同样过关。按上表差异逐项回填：

### Phase 1 MVP：`user:` 级受众（配置化）

- `core/audience/`（新包）：`Endpoint{Type,Target}` / `Recipient{Endpoints}` / `Audience{Recipients}`；`Manager.ExpandUser` 解析——**audiences 表优先**，未命中回落 recipients 表；`NewManager` 启动校验（非法 id、audience 引用未知接收人、接收人零端点、端点 type/target 空或超长 → 报错拒起）：`user:` 引用绝不静默落到空。
- `config` 新增 `audiences` / `recipients` 顶层块（宽松解析由此落地生效；见 [configuration.md「领域模型与配置块」](/guide/configuration#领域模型与配置块)）。
- `core/service/notification.go`：`expandRef` 新增 `user:` 分支（`UserResolver` 接口 + `SetUserResolver`，与 GroupResolver 同构）；`mergeUserEndpoints` **按 provider 合并端点**——同一 provider 的所有目标捆绑进一次投递，provider 顺序排序保证任务/日志跨运行稳定。未知 user / 无 resolver → 该通道显式失败，其余照发（与 group: 行为一致）。
- `cmd/heraldd/main.go` 装配 `audience.NewManager`（非法表退出码 1）；`api.Config.Users` 透传。
- 按轻量原则**未做**：channels 独立配置块、audiences/recipients 运行时 API（idempotency 请求字段已由第三批 Phase 6 补齐，见下）。

### Phase 3 MVP：Delivery 状态机（枚举化 + 同步重试）

- `core.DeliveryStatus` 枚举（accepted/queued/delivering/retrying/delivered/failed/dead）+ `DeliveryTask.Status` 字段；planner 创建任务即置 `queued`。
- `core/retry/retryer.go`：新增 `ErrMaxRetries` sentinel 供 errors.Is 判定 **dead**——重试轮数耗尽的 retryable 失败返回 `max retries exceeded: <last>`（消息文本与历史一致，仅升级为可解包标记）；重试轮驱动 `RetryCount`（已完成的重试数）与 `Status=retrying`。
- `core/runtime/manager.go`：每次尝试前置 `delivering`；终态按 **delivered / failed（永久失败与限流中止）/ dead（可重试错误耗尽）** 写入：`errors.Is(deliverErr, retry.ErrMaxRetries)` 区分 dead 与 failed。
- **TaskLog wire 不变**：`pending/success/failed/shadow` 原样保留——dead/retrying 只体现在 DeliveryTask 状态，dashboard 与 logs API 无感知（护栏 2 就地兑现）。
- 错误分类按护栏 5 保持现状：`RetryableError` / `httpclient.RetryableError` 二元标记（HTTP 408/429/5xx、网络错误）→ 可重试；其余立即 failed（此前审计稿中"401 也会重试 3 次"的表述有误，已按上述事实修正理解）。异步重新入队与 `next_retry_at` 留后续 Phase（会改变端到端耗时与日志时序，需单独设计）。

### Phase 6 MVP：notify API 领域字段（channel / audience / data / idempotency_key）

按原计划的 notify 字段设计与幂等设计、护栏 1（新增字段全部可选）落地，全部向后兼容：

- `api/handler_notify.go`：`NotifyRequest` 新增 `channel`（单数写法，与 `channels` 并集）、`audience`（受众引用数组，逐项走既有 `expandRef` 链——`group:` / `user:` / 裸渠道名）、`data`（模板数据，与 `params` 合并、`params` 重名优先）、`idempotency_key`；合并逻辑抽为 `notifyChannels` / `notifyParams` 两个纯函数，成功响应抽 `notifyData` 复用（首次投递与幂等 replay 同一构造）。
- `api/idempotency.go`（新）：`notifyIdempotency` 内存表——mutex + map + FIFO 队列，容量 1000（`defaultNotifyIdempotencyCap`，按需逐出最旧）；`get` / `put`，重复 put 保留首条记录。`NewHandler` 即挂载，零配置。
- 幂等语义：仅记录**成功受理**（`code 0`）的结果；命中 → 直接返回首次的 `notification_id` / `task_ids`，**不触碰投递管线**（队列零增长）；失败请求（422）不记录、同键可修正重试。进程生命周期内有效，重启即清空（MVP 无持久化，已在文档明示）。
- 覆盖：`handler_notify_test.go` 新增 7 个子测试（channels+channel 并集、channel 单用、audience 展开 `user:` 引用、data 并入 params、params 重名优先、data 独用、幂等 replay/键独立/失败不记录）+ `idempotency_test.go` 表驱动直测（miss/roundtrip/重复 put 保首条/FIFO 逐出/容量回退/自定义容量）。
- 设计取舍：幂等放 handler 层而非 service 层——键是请求字段、命中路径短路在 Process 之前（dedup 是内容指纹去重、幂等是请求键回放，两者语义不同、互不替代）；表内不存请求原文，只存 `service.ProcessResult`。

### Phase 2 MVP：Routing 边界显式化（决策与执行分离）

计划 Phase 2 的链路（Notification → Audience resolution → Channel routing → Delivery Task）在上一批已具备能力（`expandRef` 的 group:/user: 展开 + Router/rules 的渠道路由），本批补齐的是**边界本身**：

- `core/service/notification.go`：新增 `expandRefs(refs)` —— 引用集合 → 投递目标的**纯决策**步骤（不触碰队列）；单条引用解析失败记录为该引用的 `ChannelError`、其余照常展开。`enqueue` 改为先取决策、后执行入队（`enqueueOne`），Routing（决定谁收到什么）与 Delivery（Plan/Push/重试）在代码上有明确分割点。行为零变化。
- `core/service/routing_test.go`（新）：`expandRefs` 决策层直接单测——空引用集、裸渠道透传、group:+user: 混合展开（`user:alice` 按 provider 合并端点）、失败引用不阻断批量、无 resolver 时显式失败；`ctx`/queue 零接触（纯函数可测性即边界证明）。

### Phase 4 MVP：Queue / Worker 五项核对 + 投递字段回填

按原计划逐项核对（enqueue/dequeue/ack/worker/retry）：

- **enqueue / dequeue / ack**：`core.Queue` 接口（Push/Pop/Ack/Nack/Size/Close）与 memory 队列实现齐全（buffered chan、closed 守卫、ctx 取消；内存队列 ack 语义 = Pop 即出队，Ack/Nack 按设计为 no-op）；worker 成功→Ack、失败→Nack 已接线。**无缺口**。
- **worker**：Pool.workerLoop 只做「取任务 → 调 Provider → Ack/Nack」，不携带路由/模板/接收人知识（worker 边界成立，路由决策在 Phase 2 已独立）。**无缺口**。
- **retry**：同步重试在 runtime.Deliver 内（Phase 3），一次 Pop 覆盖整个重试周期，耗尽→dead。**无缺口**。
- **投递字段核对回填**：计划建议 Delivery 保存 `attempts` / `max_attempts` / `last_error` / `next_retry_at`。检查发现 `last_error` 与 `max_attempts` 只存在于重试策略/任务日志、不在任务对象上。本批补齐：
  - `core.DeliveryTask` 新增 `MaxAttempts int`（投递预算快照：retry 策略轮数 + 首试；无 retryer 时为 1，由 runtime 在首次尝试前钉入）与 `LastError string`（终态错误：failed/dead 写最终错误、delivered 清空、限流中止同样记录；与任务日志的完整历史并存，即计划所言「Delivery 自带 last_error」）；
  - `core/retry.Retryer.MaxAttempts()`（= MaxRetries()+1）暴露预算；
  - `next_retry_at` 依旧不做：同步重试在单次 Deliver 内完成、无排队语义（Phase 3 执行记录已声明，留异步批次）。
- **核对测试**：`core/worker/pool_phase4_test.go`（新）——真 runtime + 真 worker loop 的投递五项全链路核对（成功→delivered+ack、失败→failed+nack+last_error、retryable 耗尽→dead+预算/重试数/错误残留断言；经队列 mutex 观察点同步、race 干净）；`manager_status_test.go` 四个终态测试补 MaxAttempts/LastError 断言；`retryer_test.go` 补 MaxAttempts 预算测试。
- 外部依赖零新增（原计划：维持 MemoryQueue 为第一队列，Redis 队列不动、不引新后端）。

### Phase 5 MVP：Provider 错误分类（六类词汇接入 retrying/dead 判定）

落点：共享错误词汇 **core/errclass** + httpclient / retry 两处**单点接入**——所有走 httpclient 的 provider（aliyunsms/neteasesms/tencentsms）零改动获得分类。

- `core/errclass`（新包）：`Class` 六类常量（temporary / permanent / rate_limited / authentication / invalid_request / timeout）+ `Error{Class, Err}` 包装。**分类骑错误链、不骑马甲**：`Error()` 原样返回被包装文本（任务日志与 API 响应保持历史形状），`Of()` 经 errors.As 穿透多层 `%w` 包装，`Retryable()` 判定——前三类可重试、后三类立即失败。
- `core/httpclient` 单点接入：非 2xx → `classifyStatus`（408→timeout、429→rate_limited、≥500→temporary；其余 4xx 保持**不分类**——坏请求/坏凭据重试不可能成功，语义未变只是显式化）；传输层错误 → `sendError`（net.Error 超时→timeout、其余→temporary）；错误文本不变。
- `core/retry` 判定接入：`isRetryable` 改为 **errclass 优先**（有分类看分类、无分类回落），`RetryableError` / `httpclient.RetryableError` 旧标记保留兼容（既有测试原样通过）。
- 测试：errclass 单元（nil 透传 / 文本保真 / 穿透包装 / 8 例分类表）；`TestClassifiedStatusErrors`（7 状态码→类 + IsRetryable 与类一致性）；`TestRetryerClassifiedErrors`（生产形状策略：temporary/rate_limited/timeout 耗尽→ErrMaxRetries 共 3 次调用；permanent/authentication/invalid_request→1 次即败、非 dead）；`TestDeliverClassifiedErrorTerminals`（端到端：authentication→failed 不重试、rate_limited→重试耗尽→dead）。
- 未做（护栏 5 留批）：401 等 4xx 依旧不重试（语义与历史一致）；RateLimited 尚无 per-class 退避（Retry-After 未用，沿用统一退避策略）。
  - **后批补齐（2026-10-05）**：Retry-After 已接入——`errclass.Error` 增 `RetryAfter` 提示，`httpclient.statusError` 在 429 时解析 `Retry-After`（delay-seconds / HTTP-date，非正值与过去时点不算建议），retry 侧提示优先于退避曲线、`retry.max_delay` 封顶（MaxDelay 为 0 不封顶）；`NewWithRetryAfter` / `RetryAfterOf` / `parseRetryAfter` 各分支单测 + retryer 提示路径 seam 测试。

### 后批：异步重新入队（2026-10-05）

留批第 1 项按护栏 5 独立设计后落地：可重试失败不再占住 worker 睡完退避曲线，任务带 `next_retry_at` 回到队列，到点由队列放行、worker 立即去取下一个任务。

- `core/types.go`：`DeliveryTask` 增 `NextRetryAt *time.Time`（defer 时戳、下次尝试开始即清空，wire 字段 `next_retry_at` omitempty，worker 协议向后兼容）；新增 `Scheduler` 可选能力接口（`Schedule(task, delay)`），不进 `Queue` 主接口——没有延迟持留能力的自定义队列原样保留同步语义。
- `core/retry`：`Defer(task, err)` 单发决策——retryable 且预算未尽 → `RetryCount++`、`Status=retrying`、`NextRetryAt=now+delay`、返回 `*Deferred{Delay, Err}`；预算尽 → `ErrMaxRetries` 包装（dead）；不可重试 → 原样返回（failed）。`waitFor` 抽出共享：Retry-After 提示与 `max_delay` 封顶规则同步/异步两路一致。预算（`RetryCount`）随任务走，跨重新入队累计，跨 redis 序列化存活。
- `core/queue`：memory 实现延迟持留（最小堆；Pop 先放行到期项再看就绪 channel，退避任务不被新流量饿死；延迟堆独立互斥锁——Push 持 RLock 阻塞在满 channel 上，Pop 热路径拿写锁会自我死锁）；redis 实现有序集 `<stream>:delayed`（score=到期 UnixNano；ZREM 先行仲裁多进程，输家不动条目；不可解码成员丢弃防读循环；ZRANGEBYSCORE 失败上抛为 Pop 错误）。两实现非正延迟直落就绪队列；Close 均不排空未到期任务。
- `core/logstore.AddIfAbsent`：同一任务只开一行日志——重试期间停在 `pending`，终态才翻 success/failed。日志 wire 词汇（success/failed/pending/shadow）零变化，落实「不要把日志直接当状态」原则。
- `core/runtime`：`Deliver` 的 provider 查找/预算快照/限流等待抽为 `deliverPrologue`、终态映射抽为 `settle`，共享骨架上新增 `DeliverOnce`（恰一次尝试；Deferred 不结算，日志行 AddIfAbsent）。
- `core/worker`：`NewPool` 探测队列能力——有 `Scheduler` 走 `DeliverOnce` + `Schedule`（无 Nack：延迟重试不是失败）；Schedule 被拒 → Nack 兜底结算，任务不进 limbo。无能力回落 `Deliver`（同步退避，历史行为原样）。`herald.go` 的 `awaitingQueue` 装饰器转发 `Schedule`，库模式不因包装误判降级。
- 语义变化（护栏 5 点名的时序变化，如实记录）：重试等待不再占 worker；`DispatchSync` 的 wait 跨 defer 挂起、终态 Ack/Nack 才解析（阻塞总时长不变）；redis 队列的等待跨进程重启存活（memory 队列随进程消亡，与原语义同）。
- 测试：Defer 表（提示/封顶/不封顶/终态两路/跨调用预算耗尽）、memory Schedule（时钟 seam：到期先后/到期项优先于就绪/取消/关闭拒绝）、redis Schedule（持留/到期搬运/抢输仲裁/毒成员/读失败）、manager `DeliverOnce`（defer→恢复同日志行结算/终态表/无 retryer 即败）、pool 异步全链路（重入队/拒绝 Nack/同步回落/能力探测）、logstore `AddIfAbsent`、awaitingQueue 转发。

### Phase 7 MVP：Configuration（`channels` 独立配置块 + 三优先级叠加）

落点：`channels` 独立配置块（`channels: {ci: {providers: [...]}}`）落地，护栏 4 的「channel 显式 > channels 块 > routes 表」优先级显式化，routes/level_routes 键原样保留。

- `config.ChannelConfig`（新）：`channels` 顶层块——命名渠道 → providers 列表；`Validate()` 启动校验：渠道零 providers、引用未配置的 provider → **拒起**（与 Phase 1 受众表同纪律）；`ChannelRoutes()` 展平为 router 形状。
- `core/route.Router.ExpandChannel`（新）：渠道块查找（configured 顺序返回；空条目按未命中处理——回落裸 provider 目标与其「provider not found」熟悉失败，绝不停静默投空）。
- `core/service` 解析链（`expandRef`）：裸渠道名按优先级——**显式 provider 实例 > channels 块**；都不中维持字面 provider 目标（投递时失败，语义不变）。`routes` 表在其下：只路由未命名任何渠道的通知（既有行为）。`group:`/`user:` 引用与合成投递（概括/升级/恢复）全走同一解析链，零特判。
- 装配：`cmd/heraldd` 建 router 时注入展平块 + `channels loaded` 日志；`api/server.go` 挂 `SetChannelResolver(config.Router)`。
- 测试：router 展开（命中顺序/未知/空条目回 false）；config 校验（无 providers/未知 provider/合法）+ yaml 解析 + 展平；service 决策层（块展开有序、provider 压过块、双不中回落字面）+ 全链路（`channels: [ci]` → 每 provider 一任务）；heraldd 拒起两例 + FullFeaturedLifecycle 带 channels 块。
- 未做（护栏 5 留批/超纲）：channels 运行时 API（配置化即 MVP，与 audiences 一期口径一致）；group 成员渠道名不走块展开（成员语义是 provider 实例名，保持原样）。

### Phase 8 MVP：测试补全（七个测试块逐块核对 + Multi Provider 隔离）

按原计划的七个测试块逐一核对既有测试底稿，**六块已有**、一块真缺口：

| 测试块 | 要求 | 底稿（核对证据） |
|--------|------|-----------------|
| Notification | 创建 + 参数验证 | `TestNotificationService_Process`；handler_notify_test 的 invalid body / 422 / 未知渠道与模板 |
| Audience | group→recipients、user→recipient、recipient→endpoints | `TestProcessGroupReferences`（成员+recipient 钉选）；`TestExpandUser`/`TestExpandUserAudiencePrecedence`；`TestProcessUserReferences`/`TestMergeUserEndpoints`（端点按 provider 合并） |
| Routing | Notification+Audience+Channel → Delivery Tasks | `TestExpandRefs` 5 场景 + `TestExpandRefsChannelsBlock` + `TestProcessChannelBlockReference` + group:/user: 全链路 |
| **Multi Provider** | telegram success 不重复 / feishu failure 可 retry | **缺口 → 本批补** `TestPoolMultiProviderFanOutIsolation`（core/worker/pool_phase8_test.go） |
| Retry | Temporary→retry / Permanent→立即败 | Phase 5：`TestRetryerClassifiedErrors` + `TestDeliverClassifiedErrorTerminals` |
| State | queued→delivering→delivered；failed→retrying→delivered | Phase 3/4：`TestDeliverMarksStatusDelivered` + `TestDeliverMarksEveryAttemptDelivering` + pool_phase4 |
| Idempotency | 相同 idempotency_key → 不重复创建 Delivery | Phase 6：handler replay（队列零增长）+ `idempotency_test` 表驱动 |

补的测试走**全真链路**（service 路由 → planner 计划 → 队列 → worker → provider）：一次 fan-out 两个 provider 各自独立终态——telegram 首试即成且恰投一次（兄弟任务的失败不复制它），feishu 首试可重试失败、次试落地（delivered 而非 dead，RetryCount 1，LastError 交付时清空），两任务日志各自 success。计数断言钉住原计划的「不重复 / 可以 retry」语义。

### 差异总表回填（本批后）

| 目标（design-audience-model.md） | 落地后状态 |
|----------------------------------|-----------|
| Delivery 独立状态机 + attempts | ✅ 枚举化 + `RetryCount` 实测驱动；`next_retry_at` + 异步重新入队已落地（2026-10-05 后批：`Scheduler` 能力队列持留重试等待，worker 不再睡退避） |
| user:/Recipient/Endpoint | ✅ 配置化 MVP（audiences/recipients YAML + user: 引用展开合并） |
| 配置块 audiences/recipients | ✅ 已生效 + 启动校验（宽松解析"落地即生效"先例） |
| notify 领域字段 channel/audience/data/idempotency_key | ✅ 全部可选字段兼容上线（Phase 6：并集展开 + data/params 合并 + 内存幂等表） |
| Provider 错误分类接口 | ✅ core/errclass 六类词汇接入 httpclient 分类与 retry 判定（Phase 5：408/429/5xx/传输错误显式分类，其余 4xx 不分类不重试，wire 文本不变） |
| Logs 与 Delivery 状态分离 | 未动（成本最高项，「日志与状态分离」需独立 Phase 设计事件流） |
| Channel 独立配置块 | ✅ `channels: {name: {providers: [...]}}` 落地（Phase 7：启动校验拒起、provider 显式 > channels 块 > routes 表优先级、routes 键未删） |

### Phase 9 MVP：Documentation（全站对齐 + 差异总表收官）

按计划 Phase 9 的七个文档面逐一核对，代码改动为零、纯文档批：

- **Getting Started**（getting-started.md）：实现现状段改写为「Phase 0-9 全批落地」终态（channels 块、六类错误分类、MaxAttempts/LastError 并入），留批项点名并指到审计；第 3 步接入清单补 `user:` 受众、`channels` 命名渠道、`idempotency_key` 幂等重发三个指针。
- **Architecture**（architecture/overview.md）：边界现状段从「执行中 + 三项后续规划」改写为落地终态——Channel 独立配置块与错误分类接口**已落地**（此前是过时表述），仅异步重新入队与 Logs 事件流分离留批。
- **Configuration**：Phase 7 批已同步（领域模型与配置块表全 ✅ + channels 节 + 宽松解析警示改写），本批复核无新增。
- **Provider**（providers/overview.md）：新增「错误分类与重试」节——六类词汇表（触发/是否重试）、httpclient 自动分类口径（408/429/5xx/传输错误 vs 其余 4xx）、短信业务码差异指到各渠道页、dead/last_error 与日志 wire 词汇的分离说明。
- **Audience**（introduction.md）：「`user:` 级细分属于规划中」的过时表述改为已配置化落地。
- **Delivery**：状态机文档随 Getting Started/Architecture 终态更新；API 不暴露任务对象（queue 端点只报 size），无 API 文档改动；troubleshooting.md 修正一处**文档错误**——`/api/v1/logs` 的 `status` 字段实际是 `success/failed/pending/shadow`（wire 词汇），原文误写为 `delivered/failed/retrying`。
- **API**（api/rest.md）：Phase 6 批已同步（notify 领域字段 + 幂等节），本批复核无新增。

**差异总表收官**：全部行收敛为「✅ 已落地」或「留批 + 理由」，无未定性缺口。留批清单（均为显式设计决策，非遗漏）：
1. Logs 与 Delivery 状态分离——「日志与状态分离」的最高成本项（事件流或存储拆分），当前一份数据两种读法已满足需求。
2. audiences/recipients/channels 的运行时 API——一期口径为纯配置化（Phase 1/7 记录在案）。
3. group 成员渠道名不走 channels 块展开——成员语义即 provider 实例名（Phase 7 留批）。

> 2026-10-05 更新：原清单中的「RateLimited per-class 退避（Retry-After）」与「`next_retry_at` / 异步重新入队」均已落地（分别见 Phase 5 记录的后批补齐与本日新增的后批节），清单余 3 项。
>
> 2026-10-05 拍板：余 3 项确认维持现状——「Logs 与 Delivery 状态分离」与「audiences/recipients/channels 运行时 API」继续留批不做，「group 成员渠道块展开」维持既定不做；队列至此清零，后续新方向另行指派。
>
> 2026-10-05 显式决策记录：经用户拍板，**① Logs 与 Delivery 状态分离**（清单第 1 项）与 **② audiences/recipients/channels 运行时 API**（清单第 2 项）明确 **不实施**；第 3 项 **group 成员渠道块展开** 既定不做（Phase 7 记录在案）。三项均为显式设计决策，非遗漏；本审计文档与 todo.md 已同步收口，队列清零。

## 六、对照新模型的状态行（2026-10-07）

上表针对 Phase 0-9 管道改造，全部行仍成立。2026-10-05 起定位升级为「统一订阅与投递中枢」，受众层按[关系详设](./design-audience-relations) §15 的 11 个原子批次继续扩展，术语以[总纲](./design-audience-model#_2-术语契约-唯一口径) §2 为口径。新维度状态行：

| 新模型能力 | 状态 | 证据 |
| --- | --- | --- |
| 关系模型与受众注册表（subscription/enrollment、策略位、Subscribe/Enroll 分名） | ✅ 落地（批次 1） | `core/audience.Registry`，提交 `3fc1a82` |
| 联系面与绑定（ContactSurface 三态、一次性 token、换绑旧渠道确认、RSS 私密 token） | ✅ 落地（批次 2） | `core/audience.SurfaceRegistry`，提交 `19cf16c`；队列到期等待同批改注入时钟（`342caa1`） |
| 偏好中心（Frequency/Preference/DefaultPolicy、默认策略表、读写 API） | ✅ 落地（批次 3） | `core/audience` 偏好模型，提交 `4df837a` |
| 投递管道关系过滤（三方交集校验、渠道×关系矩阵） | ◑ 在途（批次 4） | 关系详设 §5、§14 |
| 投递审计补齐 / Digest / RSS 拉式 / 来源适配器 / 强度与投递模式 / 去重频控（三档）/ 集成者 API 与 Go SDK | ◑ 在途（批次 5-11） | 关系详设 §15 |
| 去重（既有部分） | ◑ 内容指纹去重与请求幂等键已上线 | 审计 §一#13、Phase 6 记录 |

本文至此封存为管道改造存档；新批次的执行记录记入 todo 与关系详设对应节，不再回写本表。
