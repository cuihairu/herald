# 规则引擎设计：条件与动作模型（放行 / 抑制 / 改道）

> 状态：已实施。本文是规则引擎"决策层"的设计定稿：规则如何表达、匹配什么字段、按什么顺序求值、
> 命中后对消息做什么（放行/抑制/改道）、以及无规则命中时的默认策略。
> 面向告警平台的长线演进（for/group_by/inhibit/silence/escalation 的来龙去脉）见
> [rule-engine-design.md](./rule-engine-design.md)；本文只聚焦"一条消息进来，规则怎么判、怎么动"，
> 与该文档是分层关系：那份管"判断的历史维度"，这份管"判断的当机立断"。

## 一、问题：判断权没有归位

没有规则引擎时，herald 的每一件事都是"事件来了做什么"（`api/handler_notify.go` →
`core/service/notification.go` 的 `Process`）：

- 发不发：由 dedup 决定（内容重复就不发），其余全发；
- 发到哪：调用方显式 `channels` 优先，否则静态路由表（`core/route/router.go` 的
  `routes`/`level_routes`，type 相等 → level 相等）。

两个判断维度都不可运营：

1. **"发不发"没有规则**。"非工作时间的 info 不许出声"、"灰度环境的告警一律拦下"——
   这类策略今天只能写进每个调用方，或者改代码；
2. **"发到哪"是配置文件**。静态路由改一条要重启进程，且只能按 type/level 相等匹配，
   表达不了"标题包含『演练』的走演练群"。

规则引擎把这两个判断权收进 herald 后台：**条件（match 表达式）+ 动作（放行/抑制/改道）**，
规则存 store、后台 CRUD、热生效，未命中回落静态路由——存量行为零变化。

## 二、规则的表达方式

一条规则的存储模型（`core/rules/rule.go` 的 `Rule`）：

```yaml
id: prod-payment-failure
priority: 100
action: route            # allow | suppress | route（默认）
mode: active             # shadow | active | off（默认 shadow）
match: 'params.fail_rate > 0.05 && env == "prod"'
route:                   # action == route 时必填
  - match: 'level == "critical"'
    channels: [wecom-boss, aliyunsms-duty]
  - channels: [feishu-oncall]
for: 3m                  # 以下为进阶语义，见 rule-engine-design.md
group_by: [env, service]
# inhibit / silence / escalation 同理
```

### 2.1 条件模型：可匹配字段

匹配的输入是通知的**渠道无关结构化视图**（`rules.Env`）：

| 字段 | 来源 | 说明 |
|---|---|---|
| `type` | `Notification.Type` | 业务类型（deploy、alert…） |
| `level` | `Notification.Level` | info / warning / error / critical |
| `title` | 内联 `Content.Title` | 仅直发内容；模板渲染后的文本**不进**匹配 |
| `body` | 内联 `Content.Body` | 同上 |
| `params` | `Notification.Params` | 调用方自定义结构化字段，规则匹配的主力 |

**为什么匹配的是 Env 而不是渲染后的文本**：对文本做正则匹配是脆弱的——模板一改，规则全哑；
且模板渲染发生在规则求值之后（改道决定渠道，渠道才影响渲染），时序上也拿不到。
`params.fail_rate > 0.05` 让判断落在数据的结构上，模板怎么改版都不影响规则。

**为什么字段是封闭集合**：Env 是 Go struct，`expr.Compile` 带 `expr.Env(Env{})`
在**保存时**做类型检查——`params.fail_ate`（拼错）直接编译失败被拒，而不是运行时静默 false。
开放字段集合（比如把整个通知 JSON 摊开）换不来更多表达力，只换来更多拼错不被发现的机会。

### 2.2 条件模型：表达式语义与安全

表达式引擎用 [expr-lang/expr](https://github.com/expr-lang/expr)，**绝不手写解析**。
手写解析器的第一问题不是写不出来，而是安全边界画不出来（注入、资源耗尽、深递归）。
现有约束（`core/rules/engine.go`）：

- 保存时编译一次，运行期只执行编译产物——语法错误拦在配置时刻，不是凌晨三点的告警时刻；
- `expr.Env(Env{})` 类型检查 + `AsBool()` 强制布尔结果；
- `MaxNodes(256)` 限 AST 复杂度，`MaxExpressionLen(2048)` 限源文本长度；
- `DisableAllBuiltins()` 关掉全部内置函数——表达式是纯比较/逻辑，不能调用任何东西；
- AST 检查拒绝 `..` range 操作符（`1..1e9` 能在节点限额内分配巨量内存）；
- 求值带 500ms 超时兜底（`runProgram`）。

**备选：cel-go**。成本模型（编译期估算最坏执行开销）更强，但引入 protobuf 依赖、
API 面积大一个量级；herald 的表达式是短比较式，expr + 静态限制已封死资源面。放弃。
**备选：手写 DSL（字段-操作符-值的结构化 JSON）**。表达不了 `a && (b || c)` 之外的任意组合，
要么长出一门残缺的查询语言，要么限制表达力；且同样要自己处理类型与注入。放弃。

### 2.3 动作模型：放行 / 抑制 / 改道（本次新增的 `action` 字段）

动作回答"命中之后对这条消息做什么"。三个值，对应三种意图：

| action | 语义 | 消息去向 | route 字段 |
|---|---|---|---|
| `route`（默认） | **改道**：规则决定渠道 | 规则 `route` 步骤解析出的渠道 | 必填（≥1 步） |
| `suppress` | **抑制**：把消息拦下 | 不投递，观察记录留档 | 必须为空 |
| `allow` | **放行**：按没有规则处理 | 调用方显式渠道，否则静态路由 | 必须为空 |

三点设计取舍：

**为什么 action 默认 `route`**：向后兼容。action 字段落地前的一切存量规则都是"命中即改道"，
空 action 按 route 解读，存量 store 文件一行不用改。`route` 同时要求 route 步骤非空、
`allow`/`suppress` 要求 route 必须为空——动作与路由表是互斥意图，混写要么是笔误要么是误解，
保存时直接拒绝，不留"静默忽略"的口子。

**为什么需要显式 `suppress`**：抑制此前只有四条专用通道（silence/for/folded/inhibit），
它们都绑在告警语义上；而"灰度环境的任何告警都不要发"这种**无条件过滤**要造一条
零语义的规则才能表达——`action: suppress` 就是这个过滤器：一行规则，不占任何状态机。
它和 silence 的区别：suppress 是**身份驱动**（这个流量就是不该发），silence 是**日程驱动**
（这个时段不该发）；一个 suppress 规则不随时间变化，一个 silence 窗口到点自动解除。

**为什么需要显式 `allow`**：放行是优先级模型的另一半。规则按优先级首中即停（§3.1），
没有 allow 时" exemption"（豁免）表达不出来：`priority: 1000, match: env == "canary",
action: allow` 的意思是"灰度流量到此为止，后面所有改道/抑制规则都别碰它，按原路走"。
它等价于防火墙规则里的 ACCEPT-and-stop，是策略表的标配原语。

**抑制的边界——显式渠道不受抑制**。与既有语义一致（silence/for/inhibit 均如此）：
调用方显式指定 `channels` 的通知是明确的投递意图，规则的抑制动作只作用于
"本来要靠规则/静态路由决定去向"的流量。这条边界写在 §3.4 的默认策略同一段代码里，
不给两条抑制路径各搞一套例外。

## 三、匹配语义

### 3.1 求值顺序：显式优先级（本次新增的 `priority` 字段）

规则表按 **priority 降序**求值，同 priority 按**写入顺序**（稳定排序）。
`priority` 是 int32，默认 0，越大越先被求值；首条命中的 **active** 规则即"管辖规则"
（governing rule），求值停止——首中即停。

**备选：纯列表顺序（现状）**。List 顺序即优先级，问题是重排等于全量重写：
把一条规则提到最前，得 PUT 整张表。且顺序是隐式契约，API 使用者看不见"为什么它先于她"。
**备选：iptables 式插入位置（before/after 锚点）**。表达力最强，但 CRUD 复杂度暴涨
（锚点失效、环检测），herald 的规则量级（几十条）用不上。
**选定：数值优先级 + 稳定并列**。调优先级 = PUT 一个数字；并列时行为确定（写入序）；
首中即停让"高优先级豁免/拦截"天然可组合。规则量级到需要锚点插入时再谈。

实现上排序发生在引擎装载表的时刻（`Put`/`Reload`），对 `Evaluate` 透明——
求值循环只是遍历一个已排好的切片，热路径零额外开销。`List` 返回**求值序**（活跃表顺序），
所见即所判。

### 3.2 三种模式：active / shadow / off

- `active`：规则生效，动作按 §2.3 执行；
- `shadow`（默认）：只记录不动作——每次求值照常计算命中，命中写入观察记录
  （`Decision.Shadow` → `RuleObserver.RecordShadow` → 投递日志），投递行为不变；
- `off`：规则存储但跳过求值，等价"停用"。

新规则默认 shadow 是安全网：规则是运维写的程序，第一次上线就全量生效等于拿生产环境当测试；
影子期看真实命中量，再切 active。API 层 PUT 改 mode 即启停，一键回滚。

**备选：独立 enabled 布尔字段**。和 `mode: off` 语义重复，两个字段表达一个开关，
还会出现 `enabled: true, mode: off` 这种需要仲裁的组合。放弃——mode 三值就是
"生效/观察/停用"的完整状态机。

### 3.3 求值失败：坏规则不连累路由

某条规则的表达式求值失败（如引用了本条通知没有的 params 键）：跳过该规则继续求值，
失败记入 `Decision.EvalErrors` 并作为 error 返回给调用方观察。一条坏规则最坏只影响它自己，
不允许它让整张表瘫痪。状态后端（for/group/inhibit 状态）故障同样 fail-open 跳过。

### 3.4 默认策略：无规则命中时怎么办（本次新增）

`rules_default_policy` 配置引擎级默认策略，两个值：

- `allow`（默认）：未命中任何规则 → 回落**静态路由**（`core/route`），行为与没有规则引擎时逐字节一致；
- `deny`：未命中 → **消息被拦下**（`Decision{Action: suppress, Defaulted: true}`），观察记录 RuleID 为空串。

allow 是渐进迁移的安全网：规则表为空时系统行为与主干完全一致。
deny 是"白名单模式"：herald 从"默认都发、规则拦"翻转为"默认不发、规则放"——
告警收敛的最后一步是只让明确配置过的流量出去。两种策略同一行代码切换，
且 deny 下 shadow 命中仍然记录（观察不因收紧而失明）。

**备选：每规则 default 字段**。"默认策略"描述的是无规则命中的情况，是表级属性而非规则属性，
放规则上语义错位（哪条规则的 default 算数？先执行的？）。放弃。
**备选：deny 时返回错误给调用方**。把策略当故障暴露给调用方不合适——拦下是正常决策不是错误，
HTTP 仍 200，去向进观察记录。放弃。

### 3.5 决策的统一表达

`Evaluate` 返回 `Decision`，本次起带 `Action` 字段，把"管辖规则想干什么"一次说清：

```go
Decision{
    RuleID, Mode,           // 管辖规则（默认策略生效时 RuleID 为空）
    Action,                 // allow | suppress | route —— 本次新增
    Channels,               // action == route 时的目标渠道
    Defaulted,              // true = 默认策略生效（无规则命中）—— 本次新增
    Silenced/ForPending/Folded/Inhibited,  // 既有专用抑制语义（rule-engine-design.md）
    Shadow, EvalErrors, ... // 观察记录
}
```

调用方（`service.Process`）只认 Action 与专用抑制标志，不重复实现策略。

## 四、管理面：增删改查与启停

REST API（挂在既有 Handler 模式下，`api/handler_rules.go`）：

| 方法 | 路径 | 语义 |
|---|---|---|
| GET | `/api/v1/rules` | 列表（求值序） |
| POST | `/api/v1/rules` | 新建（表达式编译失败 → 400，保存即拒） |
| GET | `/api/v1/rules/{id}` | 单条 |
| PUT | `/api/v1/rules/{id}` | 整体替换（改 mode=启停、改 priority=调序） |
| DELETE | `/api/v1/rules/{id}` | 删除 |

所有写操作走 `Engine.Put/Delete`：校验 + 编译 + 落 store + **原地刷新活跃表**，热生效无需重启。
持久化后端可换（`Store` 接口：`MemoryStore` / JSON `FileStore`，原子写：临时文件 + rename）。
配置文件 `rules:` 种子列表在启动时灌入（与 store 并存，种子永远 Put 进 store）。

## 五、实施边界

- 求值位置：`Process` 内、路由之前、模板渲染之前（见 `core/service/notification.go` 注释）；
  被拦下的消息不进队列不耗 worker。
- 投递链路零改动：queue/worker/provider 协议不动，规则引擎全部活在入队之前与管理面。
- 通知群组（规则/调用方引用 `group:xxx` 渠道组）见
  [design-notification-groups.md](./design-notification-groups.md)，与本设计正交。
