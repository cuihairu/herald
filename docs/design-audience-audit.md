# Audience 领域模型：Phase 0 现状审计

> 状态：Phase 0 审计结论（只读，未改代码）。承接 [design-audience-model.md](./design-audience-model) 的
> 改造计划：本文按计划第 33 节 Phase 0 的输出要求（当前实现 → 目标模型 → 差异 → 改动文件 → 兼容性风险）
> 记录事实底稿。所有结论带 file:line，供 Phase 1 起的 Code Agent 以此为起点，不要重新发散。

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

| 目标（design-audience-model.md） | 现状 | 差距 |
|----------------------------------|------|------|
| Notification ≠ Delivery Task | 已分离（core.Notification / core.DeliveryTask） | ✅ 语义成立；TaskLog 把状态挂到 task 上 ✓ |
| Delivery 独立状态机（accepted/queued/delivering/delivered/failed/retrying/dead）+ attempts/max_attempts/last_error/next_retry_at | Status 字符串 4 值（pending/success/failed/shadow）；RetryCount 已存在；重试同步不排队 | ❌ 状态机枚举化 + 状态挂 Delivery（或等价物）→ 另需 next_retry_at 才能支撑「Nack 重新入队」式异步重试 |
| Retry 属 Delivery、错误统一分类（Temporary/Permanent/RateLimited/Auth/InvalidRequest/Timeout） | 错误为裸 error；retryer 对所有错误一视同仁 | ❌ Provider 错误分类接口缺失 |
| Audience = group + user 两级 | 只有 group: 形态 | ❌ user:/Recipient/Endpoint 需新增（MVP：本地配置模型） |
| Channel 独立配置块（channels: {ci: {providers: [...]}}） | routes/level_routes map 承担静态映射 | ◑ 能力等价但模型不同；可选择性演进 |
| Logs 与 Delivery 状态分离（日志=历史事件流） | 日志即状态（一份数据） | ❌ 按计划 §25 演进的成本最高点（需引入事件流或拆分存储） |
| Idempotency（idempotency_key） | dedup 按内容 key 窗口去重（非显式幂等键） | ◑ 语义不同：dedup 防重复投递，幂等键防重复创建 |
| Provider Capability | 已有 format 选择能力（selectFormat/getCapability） | ✅ 部分满足计划 §26 |
| 配置块：audiences/recipients/endpoints | 不存在（宽松解析静默忽略） | ❌ 新增 |

## 三、改动文件清单（Phase 1+ 参考，勿视为已承诺）

| 文件 | 改动方向 |
|------|---------|
| core/types.go | Delivery 状态枚举常量、TaskLog 字段扩展（attempts/next_retry_at）、Audience 引用类型（group:/user: 前缀统一） |
| core/logstore/logstore.go | 状态值枚举化（保持 JSON 序列化兼容）；如做 §25 则拆事件流存储 |
| core/runtime/manager.go | 状态写入点对齐新枚举；错误分类转换（Temporary/Permanent/...） |
| core/retry/retryer.go | 按错误分类决定重试/放弃（RateLimited 退避、Permanent 直接 dead）；可选：异步重新入队 |
| core/service/notification.go、planner.go | expandRef 增加 user: 解析；Channel 解析层（显式 channels → Channel 块 → routes 兼容） |
| core/groups/ → core/audience/（新建） | 承载 recipients/endpoints 一级（group 保留为 audience 聚合） |
| config/config.go | channels/audiences/recipients 配置块（宽松解析现存语义：未落地前静默忽略） |
| api/handler_notify.go | **本次不强制改**（§21 的 channel/audience/idempotency_key 字段全部可选新增，向后兼容） |
| api/server.go、handler_groups.go | 如新增 audience/recipient 端点（第一期可只做配置化，不做 API） |

## 四、兼容性风险与护栏

1. **notify 请求/响应保持兼容**：新增字段全部可选；`channels` 显式语义、`code/message/data` 响应壳、`notification_id/task_ids/accepted/failed` 不变。Breaking Change 需按计划 §34② 走（说明+迁移+文档+测试）。
2. **TaskLog Status 字符串**：从 4 值向枚举扩展时**只增不删不改名**（`pending/success/failed/shadow` 全保留，新增状态为补充），保证既有 dashboard 与 API 消费者不破。
3. **groups API 与 group: 引用**保持现状（`group:` 不改语义；`user:` 为全新前缀，各走各的解析路径）。
4. **routes/level_routes** 保留为静态路由的等价物，引入 channels 块时以「channel 显式 > channels 块 > routes 表」的优先级叠加，不删除既有键。
5. **重试语义**：当前同步重试对调用方可观测（一次 Deliver 阻塞完整个策略）；改为异步重新入队会改变端到端耗时与日志时序——需在 Phase 3 单独设计，不能顺手改。
6. **宽松 yaml 解析**是把双刃剑：新配置键落地前的静默忽略已在文档（configuration.md「领域模型与配置块」）注明，避免用户误以为已生效。
7. 每 Phase 完成必须过全量门禁（Go race + covermerge gate 100 + golangci-lint + dashboard 测试/构建 + docs 构建），与仓库既有纪律一致。
## 五、执行记录（2026-10-04：Phase 1 + Phase 3 + Phase 6 落地）

前两批改动已交付并过全量门禁（race 全绿 / covermerge gate 100 GREEN / golangci-lint 0 issues / dashboard 100% / docs build 过）；第三批（Phase 6）同样过关。按上表差异逐项回填：

### Phase 1 MVP：`user:` 级受众（配置化）

- `core/audience/`（新包）：`Endpoint{Type,Target}` / `Recipient{Endpoints}` / `Audience{Recipients}`；`Manager.ExpandUser` 解析——**audiences 表优先**，未命中回落 recipients 表；`NewManager` 启动校验（非法 id、audience 引用未知接收人、接收人零端点、端点 type/target 空或超长 → 报错拒起）：`user:` 引用绝不静默落到空。
- `config` 新增 `audiences` / `recipients` 顶层块（宽松解析由此落地生效；见 [configuration.md「领域模型与配置块」](/guide/configuration#领域模型与配置块)）。
- `core/service/notification.go`：`expandRef` 新增 `user:` 分支（`UserResolver` 接口 + `SetUserResolver`，与 GroupResolver 同构）；`mergeUserEndpoints` **按 provider 合并端点**——同一 provider 的所有目标捆绑进一次投递，provider 顺序排序保证任务/日志跨运行稳定。未知 user / 无 resolver → 该通道显式失败，其余照发（与 group: 行为一致）。
- `cmd/heraldd/main.go` 装配 `audience.NewManager`（非法表退出码 1）；`api.Config.Users` 透传。
- 按计划 §7 轻量原则**未做**：channels 独立配置块、audiences/recipients 运行时 API（idempotency 请求字段已由第三批 Phase 6 补齐，见下）。

### Phase 3 MVP：Delivery 状态机（枚举化 + 同步重试）

- `core.DeliveryStatus` 枚举（accepted/queued/delivering/retrying/delivered/failed/dead）+ `DeliveryTask.Status` 字段；planner 创建任务即置 `queued`。
- `core/retry/retryer.go`：新增 `ErrMaxRetries` sentinel 供 errors.Is 判定 **dead**——重试轮数耗尽的 retryable 失败返回 `max retries exceeded: <last>`（消息文本与历史一致，仅升级为可解包标记）；重试轮驱动 `RetryCount`（已完成的重试数）与 `Status=retrying`。
- `core/runtime/manager.go`：每次尝试前置 `delivering`；终态按 **delivered / failed（永久失败与限流中止）/ dead（可重试错误耗尽）** 写入：`errors.Is(deliverErr, retry.ErrMaxRetries)` 区分 dead 与 failed。
- **TaskLog wire 不变**：`pending/success/failed/shadow` 原样保留——dead/retrying 只体现在 DeliveryTask 状态，dashboard 与 logs API 无感知（护栏 2 就地兑现）。
- 错误分类按护栏 5 保持现状：`RetryableError` / `httpclient.RetryableError` 二元标记（HTTP 408/429/5xx、网络错误）→ 可重试；其余立即 failed（此前审计稿中"401 也会重试 3 次"的表述有误，已按上述事实修正理解）。异步重新入队与 `next_retry_at` 留后续 Phase（会改变端到端耗时与日志时序，需单独设计）。

### Phase 6 MVP：notify API 领域字段（channel / audience / data / idempotency_key）

按计划 §21/§24 与护栏 1（新增字段全部可选）落地，全部向后兼容：

- `api/handler_notify.go`：`NotifyRequest` 新增 `channel`（单数写法，与 `channels` 并集）、`audience`（受众引用数组，逐项走既有 `expandRef` 链——`group:` / `user:` / 裸渠道名）、`data`（模板数据，与 `params` 合并、`params` 重名优先）、`idempotency_key`；合并逻辑抽为 `notifyChannels` / `notifyParams` 两个纯函数，成功响应抽 `notifyData` 复用（首次投递与幂等 replay 同一构造）。
- `api/idempotency.go`（新）：`notifyIdempotency` 内存表——mutex + map + FIFO 队列，容量 1000（`defaultNotifyIdempotencyCap`，按需逐出最旧）；`get` / `put`，重复 put 保留首条记录。`NewHandler` 即挂载，零配置。
- 幂等语义：仅记录**成功受理**（`code 0`）的结果；命中 → 直接返回首次的 `notification_id` / `task_ids`，**不触碰投递管线**（队列零增长）；失败请求（422）不记录、同键可修正重试。进程生命周期内有效，重启即清空（MVP 无持久化，已在文档明示）。
- 覆盖：`handler_notify_test.go` 新增 7 个子测试（channels+channel 并集、channel 单用、audience 展开 `user:` 引用、data 并入 params、params 重名优先、data 独用、幂等 replay/键独立/失败不记录）+ `idempotency_test.go` 表驱动直测（miss/roundtrip/重复 put 保首条/FIFO 逐出/容量回退/自定义容量）。
- 设计取舍：幂等放 handler 层而非 service 层——键是请求字段、命中路径短路在 Process 之前（dedup 是内容指纹去重、幂等是请求键回放，两者语义不同、互不替代）；表内不存请求原文，只存 `service.ProcessResult`。

### 差异总表回填（本批后）

| 目标（design-audience-model.md） | 落地后状态 |
|----------------------------------|-----------|
| Delivery 独立状态机 + attempts | ✅ 枚举化 + `RetryCount` 实测驱动；`next_retry_at` 未做（同步重试无排队语义） |
| user:/Recipient/Endpoint | ✅ 配置化 MVP（audiences/recipients YAML + user: 引用展开合并） |
| 配置块 audiences/recipients | ✅ 已生效 + 启动校验（宽松解析"落地即生效"先例） |
| notify 领域字段 channel/audience/data/idempotency_key（§21/§24） | ✅ 全部可选字段兼容上线（Phase 6：并集展开 + data/params 合并 + 内存幂等表） |
| Provider 错误分类接口 | 保持二元 RetryableError（计划 §29 细分分类未做——当前语义已够支持 retrying/dead 区分） |
| Logs 与 Delivery 状态分离 | 未动（成本最高项，按 §25 需独立 Phase 设计事件流） |
| Channel 独立配置块 | 未做（计划内后续 Phase） |
