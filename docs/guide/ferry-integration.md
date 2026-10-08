# ferry 对接验证（阶段③）

「受众层改进」三阶段的最后一环：ferry（事件源，HERALD-1/2/3/4 已落地）与 herald（投递基建，批次 1–11 已落地）真实进程对接，验证 **收事件 → 分发 → 回执留痕** 闭环。本文是 2026-10-08 联调的验证记录与差异清单，也是后续部署的对接配置底稿。

## 1. 环境拓扑

单机三进程（本地联调，不涉生产）：

```text
ferry :18080 ──① POST /api/v1/apps/ferry/events──→ heraldd :18081
   ↑                                                      │
   │ ③ POST /api/internal/event-results                    ② webhook 通道投递
   │        （shim 翻译 §13.5 → ferry 词汇）                 ↓
shim :18090  ←──§13.5 回调（HMAC 验签）──────────────────────┘
   └── /hook：webhook 投递落点（证据留档）
```

- **ferry**：origin/main 干净检出（fb851d3），sqlite，`FERRY_HERALD_URL=http://127.0.0.1:18081/api/v1/apps/ferry`、`FERRY_HERALD_TOKEN=<trigger token>`、`FERRY_EVENT_FLUSH_SEC=2`；
- **heraldd**：memory 队列 + webhook provider（指向 shim `/hook`）+ `apps.ferry` 三 token 播种 + `sources.enabled: true`（联系面绑定面挂此开关下）；
- **shim**：一页 Go 进程，两块垫片——§13.5 回调接收（HMAC 验签 + 词汇翻译）与 webhook 投递落点。**它是 ferry 侧 HERALD-5 改造的化身**（见 §4 差异三）。

## 2. 事件词汇映射（告警通道设计 §3 → §13.3 dispatch）

ferry 的 `POST {FERRY_HERALD_URL}/events` 载荷映射到 herald 命名空间触发面：

| §3 字段 | §13.3 去处 | 说明 |
| --- | --- | --- |
| `id` | `event_id` | ferry outbox 主键，§11.1 幂等 + §13.5 回调**回带**——回执关联的钥匙 |
| `kind` | `category` | 必须已在命名空间注册（未注册 422，无静默兜底） |
| `severity` | `urgency` | critical→critical、warning→urgent、info→normal；其余 422 |
| `target` | `audiences[1]` | ref 形（`user:<id>`）映射为受众 id `user.<id>`——`:` 保留给 expandRef 引用，id 词汇不含它；非 ref 形（`admin`）原样 |
| `dedup_key` | `dedup_key` | 去重闸兜底 |
| `meta` | `params` | 任意 JSON 对象，模板参数 |
| `title`/`body` | 直投内容 | 不走模板 |
| `occurred_at` | ——（无对应面） | herald 审计/回调用自己的时间戳；信息性字段 |

部署侧约定：ferry 用户 `user:N` 在 herald 的受众 id 是 `user.N`，联系面绑定（`POST /api/v1/audiences/{id}/surfaces`）用同一 id。

## 3. 闭环实测证据（2026-10-08）

触发点：ferry 管理侧公告扇出（`POST /api/notifications/announcement`，HERALD-4 接线点，每启用用户一条 notice 事件）。连发两条验证可重复：

| 腿 | 证据 |
| --- | --- |
| ① ferry→herald | outbox `events` 表 id=1/2 均 `sent`（attempts=1）；`event_deliveries` 落 `1\|herald\|sent`、`2\|herald\|sent` |
| ② herald 分发 | `GET /api/v1/apps/ferry/deliveries`：`source: app:ferry`、`audience_id: user.1`、`category: notice`、`provider: webhook`、`status: success`；shim `/hook` 收到渲染后标题正文 |
| ③ 回执留痕 | shim 验签（HMAC 通过）→ 翻译 → ferry `HTTP 200`；`event_deliveries` 落 `1\|webhook\|sent`、`2\|webhook\|sent`——**回执按 outbox id 对上了原始事件** |

herald 的 §13.4 审计流对这两条为空是正确行为：审计答「关系变更 + 去重折叠」，本次两条公告无 dedup_key、指纹各不相同，无折叠也无关系变更。

## 4. 差异清单（如实记录，能修的已修）

1. **联系面绑定无机器入口（阻塞，已修）**——bot/公众号只覆盖平台背书的 follow，偏好中心只能在既有联系面上切换品类；ferry 这类「用户联系信息在自己库里」的整合方没有任何 API 能把绑定落进来。修法：新增 `POST/DELETE /api/v1/audiences/{id}/surfaces`（admin 代绑定，走 `SourceAdapter.Follow/Unfollow`，审计记入口 `admin`）。
2. **回调不回带整合方事件身份（阻塞，已修）**——§13.5 `delivery_result` 只有 herald 自己的 task_id，整合方无法把它对回自己的事件。修法：`DeliveryTask.EventID` 贯通投递（planner 从 notification 带入），回调载荷 `delivery.event_id` 回带；ferry outbox id 经 `/events` 适配面进去、经回调回家。
3. **ferry 回执端点与 §13.5 词汇不匹配（ferry 侧待改，shim 桥接）**——ferry 的 `POST /api/internal/event-results` 绑定 `{event_id: int64, channel, status: sent|failed, detail}`（HERALD-2 先于批次 11 落地，按猜测的形状设计）；herald 的 §13.5 事件是 `{event_id: "cb-…", app, kind, at, delivery: {…}}`，直接对接 `ShouldBindJSON` 即 400。**建议 ferry HERALD-5**：回执端点原生解析 §13.5——验 `X-Herald-Signature` HMAC、取 `delivery.event_id`（int 解析）、`delivery.channel`→channel、`success→sent`。本联调的 shim 即该改造的可运行样板（约 90 行）。
4. **target ref 与受众 id 词汇冲突（已修，映射约定）**——`user:<id>` 的 `:` 与 expandRef 引用冲突且过不了受众 id 校验；适配面映射为 `user.<id>`（§2 表），文档即约定。
5. **`occurred_at` 无对应面（记录，不阻塞）**——§13.3 无事件发生时字段；若后续审计要答「事件何时发生」，随审计明细批扩一列即可。

## 5. webhook 通道收敛结论

**可收敛**，条件齐备：

| | ferry `notify.Webhook`（P1-10，现状） | herald webhook 通道（对接后） |
| --- | --- | --- |
| 投递 | 全局单 URL，fire-and-forget，5s 超时 | 同等 POST 能力 + 全管道：重试预算、at-least-once |
| 故障 | 失败只记日志，**静默丢** | 回执留痕（ferry dash 红标）、死信可人工重投 |
| 路由 | 无（所有事件一股脑） | 按受众关系 × 强度 × 联系面三方交集，逐用户 |
| 鉴权 | 静态头 `X-Ferry-Webhook-Secret` | per-app HMAC-SHA256 回调签名（可验源可防篡改） |
| 内容 | `{event, text, fields}` 一行摘要 | 模板渲染/直投内容，多格式 |

收敛顺序建议：① 接收端把 `{event,text,fields}` 适配换成 herald webhook payload（`{id, provider, level, targets, timestamp, title, body}`，另有可选 `raw` 原始字段兜底键，一次性改造）；② ferry 配好 `FERRY_HERALD_URL/TOKEN` 后 `notify_webhook_url` 设置与事件外发**并行观察一个告警周期**；③ 摘除设置入口、退役 `internal/notify` 包。唯一仍带 webhook 直发语义的生产点是 recovery 的 BR-5（`recovery_failed` 升级人工），HERALD-3 起已与 outbox 双轨——并行期结束即可单轨。

## 6. 部署底稿（生产对接时的最小配置）

```yaml
# heraldd 侧：ferry 命名空间播种
apps:
  ferry:
    tokens:
      - secret: "$FERRY_HERALD_TOKEN"   # trigger scope，ferry 投递腿用
      - secret: "$FERRY_HERALD_CONFIG_TOKEN"   # config scope，品类/回调/绑定用
      - secret: "$FERRY_HERALD_QUERY_TOKEN"    # query scope，对账读口用
sources:
  enabled: true    # 联系面绑定面挂此开关下
```

```bash
# ferry 侧：环境变量
FERRY_HERALD_URL=https://herald.example.com/api/v1/apps/ferry
FERRY_HERALD_TOKEN=$FERRY_HERALD_TOKEN
```

初始化（config token 执行一次）：注册品类（九类告警 + 四类触达各一条）→ `PUT /api/v1/apps/ferry/callback`（指向 ferry 的回执端点 + 32 字节 secret）→ 每用户 `POST /api/v1/audiences/user.N/surfaces` 绑定联系面。暴露面：`/api/internal/event-results` 沿用 ferry 现口径（无鉴权 `/api` 组，部署侧网络边界隔离）；等 HERALD-5 原生验签后可获得应用层防伪。
