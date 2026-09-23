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

# 通知规则（可选）
# 规则在去重之后、路由解析之前对每条通知求值；命中 active 规则且调用方
# 未显式指定 channels 时，改走规则的渠道路径；shadow 规则只记录不投递。
rules:
  - id: prod-fail-rate            # 1-64 字符：字母、数字与 . _ -
    match: 'params.fail_rate > 0.05 && params.env == "prod"'
    mode: shadow                  # shadow（默认，观察）| active | off
    route:                        # steps 按序求值，首个 match 命中生效
      - match: 'level == "error"'
        channels: [oncall]
      - channels: [devops]        # match 留空 = 恒命中的兜底 step

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

### P1 未生效字段

`for` / `group_by` / `inhibit` / `escalation` / `silence` 已在规则模型中建模但尚未实现——配置了这些字段会被校验拒绝（而不是静默忽略），避免给出假承诺；后续版本实装后放开。

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
