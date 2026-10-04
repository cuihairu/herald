# Herald 领域模型改造 — Phase 执行清单

跟踪 [design-audience-model.md](/design-audience-model) 计划的落地批次，事实底稿与差异详见 [design-audience-audit.md](/design-audience-audit)（含逐批执行记录）。执行规则（硬约束）：每批提交前全量门禁全绿（race + 覆盖 gate 100 + golangci-lint + dashboard 100% + docs build）；push 仅当前分支；单批小步提交。

## 已完成

- [x] **Phase 0 · 现状审计** — `d4a6f8c`：方案 vs 实现差异清单 13 项、词汇回填表、测试底稿盘点
- [x] **Phase 1 · 领域模型（user: 受众配置化 MVP）** — `3b56660`：`core/audience` 包（Endpoint/Recipient/Audience/Manager 启动校验）、config `audiences`/`recipients` 块、service `expandRef` 新增 `user:` 分支 + 按 provider 合并端点；未知 user 单通道显式失败
- [x] **Phase 3 · Delivery 状态机（枚举化 + 同步重试）** — `3b56660`：`DeliveryStatus` 枚举、`ErrMaxRetries` sentinel（errors.Is 判定 dead）、retrying 驱动 `RetryCount`、TaskLog wire 字符串不变
- [x] **Phase 6 · API 领域字段** — `8dddeb8`：notify 新增 `channel`/`audience`/`data`/`idempotency_key`（全部可选、兼容）；内存幂等表（cap 1000 FIFO、仅记录成功结果）；`audience` 逐项走既有展开链

## 待办（按序执行）

- [ ] **Phase 2 · Routing** — Routing ≠ Delivery 边界显式化：把「引用集合 → 展开决策」抽为纯函数步骤（`expandRefs`），enqueue 只消费决策结果执行入队；行为零变化，补展开决策边界测试
- [ ] **Phase 4 · Queue / Worker** — 按计划核对 enqueue/dequeue/ack/worker/retry 五项现状（MemoryQueue 已具备），补齐缺口与测试
- [ ] **Phase 5 · Provider** — 错误分类接口（计划 §29）：Temporary/Permanent/RateLimited/Authentication/InvalidRequest/Timeout 分类接入 retrying/dead 判定
- [ ] **Phase 7 · Configuration** — `channels` 独立配置块（优先级：channel 显式 > channels 块 > routes 表）；配置参考文档补全
- [ ] **Phase 8 · 测试** — Integration / Provider / Routing / Retry / Idempotency Test 补全
- [ ] **Phase 9 · Documentation** — 全站文档与代码最终对齐、差异总表清零

## 说明

- 本文件 2026-10-04 首次建立（此前计划只存在于设计/审计文档）；无历史待办条目需要清理，源文档中已被执行记录覆盖的过程性草稿（如早期审计稿的错误表述）均已在 [design-audience-audit.md](/design-audience-audit) 就地修正，不在此重复。
- 批次顺序与计划书一致（Phase 6 因独立成批已提前完成勾销，剩余按 2 → 4 → 5 → 7 → 8 → 9）。