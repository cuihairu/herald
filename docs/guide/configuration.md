# 配置

## 配置文件

Herald 使用 YAML 配置文件（`config.yaml`）。

## 运行模式

Herald 使用统一二进制，通过子命令区分运行模式：

```bash
# 调度器模式（API + Queue + 本地 Worker）
heraldd serve --config config.yaml

# 远程 Worker 模式（从共享 Queue 消费任务）
heraldd worker --config worker.yaml
```

## 完整配置示例

### 调度器配置（scheduler.yaml）

```yaml
# 服务配置
server:
  addr: ":8080"
  timeout: 30s

# Provider 配置
providers:
  # 日志 Provider（默认启用）
  log:
    type: log
    enabled: true
    config:
      name: "log"

  # Telegram 机器人
  telegram:
    type: telegram
    enabled: true
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"
      # 可选：自建 Bot API 服务器或镜像（默认 https://api.telegram.org）
      # api_url: "https://my-bot-api.example.com"

  # 飞书
  feishu:
    type: feishu
    enabled: false
    config:
      webhook_url: "${FEISHU_WEBHOOK_URL}"

  # 企业微信
  wecom:
    type: wecom
    enabled: false
    config:
      webhook_url: "${WECOM_WEBHOOK_URL}"

  # 钉钉
  dingtalk:
    type: dingtalk
    enabled: false
    config:
      access_token: "${DINGTALK_ACCESS_TOKEN}"
      secret: "${DINGTALK_SECRET}"

  # Slack
  slack:
    type: slack
    enabled: false
    config:
      webhook_url: "${SLACK_WEBHOOK_URL}"

  # Discord
  discord:
    type: discord
    enabled: false
    config:
      webhook_url: "${DISCORD_WEBHOOK_URL}"
      # 或使用 bot API
      bot_token: "${DISCORD_BOT_TOKEN}"
      channel_id: "${DISCORD_CHANNEL_ID}"
      # 可选：自建/代理 Bot API（默认 https://discord.com/api/v10）
      # api_url: "https://my-discord-api.example.com"

  # 邮件
  email:
    type: email
    enabled: false
    config:
      host: "smtp.gmail.com"
      port: 587
      username: "${EMAIL_USERNAME}"
      password: "${EMAIL_PASSWORD}"
      from: "${EMAIL_FROM}"
      from_name: "Herald"

  # Webhook
  webhook:
    type: webhook
    enabled: false
    config:
      url: "${WEBHOOK_URL}"
      method: "POST"

  # 阿里云短信
  aliyunsms:
    type: aliyunsms
    enabled: false
    config:
      access_key_id: "${ALIYUN_ACCESS_KEY_ID}"
      access_key_secret: "${ALIYUN_ACCESS_KEY_SECRET}"
      sign_name: "${ALIYUN_SMS_SIGN_NAME}"
      region: "cn-hangzhou"

  # 腾讯云短信
  tencentsms:
    type: tencentsms
    enabled: false
    config:
      secret_id: "${TENCENT_SECRET_ID}"
      secret_key: "${TENCENT_SECRET_KEY}"
      app_id: "${TENCENT_SMS_APP_ID}"
      region: "ap-guangzhou"

  # 网易云信短信
  neteasesms:
    type: neteasesms
    enabled: false
    config:
      app_key: "${NETEASE_APP_KEY}"
      app_secret: "${NETEASE_APP_SECRET}"

  # 微信个人推送（Server酱）
  wechat:
    type: wechat
    enabled: false
    config:
      sendkey: "${WECHAT_SENDKEY}"

# 路由配置
routes:
  error:
    - log
  warning:
    - log
  info:
    - log

# 队列配置
queue:
  type: memory          # memory | redis
  size: 10000           # 队列容量
  workers: 0            # 本地 Worker 数量（0 = 自动，默认 CPU核心数*2+1）
  timeout: 5s
  # redis:              # type=redis 时需要配置
  #   addr: "localhost:6379"
  #   stream: "herald:tasks"
  #   group: "herald-workers"

# 重试配置
retry:
  max: 3
  backoff: exponential
  initial_delay: 1s
  max_delay: 1m

# 去重配置
dedup:
  enabled: true
  window: 5m

# 通知规则种子（可选）
# 规则在去重之后、路由解析之前对每条通知求值；命中 active 规则且调用方
# 未显式指定 channels 时，改走规则的渠道路径；shadow 规则只记录不投递。
rules:
  - id: prod-fail-rate            # 1-64 字符：字母、数字与 . _ -
    match: 'params.fail_rate > 0.05 && params.env == "prod"'
    mode: shadow                  # shadow（默认，观察）| active | off
    for: 3m                       # 可选：持续判定防抖，见「for 持续判定」
    group_by: [env, service]      # 可选：同组事件折叠计数 + 摘要，见「group_by 聚合通知（P2）」
    # group_interval: 10m         # 可选：组静默期，默认 5m，需配合 group_by
    # silence: {start: "22:00", end: "06:00"}   # 可选：每日静默窗，见「silence 静默窗（P2）」
    route:                        # steps 按序求值，首个 match 命中生效
      - match: 'level == "error"'
        channels: [oncall]
      - channels: [devops]        # match 留空 = 恒命中的兜底 step
  - id: leaf-service-unreachable
    match: 'type == "alert" && params.service == "api"'
    mode: active
    inhibit:                      # 可选：根因在场时抑制本规则，见「inhibit 抑制（P2）」
      source: prod-fail-rate      #   根因规则的 id
      equal: [env]                #   这些字段值相同才抑制
      # ttl: 30m                  #   在场条目存活期，默认 30m
    route:
      - channels: [devops]
  - id: prod-disk-down            # 带升级链的规则，见「escalation 升级链（P3）」
    match: 'type == "alert" && params.check == "disk"'
    mode: active
    escalation:                   # ack_timeout 内无确认 → 升级到 to 渠道
      ack_timeout: 5m             #   可选，默认 5m，上限 24h
      to: [phone-bridge]          #   升级渠道（如 webhook 桥接的电话网关）
    route:
      - channels: [oncall]

# 规则持久化文件（可选）
# 设置后通过 API 对规则的新增/修改/删除会落盘到该 JSON 文件，重启自动恢复；
# 不设置时规则仅保存在内存中（rules 种子仍然生效）。
# rules_store: ./data/rules.json

# 规则状态存储（可选，for 持续判定与 group_by 聚合使用）
# type: memory（默认，单实例，进程重启后进行中的窗口重新计时）
# type: redis（多实例/重启共享窗口与组状态，连接参数如下）
# rules_state:
#   type: redis
#   addr: "localhost:6379"
#   # password: "..."
#   # db: 0

# 升级链待决记录持久化（可选）
# 设置后 escalation 的待决升级落盘到该 JSON 文件，重启时恢复：
# 已过期的补发升级（停机期间的 ack 仍会被尊重），未到期的按剩余时间重建定时器。
# 不设置时待决升级仅保存在内存中（重启即丢，等于放弃升级）。
# escalation_store: ./data/escalations.json

# Provider 限流（可选，token bucket）
# 投递前按 provider 取令牌；规则引擎会把一条事件扇出到多个渠道，
# 限流是渠道风暴的最后安全阀。不配置 = 不限流。
# providers:
#   oncall:
#     type: feishu
#     config: { webhook_url: "..." }
#     rate_limit:
#       rate: 10      # 每秒补充令牌数
#       burst: 100    # 桶容量（允许的突发量）

# WebSocket 配置（远程 Worker 管理通道）
websocket:
  addr: ":8081"
  read_timeout: 60s
  write_timeout: 60s
  ping_interval: 20s
  # allowed_origins:       # WebSocket 允许的来源列表
  #   - "https://your-domain.com"
  #   - "*"                 # 允许所有来源（仅开发环境）
  # 未配置时默认允许 localhost/127.0.0.1

# 模板定义
templates:
  # 服务器告警模板
  server_alert:
    name: "服务器告警"
    title: "【告警】{{.Level}} - {{.Service}}"
    level: "error"
    fields:
      - label: "服务器"
        value: "{{.Server}}"
        type: "text"
      - label: "错误信息"
        value: "{{.Error}}"
        type: "text"
      - label: "时间"
        value: "{{.Timestamp}}"
        type: "text"

  # 部署通知模板
  deploy_notify:
    name: "部署通知"
    title: "部署完成: {{.Env}} 环境"
    level: "info"
    fields:
      - label: "环境"
        value: "{{.Env}}"
        type: "text"
      - label: "版本"
        value: "{{.Version}}"
        type: "text"
      - label: "分支"
        value: "{{.Branch}}"
        type: "text"
      - label: "耗时"
        value: "{{.Duration}}"
        type: "text"
```

### 远程 Worker 配置（worker.yaml）

远程 Worker 从共享 Queue 消费任务，需要使用 `redis` 类型的队列：

```yaml
# 队列配置（必须与调度器使用相同的 Queue 后端）
queue:
  type: redis
  workers: 0            # Worker 数量（0 = 自动，默认 CPU核心数*2+1）
  redis:
    addr: "localhost:6379"
    stream: "herald:tasks"
    group: "herald-workers"

# Worker 注册信息
websocket:
  addr: "localhost:8081"    # 调度器 WebSocket 地址（用于注册和心跳）

# Worker 本地 Provider（可选）
providers:
  wechatmp:
    type: builtin
    enabled: true
    config:
      app_id: "${WECHAT_APP_ID}"
      app_secret: "${WECHAT_APP_SECRET}"
```

## 队列配置说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | `memory` | 队列类型：`memory`（单机）或 `redis`（分布式） |
| `size` | int | `10000` | 队列容量 |
| `workers` | int | `CPU*2+1` | 本地 Worker 并发数 |
| `timeout` | duration | `5s` | 队列操作超时 |

### 部署模式对照

| 场景 | queue.type | 说明 |
|------|-----------|------|
| 单机开发/小规模 | `memory` | 所有 Worker 在同一进程内 |
| 分布式/高可用 | `redis` | 调度器和 Worker 可以独立部署 |

### Redis 队列要求

- **最低版本**：Redis 5.0+（需要 Streams 和 Consumer Groups 支持）
- 推荐使用 Redis 6.0+ 以获得更好的稳定性
- Redis Streams 的 `XADD`、`XREADGROUP`、`XACK` 命令是核心依赖

## 环境变量

支持使用 `${VAR_NAME}` 引用环境变量：

```yaml
providers:
  telegram:
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
```

## 规则配置说明

`rules` 是可选配置；不配置时通知行为与静态路由完全一致。

### 表达式环境

| 标识符 | 含义 |
|--------|------|
| `type` | 通知类型（`Notification.Type`） |
| `level` | 通知级别 |
| `title` / `body` | 内联内容（模板渲染前的直接内容；模板渲染产物不参与匹配） |
| `params` | 模板参数 map，如 `params.fail_rate` |

### 安全边界

- 表达式在加载/保存时**编译一次**，语法或类型错误直接拒绝（启动失败/保存失败），运行期只执行编译产物
- 长度 ≤ 2048 字符、AST ≤ 256 节点
- 内置函数与 `range` 操作符被禁用——表达式是对通知环境的纯比较/逻辑运算，无函数调用、无循环

### 影子模式

- 新规则默认 `mode: shadow`：每次通知照常求值，但只记录不投递
- 命中写入投递日志（状态 `shadow`，含 `rule_id`、`would_fire` 与本应走到的渠道），按规则采样（首条 + 每 100 条记录一条），命中总数计入日志统计
- 观察真实命中量符合预期后切 `mode: active` 生效

### for 持续判定（P2）

- 规则可带 `for: <时长>`（如 `for: 3m`，Go duration 语法，上限 24h）：同一逻辑告警的条件**持续成立达到时长**才真正触发，过滤单点闪断噪声
- 归组粒度：默认通知的完整内容（类型/级别/标题/正文/参数）相同才算同一组——每组独立计时；配置 `group_by` 后改按字段值归组（见下一节）；窗口从组内首条命中起算，后续每次命中判定「首条至今是否已满时长」；调用方显式指定 `channels` 的通知不受 for 拦截（显式意图优先）
- 触发一次后同组静默（不再重复投递），状态过期（`for` + 1 小时）或规则被修改/删除后重新计时；注意：事件驱动判定无法感知「无恢复事件」的闪断；配置 `group_by` 后，静默期内的后续命中会转入组折叠计数，摘要仍然有效
- active 规则的窗口未满时事件被拦下（不入队、不投递，类似去重命中），每次拦下按规则采样记入投递日志（状态 `pending`）；shadow 规则只记录「窗口已满」的命中，影子期看到的就是真实触发节奏
- 窗口状态默认存进程内存（重启后进行中的窗口重新计时）；`rules_state.type: redis` 让多实例共享状态（key 形如 `rule:{id}:state:{group}`，带 TTL 自动回收）
- 状态存储故障时规则按「求值失败」处理（跳过该规则），路由不受影响

### group_by 聚合通知（P2）

- 规则可带 `group_by: [env, service]`（字段取 `params` 里的值，1-8 个，用于归组的字段值相同即同一组；省略 group_by 退回完整内容哈希归组）：**同组的新事件不再逐条投递**，而是折叠计数进当前轮
- 首条事件照常投递并开新一轮；`group_interval`（默认 5m，上限 24h）静默期内的后续事件全部折叠（不入队、不投递，按规则采样记入投递日志，状态 `folded`）
- 静默期结束后**下一事件到来时**结算上一轮：该事件照常投递，同时附带一条合成摘要通知（标题含组标签与折叠总数，正文含起止时间），摘要与事件走相同的渠道路径；纯事件驱动、无后台定时器——若之后再无事件，最后一轮的摘要会在下次事件到来时补上
- 摘要是规则引擎自己产出的合成通知：不再过规则求值（避免宽匹配规则把摘要折回组里）也不过去重
- 调用方显式指定 `channels` 的通知不折叠（显式意图优先）；shadow 规则不模拟折叠（影子只观察条件命中）
- 与 `for` 组合时 for 判定在前：窗口未满事件被拦且不开组轮；触发一次后进入静默的事件转入组折叠计数，摘要计数包含触发首条
- 组状态与 for 窗口同库存放（key 形如 `rule:{id}:group:{group}`，带 TTL 自动回收），存储故障同样按「求值失败」fail-open

### inhibit 抑制（P2）

- 集群挂了会带出几十条「服务不可达」子告警——都是真的，但根因在场时没有行动价值。被抑制规则配置 `inhibit: {source: <根因规则id>, equal: [字段...]}`：**source 规则真实投递时**，把该通知的 equal 字段值组合记为「在场」（key 形如 `rule:{id}:inhibit:{值哈希}`，存 `rules_state`），本规则后续命中且 equal 字段值相同的事件被拦下（不入队，按规则采样记入投递日志，状态 `inhibited`）
- 在场条目按 `ttl`（默认 30m，上限 24h）过期，source 每次投递都续期——事件驱动无定时器：根因停止投递后条目自然过期、抑制解除；「根因恢复后补发摘要」依赖 P3 的恢复感知，本批次不包含
- 判定顺序在 for / group_by 之前：被抑制事件不开组轮、不计时——根因在场时子告警的持续判定与折叠没有意义
- 归组粒度：equal 字段值组合精确匹配（缺字段视为空值）；不同字段值组合互不影响
- source 允许前向引用（先建被抑制规则、后建根因规则，索引自动接上）；禁止自己抑制自己
- 显式 `channels` 的通知不受抑制（显式意图优先）；shadow 规则不检查抑制（影子只观察条件命中）
- 在场读取故障按「求值失败」fail-open（跳过该规则）；写入故障不阻断 source 自己的投递（宁可多投不漏投，错误计入观测）

### silence 静默窗（P2）

- 规则可带 `silence: {start: "22:00", end: "06:00"}`（HH:MM，进程本地时区）：窗口内该规则**整体冻结**——事件被拦（不入队，按规则采样记入投递日志，状态 `silenced`），for 窗口不计时、组轮不开
- `end` 独占（22:00-06:00 静默到 06:00 整）；`start < end` 为当日窗口，`start > end` 自动理解为跨午夜窗口；零长度窗口（start == end）会被校验拒绝
- 可选 `match` 表达式限定静默范围，如 `silence: {start: "22:00", end: "06:00", match: 'level != "critical"'}`——窗口内只静默非 critical 事件，critical 照常投递；match 编译失败在规则校验时即拒绝
- 日程驱动、无状态：不进 `rules_state`，判定只看当前时刻，不依赖进程重启前后的一致性
- 判定顺序在最前（先于 inhibit / for / group_by）：静默是「整段日程不吵」，与根因在场、持续判定都是不同层面的语义
- 显式 `channels` 的通知不受静默（显式意图优先）；shadow 规则不检查静默（影子只观察条件命中）

### 规则存储与 API（热加载）

- 默认规则只存在内存中（来自 `rules` 种子）；设置 `rules_store: <path>` 后，通过 API 的增删改会原子落盘到该 JSON 文件（tmp + rename），重启自动恢复
- 规则增删改即热加载：保存时编译，编译产物即时替换进活表，下一条通知就用新规则求值，无需重启
- 文件损坏（非法 JSON / 版本不识别）时启动失败并保留现场，不会静默丢弃规则

**API：**

```bash
# 列出规则
curl http://localhost:8080/api/v1/rules

# 创建规则（重复 id 返回 409；表达式编译失败返回 400）
curl -X POST http://localhost:8080/api/v1/rules \
  -H 'Content-Type: application/json' \
  -d '{"id":"p1","match":"level == \"error\"","mode":"active","route":[{"channels":["oncall"]}]}'

# 查看 / 更新 / 删除（URL 中的 id 优先于 body）
curl http://localhost:8080/api/v1/rules/p1
curl -X PUT http://localhost:8080/api/v1/rules/p1 -d '{...}'
curl -X DELETE http://localhost:8080/api/v1/rules/p1
```

**实现偏离说明**：设计文档原定持久化以 SQLite 起步；P1 实际采用 JSON 文件存储（实现同一 `Store` 接口）。理由：规则规模 <100 条、单写者进程、无查询需求，SQLite 的 15MB cgo 依赖不成比例；待 P2 ACK 状态需要真实查询能力时再引入 SQLite，届时接口不变、只换实现。

### ACK 告警确认（P3）

`POST /api/v1/alerts/{id}/ack` 记录告警确认（把「通知已送达」和「事故有人负责」区分开——前者是投递系统的职责，后者是告警系统的职责）：

```bash
# 确认告警（acked_by 可选，记录确认人）
curl -X POST http://localhost:8080/api/v1/alerts/incident-123/ack \
  -H 'Content-Type: application/json' \
  -d '{"acked_by": "alice"}'

# 查询确认状态
curl http://localhost:8080/api/v1/alerts/incident-123
```

- `{id}` 是调用方的告警身份——调用方在通知 params 里带的业务告警 id，同一告警的多次通知用同一 id 确认一次即可
- 确认是幂等的：同一 id 重复确认保留首次记录（确认时间是事实，不是计数器）

### escalation 升级链（P3）

规则可带 `escalation: {ack_timeout: 5m, to: [phone-bridge]}`：ack_timeout 内无人确认时把告警重投到更宽的 to 渠道（电话渠道以 webhook 桥接外部电话网关实现，herald 不内置运营商集成）。

- 告警身份取通知 `params.alert_id`（调用方的业务 id，与 ack API 同一身份空间）；未提供时退回内容指纹（同内容告警身份一致，但显式提供 alert_id 才好确认）
- 规则路由的投递出去后开一个 ack_timeout 窗口；同一告警再次投递会重置窗口（升级看的是「最新一条也没人看」）；窗口内通过 ack API 确认则升级取消
- 到点未确认 → 合成升级通知投递到 to 渠道（标题 `[Escalation]` + 原标题，正文含规则、告警 id 与超时时长）；升级通知不过规则求值也不过去重——升级的本意就是重复一条已发出的告警
- **超时判定是定时器语义**（P2 全部语义都是事件驱动，唯独升级必须在没有后续事件时也能动作）：待决升级持久化到 `escalation_store`，重启时恢复——已过期的补发升级（停机期间的 ack 仍被尊重），未到期的按剩余时间重建定时器；不配置 `escalation_store` 时重启即放弃待决升级
- 双重保险：ack 请求若与升级触发同时刻竞争，即使升级定时器已经触发，触发时的 ack 复查仍会让它放弃投递
- 显式 `channels` 的调用不挂升级链（规则的升级只作用于规则自己的路由）；被 for/组折叠/抑制/静默拦下的事件本来就没投递，自然不挂


### 投递限流与重试

**限流**（`providers.<name>.rate_limit`，可选）：投递前按 provider 取令牌（token bucket），等待发生在实际调用之前；等待被取消（关停）时该次投递记为失败。限流是规则引擎扇出场景的最后安全阀——一条错误规则不该演变成渠道风暴。

**重试**：投递失败是否重试由错误类型决定：

| 错误类型 | 可重试 | 说明 |
|----------|--------|------|
| 网络传输失败（连接拒绝、超时） | ✅ | HTTP 客户端统一标记 |
| HTTP 408 / 429 / 5xx | ✅ | 上游过载或临时故障 |
| HTTP 其它 4xx（400/401/403/404） | ❌ | 确定性客户端错误，重试无意义 |
| 渠道业务失败（如短信 body 错误码） | 视 provider | 各渠道自行标记 |

重试按 `retry` 配置退避（默认指数退避，最多 3 次）。注意：短信类渠道「HTTP 200 但业务码失败」的错误由 provider 判定 body 后返回，部分渠道标记为可重试（如腾讯云 API 错误），部分不重试（如发送状态中运营商拒收）。

## 动态配置

### 启用/禁用 Provider

**通过 API：**

```bash
# 启用
curl -X POST http://localhost:8080/api/v1/providers/telegram/enable

# 禁用
curl -X POST http://localhost:8080/api/v1/providers/telegram/disable
```

**通过 Dashboard：**

在 Dashboard 的 Providers 页面中，点击每个 Provider 卡片的启用/禁用按钮。

### 配置优先级

1. API 调用（运行时修改）
2. 配置文件（启动时加载）
3. 默认配置

## Provider 类型

| 类型 | 说明 | 配置示例 |
|------|------|---------|
| `log` | 日志输出 | `type: log` |
| `telegram` | Telegram Bot | `type: telegram` |
| `feishu` | 飞书机器人 | `type: feishu` |
| `wecom` | 企业微信机器人 | `type: wecom` |
| `dingtalk` | 钉钉机器人 | `type: dingtalk` |
| `slack` | Slack | `type: slack` |
| `discord` | Discord | `type: discord` |
| `email` | SMTP 邮件 | `type: email` |
| `webhook` | 通用 Webhook | `type: webhook` |
| `aliyunsms` | 阿里云短信 | `type: aliyunsms` |
| `tencentsms` | 腾讯云短信 | `type: tencentsms` |
| `neteasesms` | 网易云短信 | `type: neteasesms` |
| `wechat` | 微信个人推送 | `type: wechat` |

### 模板配置

详见 [模板系统](/guide/templates)。
