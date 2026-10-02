# 场景与接入

这一页回答「**我该什么时候用 Herald、怎么接到我的业务上**」。四个典型场景各给：什么时候用 → 怎么配 → 怎么发 → 跑起来什么样。渠道凭据的申请方式见对应 [Provider 手册](/providers/overview)页，这里默认你已经有一个能用的通道（没有就先走[快速开始](/guide/getting-started)）。

## 场景一：系统告警推送

**什么时候用**：监控/巡检/定时任务发现异常，要把告警按严重程度送到不同渠道——error 进值班群弹窗，warning 留群消息，低级别只记日志。

**怎么接**：配置各渠道 Provider，再用 `level_routes` 按**级别**声明路由：

```yaml
providers:
  feishu:
    type: feishu
    enabled: true
    config:
      webhook_url: "$FEISHU_WEBHOOK_URL"
  log:
    type: log
    enabled: true
    config: {}

level_routes:
  error:   ["feishu"]
  warning: ["feishu"]
  info:    ["log"]
```

> 路由有两张表：`routes` 按**通知 type** 匹配（如 `server.alert`），`level_routes` 按**级别**匹配。都未命中且请求没带 `channels` 时，这条通知会被拒绝并报 `no route found`。

**怎么发**——监控系统只要发一个 HTTP 请求：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{"type":"cpu.high","level":"error","title":"CPU 使用率 95%","body":"order-01 cpu 95%, 持续 5min"}'
```

没写 `channels`，由 `level_routes` 决定去向；写显式 `channels` 则覆盖路由表。

**跑起来什么样**：飞书群里收到 `[错误] CPU 使用率 95%`；告警类通知可开飞书交互卡片带「确认告警」按钮（`interactive_cards: true`），超时未确认的升级策略见[规则引擎](/design-rule-engine)。

## 场景二：运营消息触达

**什么时候用**：产品/运营要给一批用户或一个值班组发公告、活动通知——内容一次定义，飞书、企微、邮件多点分发。

**怎么接**：用**模板**把内容与渠道解耦。模板里的 <code v-pre>{{.xxx}}</code> 就是发送时 `params` 传入的键：

```yaml
templates:
  ops_notice:
    name: "运营公告"
    title: "{{.Title}}"
    level: "info"
    fields:
      - label: "详情"
        value: "{{.Body}}"
```

**怎么发**——`channels` 写多个实例名，一次调用多点分发：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "ops.notice",
    "level": "info",
    "template": "ops_notice",
    "params": {
      "Title": "周五 18:00 系统维护",
      "Body": "预计 30 分钟，期间控制台不可用"
    },
    "channels": ["feishu", "wecom", "email"],
    "recipients": {
      "email": ["alice@example.com", "bob@example.com"]
    }
  }'
```

> 不需要模板时可省略 `template`/`params`，直接用顶层 `title`/`body` 传内容（快速开始用的就是这种直连方式）。

**跑起来什么样**：三个渠道同时收到同一条消息；API 返回 `accepted` 三个通道。`recipients` 只对按收件人寻址的通道生效（邮件收件人、短信号码、公众号 OpenID），群机器人类通道忽略它、发到配置固定死的群。

**受众固定成组**：把「值班组」的花名册存成命名受众，之后任何通道位写 `group:ops` 即可，人员变动改组不改业务——配置与 API 热更新见[通知群组](/design-notification-groups)。

## 场景三：验证码与事务短信

**什么时候用**：注册/登录验证码、订单状态、扣款提醒等必须到手机的高时效消息。

**怎么接**：短信通道需要「签名 + 模板」双重审核，先在云厂商后台完成申请（阿里云[申请方式](/providers/aliyunsms) / 腾讯云 / 网易云信）。以阿里云为例：

```yaml
providers:
  aliyunsms:
    type: aliyunsms          # 注意是 aliyunsms，不是 builtin
    enabled: true
    config:
      access_key_id: "$ALIYUN_ACCESS_KEY_ID"
      access_key_secret: "$ALIYUN_ACCESS_KEY_SECRET"
      sign_name: "你的签名"
      region: "cn-hangzhou"

templates:
  verify_code:
    name: "验证码"
    level: "info"
    template_code: "SMS_123456789"     # 阿里云模板 CODE
    fields:
      - label: "code"
        value: "{{.Code}}"
```

**怎么发**：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "user.verify",
    "level": "info",
    "template": "verify_code",
    "params": {"Code": "884275"},
    "channels": ["aliyunsms"],
    "recipients": {"aliyunsms": ["+8613800000000"]}
  }'
```

**跑起来什么样**：手机收到 `【你的签名】884275` 样式的短信；模板参数、号码格式（+86 前缀等）的差异见[短信 Provider 总览](/providers/sms)。

**验证码场景的两个关键点**：
- **不丢单**：408/429/5xx 等可重试错误自动按退避策略重试（`retry` 配置），运营商抖动不会静默吞掉验证码
- **防刷**：`rate_limit` 限制单 Provider 发送速率；配合 `dedup` 窗口挡住重复轰炸，见[配置参考](/guide/configuration)

## 场景四：多通道兜底与重试

**什么时候用**：告警不能只赌一条通道——IM 群挂了、机器人限流了，消息还得出去。

**怎么接**：`channels`（或路由表）写多个通道即**同时扇出**，各通道独立投递、互不阻塞：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "db.down",
    "level": "error",
    "title": "主库失联",
    "body": "mysql-primary 心跳超时",
    "channels": ["feishu", "telegram", "log"]
  }'
```

**失败时会发生什么**（语义与所有 Provider 一致）：

1. 单通道瞬时故障（超时/408/429/5xx）→ 该任务标记**可重试**，按 `retry` 的指数退避自动重投，最多 `max` 次
2. 确定性错误（token 无效、号码不存在等 4xx 类）→ 不重试，直接记失败
3. 最终结果全部落在[投递日志](/guide/troubleshooting)里，`accepted` / `failed` 一目了然：

```json
{"code":0,"message":"ok","data":{
  "notification_id":"...",
  "accepted":["feishu","log"],
  "failed":[{"channel":"telegram","error":"telegram API error: chat not found"}]
}}
```

**跑起来什么样**：任何一条通道成功，人就能收到；失败通道的报错在日志里可查可断言。同一事件去重窗口内的重复告警由 `dedup` 折叠，见[配置参考](/guide/configuration)。

> 想要「主通道失败**才**走备用」的级联语义（而非同时扇出），用规则引擎的路由决策（`rules` + 改道）表达，见[规则引擎决策层](/design-rule-engine)。

## 下一步

- [快速开始](/guide/getting-started) - 还没跑通？从这里开始
- [配置参考](/guide/configuration) - 上面出现的每个配置键的完整说明
- [Provider 手册](/providers/overview) - 每个渠道的凭据申请与第一条消息
