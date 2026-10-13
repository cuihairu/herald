# 留批解冻预案（3 项；① ② 待拍板，③ 已解冻落地）

> 状态：**① ② 待拍板，③ 已落地**。本文承接 [Audience 改造审计（存档）](./design-audience-audit)
> 2026-10-05 拍板维持不做的 3 项留批，为每项列出现状事实、可选路径与后果。审计第十一轮
> 确认：除 todo.md 这 3 项外无其他未完成项，本仓无 open issue/PR，文档面十轮复扫无新增
> 差异——审计已进入纯复扫，故按巡检指令转做本文。每项现状均带 file:line，选项为 A/B/C，
> 推荐基线均为 A（维持现状，即 2026-10-05 拍板）。

## 总览

| # | 条目 | 卡在哪 | 解锁需要 |
|---|------|--------|----------|
| ① | Logs 与 Delivery 状态分离 | 用户拍板（2026-10-05 维持不做：成本最高项） | 用户在 A/B/C 中选一个并给预算 |
| ② | audiences/recipients/channels 运行时 API | 用户拍板（2026-10-05 维持纯配置化口径） | 用户选定写口范围 + 持久化口径 |
| ③ | group 成员渠道块展开 | **已解冻（2026-10-13 巡检指派开工，选项 B）** | 无需——`2a857ff` 落地，文档同步 `9b741ef` |

---

## ① Logs 与 Delivery 状态分离

**现状事实（一份数据，两种读面）**

- `core/logstore/logstore.go:19-48` `TaskLog` 结构：任务执行在 API 上的唯一载体。`Status`
  为 wire 4 词汇 `success/failed/pending/shadow`（`:24`），shadow 行为规则引擎 dry-run
  观察（`:30-38`）。
- `core/types.go:65-77` `DeliveryStatus` 7 值状态机
  （accepted/queued/delivering/retrying/delivered/failed/dead），落在
  `DeliveryTask.Status`（`:94`）——**不经 API 暴露**：`/api/v1/logs` 系列
  （`api/server.go:252-254`）只读 `TaskLog`，queue 端点只报 size（Phase 9 记录，
  审计存档 :212）。
- 两者由同一写路径驱动：`core/runtime/manager.go:103-220` 七个任务转换点写
  `TaskLog`，`retry_count` 随任务内存态演进。

**拍板记录**：2026-10-05 维持不做——「日志与状态分离」的最高成本项（需引入事件流或
拆分存储），当前一份数据两种读法已满足需求（审计存档 :216）。

**选项与后果**

| 选项 | 做法 | 后果 |
|------|------|------|
| A 维持现状（推荐基线） | 不动 | wire 仅 4 词汇；`delivering/retrying` 中间态、`dead` 终态只在内存与日志派生可见；报表口径受 wire 词汇约束（troubleshooting.md 已按 wire 词汇修正） |
| B 拆分存储 + 独立查询面 | 新建 `DeliveryStore`（task ID 键、独立保留期）+ `/api/v1/deliveries`；日志保留历史事件语义 | 新存储、新 API、dashboard 改面；成本最高，即留批理由 |
| C 事件流 | 复用 `core/audit` 7 事件族（`delivery.deduped` 等）扩每尝试事件，日志作投影 | 写入路径翻倍，但拿到全状态历史；中-高成本 |

**解锁**：用户选定 A/B/C；选 B/C 时需给是否新增 API 面的口径。

## ② audiences/recipients/channels 运行时 API

**现状事实（实体纯配置化；运行时写口只覆盖关系面）**

- audiences/recipients/channels 三块均为配置、启动校验拒起（审计存档 :64、:99、Phase 7
  :164-173）；无运行时增删改，改实体 = 改 YAML + 重启。
- 运行时 API 只覆盖**关系与联系面**：`/api/v1/audiences/{id}/relations` 读
  （`api/server.go:311`，GET）、`/subscriptions` 订阅切换（`:318`，POST/DELETE
  `api/handler_sources.go:214`）、`/surfaces` 联系面绑定（`:322`，
  `api/handler_surfaces.go:32`，一次性 token）。实体的 audiences/recipients/channels
  本身无写口。

**拍板记录**：2026-10-05 一期口径为纯配置化（审计存档 :217）。

**选项与后果**

| 选项 | 做法 | 后果 |
|------|------|------|
| A 维持现状（推荐基线） | 不动 | 运维节奏 = 文件 + 重启；集成者程序化加受众/收件人/命名渠道须走配置变更流程 |
| B 最小写口（audiences） | `POST /api/v1/audiences` 写入运行时注册表，YAML 保持引导种子 | 注册表现为内存态（YAML 加载），运行期新增需持久化；校验须与配置 `Validate()` 对齐；中成本 |
| C 全量 CRUD + 持久化 + 回写 | audiences/recipients/channels 全量运行时增删改，配置回写 | 双源真相问题：文件 vs 运行时优先级与回写格式需额外裁决；最高成本 |

**解锁**：用户选定范围（仅 audiences / 全量三实体）+ 持久化口径（是否回写配置文件）。

## ③ group 成员渠道块展开

**现状事实（成员渠道名 = provider 实例名，字面解析）**

- `core/service/notification.go:675-683`：`group:` 引用展开时，成员直接成为
  `deliveryTarget{channel: m.Channel}`；channels 块查找（`:696-704`，provider 显式
  > channels 块 > routes 表）只作用于顶层引用，**不作用于成员渠道**。
- `core/groups/groups.go:32-39` `Member.Channel` 注释即「provider instance name（never
  another group reference）」。成员渠道名若与 channels 块键重名，按字面 provider
  投递、未命中即在投递时失败（熟悉失败模式）。

**拍板记录**：既定不做——成员语义即 provider 实例名（审计存档 :218，Phase 7 留批 :173）。

**选项与后果**

| 选项 | 做法 | 后果 |
|------|------|------|
| A 维持现状（推荐基线） | 不动 | group 无法复用 channels 块；块改名时成员不跟随，须改 YAML 成员列表 |
| B 成员渠道走同一解析链 | 复用 `:696-704` 渠道臂展开成员渠道 | 成员名与块键重名时**静默扇出**为多 provider，改变既有投递语义；存量配置需迁移说明；小成本（代码复用 + 测试） |
| C 显式前缀 opt-in | 新语法如 `channel: "channel:ci"` 才走块展开 | 无静默行为变化；新增语法要进文档与校验；中小成本 |

**解锁**：~~用户选定 A/B/C~~ → **已解冻**（2026-10-13 巡检指派开工，实施为 B）。

> 2026-10-13 解冻记录：巡检指派「最有价值项开工」，本项以最小自包含改动中选；
> 选项实施为 **B**（实施时自选，理由：既有行为不变的依据——显式 provider 实例在
> 解析链两端都压过块（`resolveChannel` 先查 provider），成员名与块键重名时扇出；
> 重名此前是「投递时按 provider 缺失失败」，扇出只把当前失败的引用变成可达，
> 不改变任何正常工作的配置）。落地见 todo 与 [configuration](/guide/configuration)。

---

> 拍板后：本文对应小节标记所选选项与执行 commit，todo.md 三行转 `[x]` 证据行；未选
> 项维持 `[ ]`。拍板前不动代码。
