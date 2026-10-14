# Herald 受众层改造需求清单

Phase 0-9 与受众层三阶段全部收官。事实底稿、差异与留批拍板见 [design-audience-audit](design-audience-audit)。测试数为该批收官提交时的 Go 测试函数数。

## 已完成

- [x] Phase 0 现状审计：方案 vs 实现差异清单与测试底稿盘点 — `d4a6f8c`（1079 例）
- [x] Phase 1 领域模型：user: 受众配置化 MVP — `3b56660`（1095 例）
- [x] Phase 2 Routing 边界：expandRefs 纯决策步骤与投递执行分离 — `efaae6b`（1097 例）
- [x] Phase 3 Delivery 状态机：枚举化 + 同步重试 — `3b56660`（1095 例）
- [x] Phase 4 Queue/Worker：MaxAttempts/LastError 回填与全链路核对 — `d65d3e9`（1100 例）
- [x] Phase 5 Provider 错误分类：errclass 六类词汇接入重试判定 — `35b9b37`（1107 例）
- [x] Phase 6 API 领域字段：notify 增 channel/audience/data/idempotency_key — `8dddeb8`（1096 例）
- [x] Phase 7 Configuration：channels 独立配置块与优先级链 — `edc81d3`（1111 例）
- [x] Phase 8 测试：Multi Provider 隔离全真链路补齐 — `ba10542`（1112 例）
- [x] Phase 9 Documentation：七文档对齐收官、差异总表清零 — `523746a`（1112 例）
- [x] 留批补齐：RateLimited per-class 退避（Retry-After）— `39979bb`（1126 例）
- [x] 留批补齐：next_retry_at 异步重新入队 — `20e25d4`（1160 例）
- [x] 插批 A 概念边界与分层审计：五层地图与八词裁决成文 — `de4bbac`（1220 例）
- [x] 受众层 1 关系模型与受众注册表：RelationType 枚举与 Subscribe/Enroll API — `3fc1a82`（1166 例）
- [x] 受众层 2 联系面与绑定 API：SurfaceRegistry 三态与一次性 token 绑定 — `19cf16c`（1179 例）
- [x] 受众层 3 偏好中心：Frequency/Preference/DefaultPolicy 与读写 API — `4df837a`（1184 例）
- [x] 受众层 4 投递管道关系过滤：ChannelClass 四族与 MatrixAllows 门 — `4b18b4a`（1190 例）
- [x] 受众层 5 投递审计：core/audit 流水与去重折叠明细 — `de05e94`（1195 例）
- [x] 受众层 6 Digest 聚合器：窗口聚合与 FlipLoop 定时器 — `3204e90`（1220 例）
- [x] 受众层 7 RSS 拉式渠道：feeds 存储、公开/私密 feed 与投递投影 — `a96e1a1`（1244 例）
- [x] 受众层 8 来源适配器：三端点入口与定期对账 Reconciler — `dc5092e`/`2f3d867`（1282 例）
- [x] 受众层 9 渠道强度与投递模式：Intensity/Urgency/Mode 与 DeliveryPolicy — `0c20459`（1314 例）
- [x] 受众层 10 去重与频控：Gate 去重、折叠与频控三档 — `3c6c395`/`ca6ee39`（1297 例）
- [x] 受众层 11 集成者 API 与 Go SDK：app 命名空间、触发/查询 API 与 SDK — `4899d8f`…`f062c20`（1355 例）

## 留批（显式设计决策，不实施；待拍板 D4F1）

- [x] Logs 与 Delivery 状态分离 — `0effddf`…`6534269`（1432 例）
- [x] audiences/recipients/channels 运行时 API — audiences 写口已落地（`8193172`）；recipients/channels 写口已代决(巡检 R1)：维持纯配置——运行时地址变更归联系面 API（ContactSurface 绑定/换绑/失效），静态 recipients/endpoints 只是种子，再加 CRUD 写口会造出同一概念第二条运行时路径（双源真相）；预案见 [design-audience-deferrals](design-audience-deferrals)
- [x] group 成员渠道块展开 — `2a857ff`（1398 例）
