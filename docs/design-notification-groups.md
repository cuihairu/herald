# 通知群组设计：群组与成员模型、规则绑定与按组路由

> 状态：已实施。通知群组（notification group）是**命名的投递受众**：一组"渠道实例 + 可选
> 收件人"的集合，任何接受渠道名的地方都可以用 `group:名字` 引用它，投递时展开成成员。
> 它与规则引擎（[design-rule-engine.md](./design-rule-engine.md)）正交：规则决定"发不发、
> 走哪条改道路径"，群组决定"路径解析成哪些真实渠道与收件人"。

## 一、问题：受众在配置里没有名字

herald 今天描述受众的唯一粒度是**渠道实例名**（provider 实例，如 `feishu-oncall`）。
两种痛：

1. **重复**。"值班梯队"是 feishu-oncall + sms-duty 两个人（渠道），这个列表今天要抄进
   每条规则、每个调用方的 `channels`、每段 escalation 的 `to`。值班换人（换渠道实例配置）
   要改 N 处；
2. **表达不了人**。渠道实例是"一个飞书机器人"，不是"一队人"。同一短信渠道发给
   张三李四两个号码，今天要么建两个渠道实例，要么靠通知级 `recipients`——
   而规则路由的流量根本没有地方带 recipients。

群组给受众一个**稳定的名字**：`group:ops-oncall` 背后是成员列表，成员值班换人只改群组，
所有引用它的规则、调用方、升级链自动跟随。

## 二、群组与成员模型

```go
// core/groups
type Member struct {
    Channel    string   `json:"channel"`              // 渠道实例名（provider 名）
    Recipients []string `json:"recipients,omitempty"` // 该渠道的可选收件人钉死
}

type Group struct {
    ID          string   `json:"id"`                  // 群组名，如 ops-oncall
    Description string   `json:"description,omitempty"`
    Members     []Member `json:"members"`
}
```

设计取舍逐条：

**成员 = 渠道 + 可选收件人，不是"用户"**。备选是引入用户实体（用户绑定多种联系方式，
群组装用户）。放弃：用户-联系方式映射是身份系统，herald 是投递系统，多一层实体就多一套
CRUD/审计/权限，而投递只需要"这条消息发给谁"；`Recipients` 字段已经覆盖"同一个渠道实例
发给特定人"的全部场景（短信号码、飞书 chat_id、邮件地址）。等真的需要用户体系时，
用户系统生成群组成员即可，模型不用改。

**群组不嵌套**。成员只能是渠道实例，不能是另一个 `group:`。备选：嵌套群组（组里含组）。
放弃：需要环检测、展开深度无界、一个成员被多层覆盖时收件人语义不清；而嵌套想表达的
"梯队包含小组"用扁平成员列表已经够写。展开是**单层、无递归**的，可审计。

**成员渠道在一个群组内唯一**。同一渠道发多人，用 `Recipients` 列表表达
（`channel: sms-duty, recipients: [138..., 139...]`），不建两个成员。
重复渠道成员在保存时拒绝——两个同名成员各带一半收件人是配置事故的典型形态。

**校验**与规则一致：ID 沿用规则 id 的模式（`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`）；
成员数 ≥1 且 ≤64（群组是"一队人"不是"通讯录"）；渠道名非空、收件人非空项；
Recipients 不去解释内容（那是渠道的事）。

## 三、引用语法：`group:` 前缀

**任何接受渠道名的位置**都接受 `group:名字`：

- 规则 `route[].channels`（规则绑定群组的唯一方式）；
- 调用方显式 `channels`（`POST /api/v1/notify` 的 `"channels": ["group:ops-oncall"]`）；
- 静态路由表 `routes`/`level_routes` 的渠道列表；
- `escalation.to` 升级目标。

**备选：规则上独立的 `groups` 字段**（`route: [channels: [...], groups: [...]]`）。
放弃：与 channels 语义重叠（两组并集？谁先谁后？），且调用方显式渠道、静态路由、
escalation 也都想引用群组——每种位置加一个字段不如一个统一引用语法。
**备选：`@ops-oncall` 前缀**。更短，但 `@` 在通知语境里已被用户名语义占用
（飞书/Twitter 的 @提及、telegram 的 @username），读规则的人会误以为 @ 的是人。
`group:` 自带类别词，一眼可判。
**备选：群组做成虚拟 provider**（注册一个名为 ops-oncall 的合成渠道，投递时扇出）。
放弃：虚拟 provider 混进 provider 注册表就要实现整套 Provider 接口（Send/健康检查/启停），
而它没有独立的发送语义；且投递日志、限流、重试都会看到假渠道名，排障多一层翻译。
前缀引用停留在**路由层**概念，投递层看到的永远是真实渠道。

展开语义（`Member.Recipients` 与通知自带 `Recipients` 的优先级）：
成员带 Recipients 时**成员钉死收件人**（群组的受众就是群组说了算）；
成员没带时回落通知自带 `n.Recipients[channel]`；两者皆无则走渠道实例默认目标。

## 四、按群组路由：展开发生在投递编排的单点

展开点选在 `NotificationService.enqueue`（`core/service/notification.go`）——
即"渠道引用 → 投递任务"的换算处，对每个渠道引用：

```
ref = "feishu-oncall"          → 自身，无收件人覆盖
ref = "group:ops-oncall"       → 群组存在：每个成员一条 {渠道, 收件人?}
                                → 群组不存在：该引用记为失败渠道（unknown group）
```

**为什么在 enqueue 而不是引擎求值时展开**：

1. **单点覆盖全部路径**。显式渠道、规则改道、静态路由、escalation 升级、群组摘要、
   恢复通知最终都汇入 enqueue，一处展开全部生效；在引擎里展开只覆盖规则路径，
   显式渠道与升级链还得再各写一遍；
2. **Decision 保持"意图"而非"实现"**。改道决策里写 `group:ops-oncall`，
   受众解析是投递编排的职责——两个变更频率不同的东西（规则策略 vs 值班表）不在一个对象里；
3. **dedup 键稳定**。dedupKey 基于通知内容（含 `group:` 引用原文），不受群组成员
   增删影响——值班换人不该让同一封告警绕过去重窗口重复炸。

**失败语义**：未知群组引用**不中断其余渠道**，与"渠道实例不存在/被停用"同一处理
（`ProcessResult.Failed` 里逐条记账）。群组被删而规则还在引用，是配置漂移，
表现应该是"这个引用失败且可见"，不是整条消息 500。

**空展开**：群组存在但成员为空被校验挡在保存时；运行中成员列表只增不减到零
（Delete 是整组删除），所以展开结果恒非空。

## 五、管理与持久化

与规则引擎同构，降低第二套心智模型：

| 方法 | 路径 | 语义 |
|---|---|---|
| GET | `/api/v1/groups` | 列表 |
| POST | `/api/v1/groups` | 新建（校验失败 400） |
| GET | `/api/v1/groups/{id}` | 单条 |
| PUT | `/api/v1/groups/{id}` | 整体替换（换值班 = 改成员） |
| DELETE | `/api/v1/groups/{id}` | 删除（引用它的规则不删，投递时按 unknown group 失败） |

- **持久化**：`Store` 接口 + `MemoryStore` + JSON `FileStore`（临时文件 + rename 原子写），
  配置 `groups_store: /var/lib/herald/groups.json`；
- **热路径读快照**：`Manager` 持内存活跃表（Put/Delete 原地刷新），投递热路径
  （`Resolver.Members`）只读内存快照，不走磁盘；
- **配置种子**：`groups:` 列表启动灌入（与 store 并存，语义同 `rules:`）；
- **无影子模式**：群组不是判断，是数据；数据错了改回来就是，不需要观察期。

## 六、明确的边界

- 群组**不参与**规则匹配（Env 里没有群组概念）——匹配基于消息内容，路由目的地才用群组；
- 群组**不带层级/角色**（无 owner、无审批流）——那 是权限系统的事；
- 嵌套群组、按时间轮换的值班表（schedule）不做；值班表由外部排班系统生成群组成员
  （PUT 群组）接入，herald 不内建排班引擎。
