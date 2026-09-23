# 通知规则引擎升级设计

> 状态：设计稿（未实施）。本文所有"现状"描述均对照 `main` 分支真实代码，引用处标注了文件与函数；实施顺序与边界见文末分阶段规划。

## 一、定位：从"事件驱动投递"到"可编程告警平台"

**现状**：herald 今天约等于 **ntfy + 多渠道矩阵**。调用方（CI、监控脚本、业务代码）带着一个明确意图 `POST /api/notify`（`api/handler_notify.go` 的 `HandleNotify`），herald 的职责是把这条 `Notification`（`core/types.go`）经过去重、路由、模板渲染、任务拆分（`core/service/notification.go` 的 `Process`），推入队列，最后由 worker 池投递到 14 类内置渠道（`providers/builtin/`：aliyunsms、dingtalk、discord、email、feishu、log、neteasesms、slack、telegram、tencentsms、webhook、wechat、wechatmp、wecom，支持同名多实例）。这条链路里**每一件事都是"事件来了做什么"**——herald 自己不做任何判断，判断全部在调用方。

**为什么这是问题**：告警场景的判断逻辑（"失败率超 5% 且在生产环境"、"持续 3 分钟才算真故障"）被迫前移到每一个调用方。十个团队接入就要把同一套判断写十遍，阈值改一次要发十次版。调用方通常是不懂告警纪律的 CI 脚本，结果就是告警风暴——而风暴的代价由渠道承担（飞书刷屏、短信烧钱）。

**升级目标**：做成"**可以在后台配规则的 Alertmanager，但渠道矩阵是自己的**"。Alertmanager 的路由树/抑制/静默模型经过十年生产验证，但它的规则写死在配置文件里，改一条路由要走一次发布；herald 已有的渠道广度（含国内 IM/短信）是 Alertmanager 不具备的。两者结合点就是：**把 Alertmanager 的判断模型搬进来，把判断的配置权从配置文件搬到数据库后台**。

**演进路径分四层**，每层独立产生价值，不做大爆炸式重写：

1. **静态配置路由**（现状）：yaml 里的 `routes`/`level_routes`（`core/route/router.go`），改配置要重启；
2. **动态规则**：规则存 DB，后台 CRUD + 热加载，匹配条件从"类型相等"升级为"表达式求值"；
3. **有状态判断**：`for` 持续判定、`group_by` 聚合、抑制（inhibit）——判断依赖历史事件，不再是纯函数；
4. **ACK 闭环**：告警发出去不是终点，确认（acknowledge）、超时升级、事故记录形成闭环。

## 二、现有模块在新架构中的位置

升级不是推倒重来。现有五个 core 模块恰好构成新架构的下半身，逐个说明它们留在哪里、为什么留在那里：

| 模块 | 现状（真实代码） | 在新架构中的角色 | 为什么是它 |
|---|---|---|---|
| `core/route` | 静态 map：`Route(type, level)` 先按类型后按级别查渠道列表，`SetRoute` 运行时可改但仅内存态 | 保留为**兜底路由**。规则引擎命中后产生的渠道列表优先，未命中规则的流量回落到现有静态路由 | 兜底路由保证"规则引擎全挂/全不命中"时系统行为与今天完全一致——这是渐进迁移的安全网 |
| `core/dedup` | 进程内 map + 时间窗（默认 5 分钟），key 为通知内容 sha256（`notification.go` 的 `dedupKey`） | 保留，位置**从 ingress 前移到规则求值后**；有状态规则上线后底层存储与规则状态共用 Redis 方案（见 §四.1） | 去重和持续判定都是"对同一逻辑实体的事件序列做决策"，拆在两层会各自为政；但 dedup 现有语义简单可靠，规则引擎不必重造它 |
| `core/limiter` | TokenBucket（`limiter.go`）实现完整（Allow/Wait/Reservation），`Manager` 按 provider 维度管理；**当前没有任何调用方接线，是孤儿模块** | 正式接线到投递侧：规则引擎会放大单条事件的投递量（一条命中 N 条规则），渠道限流从"建议"变成"必须" | 规则引擎最大的运营风险就是规则写错导致渠道风暴，limiter 是最后的安全阀；模块已写好只差接线，是升级中性价比最高的一步 |
| `core/retry` | `Retryer.Execute` + 指数/固定退避；注意现状：`ShouldRetry` 只认 `*RetryableError`，而代码库中没有任何 provider 构造该类型，**因此当前实际几乎不发生重试** | 规则引擎不改它，但 P1 顺带把"哪些错误可重试"的判定修正（provider 返回 429/5xx 时包一层 `NewRetryableError`） | 升级会提高单渠道事件密度，重试语义不修正会放大失败重试风暴；这是已知债务，趁接线一起还 |
| `core/logstore` | 内存环形日志（默认 1000 条），`runtime.Manager.Deliver` 中记录任务成败，`GET /api/logs` 可查 | 扩展为**影子模式（shadow mode）的数据落点**：规则求值结果（命中/未命中、本应触发）作为新日志类型写入，供后台预览 | 影子模式的核心诉求就是"记下来但不发出去"，logstore 已有记录-查询-统计骨架（`Stats`/`Filter`），加字段比新造存储省事 |

不直接参与但保持不变的：`core/queue`（memory/redis 双实现，Queue 接口 Push/Pop/Ack/Nack）、`core/template`（模板 + 渠道 Binding，规则引擎只决定"发不发/发到哪"，不碰渲染）、`core/runtime.Manager`（投递编排：限流未接、重试在此、日志在此——`manager.go` 的 `Deliver`）、`core/worker`（Pool 消费循环）。**规则引擎全部放在入队之前和后台面，队列与投递链路零改动**——为什么：投递链路是当前系统最稳定的部分，覆盖率与测试都围绕它建立，动它风险最大、收益为零。

## 三、规则模型

一条规则的完整形态（YAML 表示，实际存储为 DB 行）：

```yaml
id: prod-payment-failure
match: payload.fail_rate > 0.05 && env == "prod"   # 表达式求值
for: 3m                                            # 持续判定防抖
group_by: ["env", "service"]                       # 聚合维度
inhibit:                                           # 抑制
  - match: cluster_down == true
    equal: ["env", "cluster"]
route:                                             # 渠道选择（复用现有 14 类渠道）
  - channels: [feishu-oncall]
  - match: level == "critical"
    channels: [wecom-boss, aliyunsms-duty]
escalation:
  ack_timeout: 5m                                  # 无人确认升级
  to: [phone-bridge]
silence:                                           # 静默窗
  - window: "00:00-06:00"
    tz: "Asia/Shanghai"
    match: level != "critical"
```

以下逐字段写清**为什么要有它**，以及设计取舍：

### match —— 表达式匹配

**为什么**：静态路由只能问"通知的类型是什么"，规则要能问"通知的内容意味着什么"。`payload.fail_rate > 0.05 && env == "prod"` 把判断权交给写规则的人，herald 只负责安全地执行表达式（执行安全见 §四.2）。匹配的输入是通知的结构化视图（`Notification.Params` + `Level` + `Type` + 渠道无关元数据），不是渲染后的文本——对文本做正则匹配是脆弱的，模板一改规则全哑。

### for —— 持续判定防抖

**为什么**：单点事件大多是噪声。磁盘报警闪一下就消失、Pod 重启自己恢复了——如果每次都发，人会在第 10 次之后开始无视通知，这在告警工程里叫"狼来了效应"，比不发告警更糟（用户会连真故障一起屏蔽）。`for: 3m` 表示同一逻辑告警（按 group_by 归组）的条件**持续成立 3 分钟**才真正触发。代价是告警延迟 3 分钟，收益是噪声减少一个数量级——告警的价值密度比时效更重要，误报才是时效的最大敌人。

### group_by —— 聚合

**为什么**：一个集群挂掉会产生 200 个 Pod 的告警，没有聚合就是 200 条飞书消息。`group_by: ["env", "service"]` 把同组事件折叠成一条组通知（组内计数递增、首条详情+后续计数更新）。分组键的选择是运维语义：太粗（只按 env）会把无关故障搅在一起，太细（按 Pod 名）失去聚合意义——所以 group_by 必须可配，Alertmanager 十年经验证明没有万能默认值。

### inhibit —— 抑制

**为什么**：集群挂了，随之而来的是几十条"服务不可达"子告警——它们都是真的，但都已经没有行动价值，人只需要处理根因。抑制是"高级告警在场时压住低级告警"：`cluster_down == true` 的规则触发时，所有与之 `equal` 字段相等（同 env、同 cluster）的子规则进入抑制态。**为什么不用 match 过滤掉**：子告警仍然要记录、仍然要在根因恢复后补发摘要，抑制是"延迟展示"而非"丢弃"。这也是它必须做成独立字段而不是让用户改 match 的原因。

### route —— 渠道选择

**为什么**：不同级别的告警走不同渠道是告警纪律的核心——critical 打电话、warning 进值班群、info 只进日志。`route` 数组按序求值、首个 `match` 命中生效，channels 直接复用现有 provider 实例名（`runtime.Manager` 已支持多实例，如 `feishu-oncall`/`feishu-boss` 两个飞书群）。**为什么保留静态路由**：存量调用方已经显式指定 `channels` 或依赖 `level_routes`，规则是增量能力而非强制门槛——没有规则命中的通知行为与今天逐字节一致。

### escalation —— 超时升级

**为什么**：告警发出去没人看等于没发。`ack_timeout: 5m` 表示触发后 5 分钟内无人确认，则升级到下一渠道（`to: [phone-bridge]`）。它把"通知已送达"和"事故有人负责"区分开——前者是投递系统的职责，后者是告警系统的职责，herald 要做的是后者。升级链的实现依赖 ACK 状态（P3），所以此字段 P1/P2 只建模不生效，避免给出假承诺。

### silence —— 静默窗

**为什么**：凌晨三点的例行巡检报告不该叫醒任何人，但周五下午的核心服务告警必须立刻广播。静默窗（时间段 + 匹配条件的组合）让"什么时间什么告警可以不出声"成为显式配置而非人的自觉。与 inhibit 的区别：inhibit 是事件驱动（高级告警在场才压），silence 是日程驱动（到点就压）——两者不可互相替代，值班场景两个都要。

## 四、三个架构要点

### 4.1 求值前置到 ingress，状态外置 Redis

**求值位置：入队前，不是出队后。** 规则求值放在 `NotificationService.Process` 的 dedup 检查之后、路由解析之前。为什么：被规则拦下的事件**不消耗队列与 worker**——一次误报风暴如果放到 worker 侧过滤，队列会被灌满、合法事件被排队延迟，过滤的代价转嫁给了整个系统；而在入队前拦下，代价只是几毫秒的表达式求值。另一个理由是状态判定（for/inhibit）需要"拒绝"能力：求值结果为"抑制中/持续未满足"时事件根本不该变成任务。

**有状态规则的存储必须外置 Redis，不进进程内存。** `for` 要记"这个组第一次命中是什么时候"，inhibit 要记"哪些高级告警正在场"，escalation 要记"什么时候该升级"。这些状态如果像 `core/dedup` 一样放在进程 map 里，worker 扩容就丢状态：两个 worker 各自维护"首次命中时间"，同一组告警可能永远凑不满 3 分钟持续时间，或者各自触发导致重复告警。key 设计：

```
rule:{rule_id}:state:{group_hash}   →  {first_seen, last_seen, count, ...}
rule:{rule_id}:ack:{group_hash}     →  {acked_by, acked_at, escalate_at}
```

`group_hash` 是 group_by 字段值的规范化哈希。所有 key 带 TTL（对齐 `for` 时长 + 静默窗上限），保证规则删除/不再命中后状态自动回收——为什么强调 TTL：告警状态是典型的"写了就忘"数据，没有 TTL 的 Redis 迟早变成第二次事故现场。单实例部署时 Redis 状态与内存 dedup 并存（dedup 保持现状），Redis queue 未启用的部署选择 `memory` 规则状态实现（同一接口两个后端，与 `core/queue` 的做法一致）。

### 4.2 表达式引擎：expr-lang/expr 或 cel-go，绝不手写解析

**为什么不能手写**：表达式语言的第一杀伤力不是写不出来，而是安全边界画不出来。用户写的 `match` 会被执行百万次，一个手写解析器迟早漏掉一类注入：属性穿透（`env == "prod" || deleteAll()`）、资源耗尽（`repeat("x", 1e9) == "x"`）、深递归爆栈。现成的表达式引擎把这些问题做成了类型系统和执行预算。

两个候选：

- **[expr-lang/expr](https://github.com/expr-lang/expr)**：纯 Go、零依赖，`expr.Compile(code, expr.Env(Env{}))` 编译期做**类型检查**（`payload.fail_rate` 不存在直接报编译错误，而不是运行时静默为 false），求值时传入强类型环境结构体。Go 生态里 Kubernetes 相关工具链大量使用。
- **[cel-go](https://github.com/google/cel-go)**（Google CEL，Common Expression Language）：标准化语法定义 + 多语言实现，内置**成本模型（cost model）**可以在编译期估算 AST 最坏执行开销，超限拒绝编译；protobuf 类型系统亲和。

选型建议：**P1 用 expr-lang/expr**——API 面积小、类型环境即 Go struct（与现有代码零粘合成本），补一层求值超时（`context.WithTimeout` 包住 `vm.Run`）+ 长度上限（表达式字符数、AST 深度）即够用；cel-go 的成本模型更强但引入 protobuf 依赖，等规则复杂度真正需要编译期开销估算时再评估迁移。**共同的红线**：表达式在规则保存时编译一次（编译失败即拒绝保存），运行期只执行编译产物——为什么：把"语法错误"拦在配置时刻而不是凌晨三点的告警时刻，这也是影子模式能给出确定性预览的前提。

### 4.3 影子模式：所有新规则先 dry-run

**为什么**：规则是运维人员写的程序，程序第一次上线就全量生效等于把生产环境当测试环境。一条写错的规则（阈值写错、字段名拼错）最坏的结局是半夜炸醒全公司——短信渠道按条计费，10000 人误报就是一次真实的预算事故。影子模式把"规则生效"从一次性开关变成可观察的渐进过程：

1. 规则创建后默认 `mode: shadow`。每次通知求值时照常计算命中结果，但**只记录不投递**：写入 logstore 扩展字段（`rule_id`、`would_fire: true/false`、`matched_at`），日志状态标记 `shadow`；
2. 后台规则详情页展示影子期统计：近 24h/7d 命中次数、命中样本（脱敏后的通知摘要）、若激活将走到的渠道路径；
3. 运维看着真实命中量判断"这是不是我想要的"，确认后切 `mode: active`。API 层支持一键回滚到 shadow。

**为什么 dry-run 不做成开关之外的东西**：影子求值的成本与真实求值完全相同（同一段编译产物、同一个求值器），不存在"影子模式拖慢系统"——如果连求值都嫌贵，规则本身就该重新设计。唯一要小心的是影子日志的量：高 QPS 通知下全量记录会挤爆 logstore 的环形缓冲，影子日志按 `rule_id` 做采样记录（首条 + 每 N 条计数 +1），后台预览读的是计数而非明细。

## 五、对标

| 能力 | ntfy | Alertmanager | PagerDuty / Opsgenie | herald 现状 | herald 目标 |
|---|---|---|---|---|---|
| 多渠道投递 | 自建推送（App/Webhook） | webhook 转发（渠道靠生态） | 原生全（电话/短信/IM），SaaS 覆盖广 | **14 类内置渠道，国内 IM/短信原生** | 不变，复用 |
| 路由模型 | topic 订阅 | 路由树（receiver 子树匹配） | 静态 routing rules | type/level 两级静态 map | 表达式规则树 + 静态兜底 |
| 条件匹配 | 无（按 topic 分发） | 内置 matchers（标签相等/正则） | 简单字段条件 | 类型相等 | 沙箱表达式（§4.2） |
| for 持续判定 | — | ✅ | ✅ | — | P2 |
| group_by 聚合 | — | ✅ | ✅（事件去重规则） | — | P2 |
| inhibit 抑制 | — | ✅（inhibit_rules） | ✅（related alerts） | — | P2 |
| silence 静默 | — | ✅（API 建静默） | ✅（维护窗） | — | P2 |
| ACK + 超时升级 | — | —（只管发，ack 在 oncall 工具里） | ✅（核心能力） | — | P3 |
| 配置方式 | CLI/配置文件 | **配置文件写死，改路由要发布** | 后台 UI 但规则模型不可编程 | yaml 静态 | **DB CRUD + 热加载 + 影子模式** |
| 部署形态 | 自托管，免费 | 自托管，免费 | SaaS，按人/按事件计费，贵 | 自托管，免费 | 不变 |

三个参照物正好是三种取舍：ntfy 把判断完全留给客户端（所以轻）；Alertmanager 有完整判断模型但配置冻结在文件里（所以运维恨发布）；PagerDuty 把判断模型与响应闭环做成 SaaS（所以全但贵）。herald 的位置：**Alertmanager 的判断深度 + 后台化配置 + 自有渠道矩阵 + ACK 闭环**，全部自托管。

## 六、分阶段实施

### P1 —— 动态规则底座（求值是纯函数，无状态）

- 规则存储：DB（SQLite 起步，接口抽象后可换 PostgreSQL），后台 CRUD API（挂在现有 `api/` Handler 模式下）+ 热加载（watch 版本号/更新时间，参照 config 加载处做 diff 重载）；
- 表达式求值：expr-lang/expr，保存时编译 + 类型检查，执行带超时与长度上限（§4.2）；
- 求值位置：`NotificationService.Process` 入队前（§4.1），未命中规则回落现有静态路由；
- **影子模式**（§4.3）与 logstore 扩展；
- `core/limiter` 正式接线到投递侧，顺带修正 retry 的可重试判定（§二）；
- **交付判据**：存量通知行为零变化（规则表为空时与 main 分支等价）；影子模式下线第一条真实规则。

### P2 —— 有状态判断

- 规则状态外置 Redis（§4.1 的 key 设计），memory 后端兜底单实例；
- `for` 持续判定（首见时间 + 持续校验，事件驱动更新而非定时轮询——为什么：轮询引入扫描周期误差且空转，事件驱动在每次命中时判定"持续时长是否已到"，语义精确且零空转）；
- `group_by` 聚合通知（组状态机：open → 通知 → 计数更新 → resolved 摘要）；
- `inhibit` 抑制（高级告警在场集合 + equal 字段匹配）；
- `silence` 静默窗（时段 + 时区 + 值班表对接的排班占位）；
- **交付判据**：同一组事件在 worker 扩缩容前后状态连续（Redis 状态不被进程生命周期影响）。

### P3 —— ACK 闭环

- ACK 回调：飞书卡片按钮回调（复用 `providers/builtin/feishu` 的应用配置，新增回调 HTTP 端点）+ 通用 `POST /api/alerts/{id}/ack`；
- `escalation` 生效：ack_timeout 到期未确认 → 升级链投递（电话渠道以 webhook 桥接外部电话网关实现，herald 不内置电话运营商集成——为什么：电话通道的合规与运营商接入是独立业务，webhook 桥接保持渠道矩阵可插拔）；
- 事故记录：组告警从触发到 ack 到 resolve 的全时间线落库，logstore 从"投递日志"升级为"事故台账"的查询视图。

### 明确不做的

- 不做表达式自定义函数注册（保持求值器封闭，为什么：开放函数注册等于放弃沙箱边界）；
- 不在 P1/P2 动投递链路（queue/worker/provider 协议不变）；
- 不做多租户（现有单配置文件部署形态保持，规则表带 `created_by` 字段为将来留痕）。
