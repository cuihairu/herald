# Herald 领域模型改造 — Phase 执行清单

跟踪 [design-audience-model.md](/design-audience-model) 计划的落地批次，事实底稿与差异详见 [design-audience-audit.md](/design-audience-audit)（含逐批执行记录）。执行规则（硬约束）：每批提交前全量门禁全绿（race + 覆盖 gate 100 + golangci-lint + dashboard 100% + docs build）；push 仅当前分支；单批小步提交。

> **计划已收官（2026-10-04）**：Phase 0-9 全批落地。留批项（异步重新入队、Logs 事件流分离、受众/渠道运行时 API、RateLimited 专项退避、group 成员渠道块展开）均为显式设计决策，理由见 audit 差异总表收官节。

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

## 说明

- 本文件 2026-10-04 首次建立（此前计划只存在于设计/审计文档）；无历史待办条目需要清理，源文档中已被执行记录覆盖的过程性草稿（如早期审计稿的错误表述）均已在 [design-audience-audit.md](/design-audience-audit) 就地修正，不在此重复。
- 批次顺序与计划书一致（Phase 6 因独立成批提前勾销，实际执行序 6 → 2 → 4 → 5 → 7 → 8 → 9）；全部批次收官，本清单转为归档。