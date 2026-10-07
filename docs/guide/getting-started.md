# 快速开始

**第一条真实送达的通知**，两条路径可以走：

- **第 1 步（约 1 分钟，零凭据）**：用 `log` 通道在本地跑通「API 接单 → 队列 → 投递」全链路，亲眼看到消息被送达；
- **第 2 步（约 5 分钟）**：接入一个真实渠道（Telegram 或群机器人 Webhook），把通知发到你的手机/群里。

之后再到[场景与接入](/guide/use-cases)挑你的业务场景照抄配置。

## Herald 是什么

> Herald receives notifications from applications and reliably delivers them to one or more notification providers.

中文定位：

> **Herald 是轻量、Provider 无关的统一订阅与投递中枢**，为业务系统、运维、CI/CD、Agent 和自动化任务提供统一的通知路由、受众管理与多渠道可靠投递能力。

业务侧只描述「发生了什么、通知什么、通知谁」；谁在什么渠道、以什么频率收到什么品类，由受众自己的关系决定；消息最终通过什么渠道、由哪个 Worker、何时以及如何重试送达，全部由 Herald 完成（模型见 [受众领域模型总纲](/design-audience-model)）。

## 核心概念

| 概念 | 一句话 | 举例 |
|------|--------|------|
| **Notification** | 一次需要被 Herald 处理和投递的通知 | 一条 `type=server.alert, level=error` 的告警 |
| **Audience** | 一条通知所面向的受众集合（通知谁） | `group:ops` |
| **Recipient** | Audience 中的具体接收人 | `user:alice` |
| **Endpoint** | Recipient 的具体投递地址（静态配置） | `telegram:123456` |
| **ContactSurface** | 受众的联系面：运行时绑定、带状态的渠道凭据 | `alice` 绑定的 telegram（`active`） |
| **Relation** | 受众对品类的收发关系，投递的唯一合法依据 | `alice × alerts × telegram`（subscription） |
| **Channel** | 业务定义的通知逻辑通道（哪类通知） | `ci` / `ops` / `security` |
| **Template** | 通知内容如何生成 | `server_alert` 模板渲染标题与字段 |
| **Routing** | 决定通知展开成哪些投递 | 受众解析 + 通道路由 → Delivery Task |
| **Delivery Task** | 一次具体的渠道投递任务 | `T001 → Alice → Telegram` |
| **Queue / Worker** | 何时执行 / 谁执行 | memory 队列 + 本地 worker 池 |
| **Provider** | 实际调用外部服务的投递实现 | Telegram / Feishu / Email / Log |

## 架构

```text
Client
 ↓ (HTTP POST /api/v1/notify)
Notification API
 ↓
Routing（受众解析 / 通道路由 / 规则引擎）
 ↓
Delivery Task
 ↓
Queue
 ↓
Worker
 ↓
Provider
 ↓
Telegram / Feishu / Email / Log ...
```

Notification 与 Delivery 分离：一条 Notification 可能展开成多条 Delivery（多渠道），各 Delivery 独立状态、独立重试。`accepted` 只代表 Herald 受理，不代表已送达。

> 实现现状：Phase 0-9 管道改造已全批落地——`Notification / Template / Channel（routes + channels 块）/ Delivery（枚举状态机 + MaxAttempts/LastError + NextRetryAt）/ Queue / Worker / Provider` 全部就位；`user:` 级 Recipient 与多 Endpoint 以配置化形态落地（`audiences` / `recipients` 配置块，见 [配置参考](/guide/configuration) 的"领域模型与配置块"）；Provider 错误按六类词汇分类（可重试类带 `next_retry_at` 回队列重投、确定性类立即失败）；notify API 支持 `channel` / `audience` / `data` / `idempotency_key` 领域字段（见 [REST API](/api/rest#notify-receivers)）。逐批执行记录见 [现状审计](/design-audience-audit)；明确留批的项（Logs 与 Delivery 事件流分离、受众与渠道的运行时 API）在审计差异总表中各有留批理由。受众层正按[关系详设](/design-audience-relations)扩展为统一订阅与投递中枢：关系模型、联系面绑定、偏好中心、渠道×关系矩阵与投递过滤、投递审计补齐、Digest 时间窗聚合、RSS 拉式渠道、来源适配器已落地，集成者 API 在途（落地状态见[总纲 §13](/design-audience-model#_13-落地状态-诚实口径)）。

## 安装

### 源码构建（推荐，需 Go 1.26+）

```bash
git clone https://github.com/cuihairu/herald
cd herald
make build          # 产出 bin/heraldd
```

### Docker

```bash
git clone https://github.com/cuihairu/herald
cd herald
cp .env.example .env   # 按需填渠道凭据
make docker-up
```

## 第 1 步：60 秒跑通全链路（零凭据）

`log` Provider 不发网络请求，直接把通知打印到 stdout。不需要申请任何账号，就能验证整条投递链路。

**1. 写最小配置** `config.yaml`：

```yaml
server:
  addr: "127.0.0.1:8080"
  timeout: 30s

queue:
  type: memory       # 单机内嵌队列，无需 Redis
  workers: 2

providers:
  log:
    type: log
    enabled: true
    config:
      name: "log"
```

**2. 启动服务**：

```bash
./bin/heraldd serve --config config.yaml
```

启动日志大概长这样（JSON 结构化输出）：

```json
{"time":"...","level":"INFO","msg":"provider registered","name":"log","type":"log","enabled":true}
{"time":"...","level":"INFO","msg":"herald scheduler started","addr":"127.0.0.1:8080","workers":2}
{"time":"...","level":"INFO","msg":"worker pool started","local_workers":2}
```

**3. 发一条通知**（另开一个终端）：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "server.alert",
    "level": "error",
    "title": "Node Offline",
    "body": "node-17 is offline",
    "channels": ["log"]
  }'
```

API 立即返回受理结果：

```json
{"code":0,"message":"ok","data":{"accepted":["log"],"notification_id":"a2c000ea-77b2-45d8-ac50-52f83d8dd0ae","task_ids":["73e15ce1-6434-4f87-9520-9d5e990f15c9"]}}
```

服务端日志随后出现**送达行**，消息到了：

```
[error] Node Offline: node-17 is offline
```

这一分钟里发生的事：`notify` API 收单 → 按 `channels` 找到 Provider → 生成投递任务进队列 → worker 池取出 → 调用 Provider 的 `Deliver` → 打印送达行。换任何真实渠道，流程完全一样，只是最后一步变成了发 Telegram / 短信 / 邮件。

> ⚠️ 启动命令是 `heraldd serve --config ...`（serve 是子命令）。另外 WebSocket 管理端默认占 `:8081`，被占用时在配置里改 `websocket.addr`。

## 第 2 步：发到真实渠道

### 路径 A：Telegram（个人手机收到，约 5 分钟）

**申请凭据**（全程无需审批）：

1. Telegram 里找 [@BotFather](https://t.me/BotFather) → 发 `/newbot` → 按提示起名，得到 **Bot Token**（形如 `123456:ABC-DEF...`）
2. 给你的机器人随便发一条消息（私聊必须用户先发起，否则机器人不能主动发给你）
3. 拿 **chat_id**：浏览器打开 `https://api.telegram.org/bot<你的Token>/getUpdates`，在返回 JSON 里找 `chat.id`

**配置并启动**：

```yaml
providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
```

```bash
export TELEGRAM_BOT_TOKEN=123456:ABC-DEF...
export TELEGRAM_CHAT_ID=你的数字ID
./bin/heraldd serve --config config.yaml
```

**发送**：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{"type":"demo","level":"error","title":"Herald 第一条推送","body":"from curl","channels":["telegram"]}'
```

手机上的 Telegram 会立刻收到 `🔴 Herald 第一条推送`。详见 [Telegram Provider](/providers/telegram)。

### 路径 B：群机器人 Webhook（飞书/企业微信/钉钉，约 2 分钟）

三家的自定义机器人都不需要审批，群设置里添加后拿到 Webhook 地址即可。以飞书为例：

1. 飞书群 → 设置 → 群机器人 → 添加自定义机器人，复制 Webhook 地址
2. 配置：

```yaml
providers:
  feishu:
    type: feishu
    enabled: true
    config:
      webhook_url: "$FEISHU_WEBHOOK_URL"
```

3. 重启后把上面 curl 的 `"channels"` 换成 `["feishu"]`，群里即收到 `[错误] Herald 第一条推送`。

企业微信、钉钉同理：[企业微信](/providers/wecom) · [钉钉](/providers/dingtalk) · 全部渠道见 [Provider 手册](/providers/overview)。

## 第 3 步：接上你的业务

把 curl 换成你业务代码里的一次 HTTP 调用即可——Herald 是 HTTP First 的，任何语言、脚本、监控系统的 webhook 都能接：

- **按业务类型路由**：`type` + `routes`/`level_routes` 决定一条通知走哪些渠道，见[场景与接入](/guide/use-cases)
- **模板复用**：`templates` 一次定义、多渠道复用，见[模板系统](/guide/templates)
- **命名受众**：`group:ops` 一个引用展开整组值班人，见[通知群组](/design-notification-groups)；`user:alice` 精确到人（多端点合并投递），见[配置参考](/guide/configuration)
- **命名渠道**：`channels: {ci: {providers: [...]}}` 把一组 provider 定义成一个可引用的渠道，见[配置参考](/guide/configuration)
- **RSS 拉式出口**：`feeds.enabled: true` 后每品类一个公共 feed（`/feeds/<品类>.xml`），路由到 `rss` 渠道的投递就地投影成 feed 条目，订阅读者自己来拉，见[配置参考](/guide/configuration)
- **幂等重发**：网络重试时带上同一个 `idempotency_key`，Herald 只受理一次，见 [REST API](/api/rest#notify-receivers)

## 常用查询

```bash
curl http://127.0.0.1:8080/api/v1/status      # 服务状态
curl http://127.0.0.1:8080/api/v1/providers   # Provider 列表与状态
curl "http://127.0.0.1:8080/api/v1/logs?limit=10"  # 投递日志（送达/失败/重试）
```

发送接口的完整字段（`recipients`、`template`、`params` 等）见 [REST API](/api/rest)。出了问题先看[排错指南](/guide/troubleshooting)。

## 下一步

- [场景与接入](/guide/use-cases) - 告警推送/运营触达/验证码短信/多通道兜底的完整配置
- [Provider 手册](/providers/overview) - 每个渠道的申请凭据、配置、第一条消息
- [配置参考](/guide/configuration) - 全量配置项
