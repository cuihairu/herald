# 事件与告警模型

接入方最常问的四件事：级别怎么定、同一条告警为什么没重发、能不能保证送达、顺序有没有保证。这一页把这些口径一次说清。

## 三套级别词汇

Herald 在三个层面各有一套词汇，映射关系固定：

| 层面 | 字段 | 取值 | 谁在用 |
| --- | --- | --- | --- |
| 通知层 | `level` | 惯用 `info` / `warning` / `error`（`critical` 可用，仪表盘按四级着色）；**无枚举校验，任意字符串透传** | `POST /notify`、规则表达式、`level_routes` 路由 |
| 紧急度 | `urgency` | `routine` / `normal` / `urgent` / `critical`（接受中文别名 例行/一般/紧急/关键） | app dispatch、品类默认值、投递策略 |
| 事件适配面 | `severity` | `critical` / `warning` / `info`，**只认这三个值** | `POST /apps/{app}/events` |

事件适配面的固定映射（其余值一律 422，不猜不钳制）：

| severity | urgency |
| --- | --- |
| `critical` | `critical` |
| `warning` | `urgent` |
| `info` | `normal` |

紧急度决定渠道匹配的天花板——渠道强度（L0–L5，从 RSS 拉式到电话强打断）不得超过紧急度上限：

| urgency | 强度上限 |
| --- | --- |
| routine | L1（邮件） |
| normal | L2（webhook/站内） |
| urgent | L4（短信） |
| critical | L5（电话） |

渠道默认强度：`rss`/`feed`→L0，`email` 系→L1，`webhook`→L2，`sms` 系→L4，`phone`/`call`→L5，**未列出的渠道（含 `log`）默认 L3**。超出上限的渠道在 dispatch 结果里标 `refused`，原因 `intensity_exceeded`。

## 去重与幂等

去重闸在「要不要发」这一层起作用，门闩顺序固定：**事件幂等 → 状态机 → 频控**。

| 层 | 触发条件 | 效果 |
| --- | --- | --- |
| 事件幂等 | 同一 `event_id` 再次出现（内容漂移也拦） | 抑制，原因 `idempotent` |
| 状态机 | 同一 `dedup_key` + 同一 `state` 重复上报 | 抑制，原因 `state-repeat`；状态翻转重发并开新一轮 |
| 频控 | 同一去重键在窗口内再次出现 | 抑制，原因 `once` 或 `throttled` |

频控三档按品类配置：`once`（进程生命周期内一次）、`throttle`（窗口内一次，窗口默认 5 分钟，可按品类覆盖）、`always`（全部放行）。默认档：`system`→once，`alerts`→throttle，其余 throttle。

去重键：显式 `dedup_key` 优先；没带时对内容（type/level/template/channels/recipients/params/title/body）做规范化哈希。**注意：level 变化等于换了键**——同一故障从 warning 升到 error 会重发，这是特性不是缺陷。

被抑制的通知返回 `suppressed: true`、HTTP 200，不出现在投递记录里；展开明细（`×N`）在审计流的 `delivery.deduped` 事件上。

幂等表、去重表都是**进程内存表**（容量 4096–1000，FIFO 逐出），重启即清空——语义是「重启后重投一次」，不是静默丢弃。需要跨重启幂等的调用方自己拿 `event_id` 做持久去重。

`POST /notify` 另有请求体级的 `idempotency_key`：同键重复请求返回首次受理结果（同样的 notification_id），不产生新投递。只有成功受理的结果入表；全部失败的请求可修正后同键重试。

## 投递保证

**至少一次，尽力而为；无顺序保证。**

- 每条通知展开成多个投递任务，各自独立重试。可重试错误（超时、HTTP 408/429/5xx、网络故障）按退避策略重投，默认 `max: 3`（总尝试 4 次）、指数退避 1s→1m；上游 429 的 `Retry-After` 优先于退避曲线，封顶 `max_delay`。
- 确定性错误（凭据无效、号码不存在、请求被拒等 4xx）**不重试**，直接记失败。
- 最终失败的任务在投递日志里落 `failed` 状态，重试期间是 `pending`，每任务一行。
- 多 worker 并发消费、重排队、扇出都会打乱顺序，herald 不承诺投递顺序，也不提供顺序补偿。需要顺序的调用方在内容里带序号自行处理。
- 队列实现：`memory`（默认，重启丢队列内任务）与 `redis`（Streams，跨重启保任务，但崩溃在延迟重投的窗口内可能丢一条 due 任务）。
- app 回调同样至少一次：事件走投递同一套队列与重试管道，非 2xx 重试，可能重复，接收方按 `event_id` 去重。

「受理」与「送达」是两件事：`code:0`/`accepted` 只代表进入投递管线；每个渠道的最终成败看投递日志（`/logs` 或 `/apps/{app}/deliveries`）与回调。

**RSS 渠道例外**：路由到 `rss` 的投递不产生投递任务，就地投影成 feed 条目，由订阅读者拉取——`accepted` 为空是预期行为。

## 事件的生命周期

规则路由的投递会打开事件账本（incident）条目：`open` → 确认（`POST /alerts/{id}/ack` 或飞书卡片按钮）→ 恢复（规则表达式的 match 不再成立时自动 resolve 并发恢复摘要）。确认是幂等的：首条生效，重复确认返回同一条记录，及时确认会取消待触发的升级链。

配置了 `escalation` 的规则，`ack_timeout` 内没人确认就向升级渠道重发（绕过规则评估与去重）。alert id 取通知 `params.alert_id`，缺省回退到内容去重键。
