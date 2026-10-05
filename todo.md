# Herald 领域模型改造 — Phase 执行清单

跟踪 [design-audience-model.md](/design-audience-model) 计划的落地批次，事实底稿与差异详见 [design-audience-audit.md](/design-audience-audit)（含逐批执行记录）。执行规则（硬约束）：每批提交前全量门禁全绿（race + 覆盖 gate 100 + golangci-lint + dashboard 100% + docs build）；push 仅当前分支；单批小步提交。

> **计划已收官（2026-10-04）**：Phase 0-9 全批落地。留批项（Logs 事件流分离、受众/渠道运行时 API、group 成员渠道块展开）均为显式设计决策，理由见 audit 差异总表收官节。原留批两项 2026-10-05 已补齐落地并从清单移除：RateLimited 专项退避（Retry-After，`39979bb`）、`next_retry_at` / 异步重新入队（重试等待移入队列侧持留，worker 不再睡退避，`20e25d4`）。**2026-10-05 显式拍板**：余 3 项留批维持不做——① Logs 与 Delivery 状态分离、② audiences/recipients/channels 运行时 API、**不实施**；③ group 成员渠道块展开既定不做；队列清零，后续新方向另行指派。

## 已完成

- [x] **Phase 0 · 现状审计** — `d4a6f8c`：方案 vs 实现差异清单 13 项、词汇回填表、测试底稿盘点
- [x] **Phase 1 · 领域模型（user: 受众配置化 MVP）** — `3b56660`：`core/audience` 包（Endpoint/Recipient/Audience/Manager 启动校验）、config `audiences`/`recipients` 块、service `expandRef` 新增 `user:` 分支 + 按 provider 合并端点；未知 user 单通道显式失败
- [x] **Phase 3 · Delivery 状态机（枚举化 + 同步重试）** — `3b56660`：`DeliveryStatus` 枚举、`ErrMaxRetries` sentinel（errors.Is 判定 dead）、retrying 驱动 `RetryCount`、TaskLog wire 字符串不变
- [x] **Phase 6 · API 领域字段** — `8dddeb8`：notify 新增 `channel`/`audience`/`data`/`idempotency_key`（全部可选、兼容）；内存幂等表（cap 1000 FIFO、仅记录成功结果）；`audience` 逐项走既有展开链
- [x] **Phase 2 · Routing** — Routing ≠ Delivery 边界显式化：`expandRefs` 纯决策步骤（引用集合 → 目标，不触队列；单引用失败不阻断批量），`enqueue` 只消费决策结果执行入队；行为零变化 + 决策层直接单测（routing_test.go 5 场景）
- [x] **Phase 4 · Queue / Worker** — 五项核对（enqueue/dequeue/ack/worker/retry）全部无缺口；§14 字段回填：`DeliveryTask` 新增 `MaxAttempts`（投递预算快照）+ `LastError`（终态错误残留，delivered 清空），`Retryer.MaxAttempts()` 暴露预算；全真链路核对测试（pool_phase4_test.go：delivered/failed/dead + 预算 + last_error）；`next_retry_at` 留异步批次
- [x] **Phase 5 · Provider 错误分类** — `core/errclass` 六类词汇（temporary/permanent/rate_limited/authentication/invalid_request/timeout，分类骑错误链、文本不变）；httpclient 单点接入（408→timeout、429→rate_limited、≥500→temporary、传输错误按超时分类，其余 4xx 不分类不重试）+ retry `isRetryable` 分类优先（旧 RetryableError 标记兼容）；测试：分类表 / 状态码映射 / 重试判定 / 端到端终态（auth→failed、rate_limited→dead）；RateLimited per-class 退避留异步批次
- [x] **Phase 7 · Configuration** — `channels` 独立配置块（`channels: {ci: {providers: [...]}}`，启动校验拒起：零 providers/未知 provider）；优先级「channel 显式 > channels 块 > routes 表」在 expandRef 落地（显式 provider 实例压过块、`routes` 键未删）；router.ExpandChannel + service ChannelResolver；配置参考文档 channels 节；channels 运行时 API 按受众一期口径留批
- [x] **Phase 8 · 测试** — §31 逐块核对：Notification（创建/参数 422）、Audience（group→recipients、user→recipient→endpoints）、Routing（refs→tasks）、Retry（Temporary 重试/Permanent 立败）、State（queued→delivering→delivered、failed→retrying→delivered）、Idempotency（键回放不重复创建）**均有既有测试底稿**；唯一真实缺口 **Multi Provider 隔离**（同一 fan-out 中 telegram 恰投一次不被兄弟失败带偏、feishu 可重试落地）补 `pool_phase8_test.go` 全真链路测试（service 路由→计划→队列→worker→provider）
- [x] **Phase 9 · Documentation** — 七个文档面对齐（Getting Started/Architecture/Configuration/Provider/Audience/Delivery/API）：终态改写 + providers/overview 新增错误分类与重试节 + troubleshooting 修正 logs status 词汇错误（success/failed/pending/shadow）；差异总表收官——全部行收敛为「✅ 已落地」或「留批 + 理由」，无未定性缺口

## 进行中：受众层改进（统一订阅与投递中枢）

设计与实现详见 [design-audience-relations](design-audience-relations)。三阶段推进（2026-10-05 立项）：① 文档优先（本篇，写全再动码）；② 代码实现按文档落；③ ferry 对接验证（告警/通知真实触发走一遍）。全部扩在既有受众层上，不另立「订阅者」实体；关系类型显式（subscription 主动订阅 / enrollment 被动指派），接口 Subscribe/Enroll 分名。

- [ ] **1. 关系模型与受众注册表** — RelationType 枚举、关系存储（类型/来源/策略位字段）、Subscribe/Enroll 分名接口、查询/审计按类型分
- [ ] **2. 联系面与绑定 API** — ContactSurface（pending/active/invalid）、一次性 token 签发核销（15 分钟过期）、换绑旧渠道确认、RSS 私密 token 随绑签发
- [ ] **3. 偏好中心** — 品类×渠道×频率模型与校验、默认策略表（系统必收/营销默认低频）、偏好读写 API
- [ ] **4. 投递管道关系过滤** — expandRef 后置过滤（关系允许×联系面绑定交集）、渠道×关系矩阵校验、系统必达/营销退订策略位
- [ ] **5. 投递审计补齐** — 关系类型/入口来源快照进任务与 TaskLog、关系变更审计流水、去重折叠明细
- [ ] **6. Digest 聚合器** — 受众+品类+时间窗、定时翻转（redis 锁选主）、摘要模板与投递、实时豁免
- [ ] **7. RSS 拉式渠道** — 公共/私密 feed 生成、品类可见性校验（token 归属）、多地址容灾
- [ ] **8. 来源适配器** — bot /start /stop、公众号关注/取关事件、应用内勾选 API、取关回流全停、外部状态定期对账
- [ ] **9. 渠道强度与投递模式** — Intensity/Urgency 枚举、品类→紧急度映射、三方交集匹配策略件、三模式执行器（升级链/固定单渠道/多渠道并行）、升级链 ack 应答即停
- [ ] **10. 去重与频控** — event_id 幂等、内容折叠（计数+原始事件保留）、状态机去重、三档频控（once/throttle/always）与品类默认档
- [ ] **11. 集成者 API 与 Go SDK** — app 命名空间与 token 权限分级（config/trigger/query）、配置/触发/查询 API、webhook 回调（投递结果/退订回流）、SDK 与 docs/guide/integration.md

## 说明

- 本文件 2026-10-04 首次建立（此前计划只存在于设计/审计文档）；无历史待办条目需要清理，源文档中已被执行记录覆盖的过程性草稿（如早期审计稿的错误表述）均已在 [design-audience-audit.md](/design-audience-audit) 就地修正，不在此重复。
- 批次顺序与计划书一致（Phase 6 因独立成批提前勾销，实际执行序 6 → 2 → 4 → 5 → 7 → 8 → 9）；全部批次收官，本清单转为归档。