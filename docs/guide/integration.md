# 应用接入（集成者 API）

这一页回答「**我的应用怎么把 Herald 当投递中枢用**」。面向业务系统集成方：不进管理界面、不发匿名 `/notify`，用自己的命名空间完成**配品类 → 绑受众 → 触发 → 查状态**全流程，凭证、品类、模板、策略与别的应用互相隔离。

> 应用侧模型（订阅/指派、联系面、受众）见[关系详设](/design-audience-relations)。本页只讲接入动作。

## 1. 申请接入：配置 app 与 token

app 在 heraldd 的配置文件里播种（谁有配置权，谁决定谁能接入）：

```yaml
apps:
  demo-app:
    tokens:
      - secret: "$DEMO_CONFIG_TOKEN"
        scopes: [config]
      - secret: "$DEMO_TRIGGER_TOKEN"
        scopes: [trigger]
      - secret: "$DEMO_QUERY_TOKEN"
        scopes: [query]
```

权限分级：**config** 改品类/模板/策略/回调，**trigger** 触发投递，**query** 读投递状态与审计流。一个 app 可持多枚 token 各带权限集——监控触发器只拿 trigger，对账作业只拿 query。

配置写错（token scope 超出 config/trigger/query 词汇、app 名非法）heraldd **拒起**，不会静默忽略。

## 2. 配品类：注册品类与默认紧急度

品类是触发面的分类键，默认紧急度决定没带 `urgency` 的触发按哪档窗口匹配渠道：

```bash
curl -X POST http://herald:8080/api/v1/apps/demo-app/categories \
  -H "Authorization: Bearer $DEMO_CONFIG_TOKEN" \
  -d '{"name":"alerts","default_urgency":"urgent"}'
```

同名品类各 app 互相隔离——demo-app 注册的「alerts」与另一应用的「alerts」互不可见。同默认值重复注册幂等，改默认值是冲突（422）。

## 3. 绑受众：订阅与联系面

触发面只投给**有出路的受众**——三方交集（关系 × 紧急度强度区间 × 联系面绑定）哪个缺一都投不出去。绑受众有三个入口：

- **受众自助/偏好中心**：`POST /api/v1/audiences/{id}/subscriptions`（订阅）与对应的退订；
- **平台入口收敛**：bot `/start`、公众号关注等由来源适配器落到同一张关系表；
- **管理侧代绑定**：`POST /api/v1/audiences/{id}/surfaces`（`{channel, target, categories?}`，绑联系面并可顺带落默认订阅；`DELETE ?channel=` 解绑）——用户联系信息在整合方自己库里的走这里，审计记入口 `admin`。

应用侧查询某受众现有关系：`GET /api/v1/audiences/{id}/relations`。指派型（enrollment，被动指派、可含必达）走操作侧登记，触发时必须显式声明 `relation_type: "enrollment"`。受众 id 词汇不含 `:`（它保留给 `user:`/`group:` 引用前缀）——整合方的 ref 形目标（`user:5`）绑定时映射为受众 id `user.5`。

## 4. 触发：dispatch

```bash
curl -X POST http://herald:8080/api/v1/apps/demo-app/dispatch \
  -H "Authorization: Bearer $DEMO_TRIGGER_TOKEN" \
  -d '{
    "category": "alerts",
    "audiences": ["user:alice", "group:oncall"],
    "dedup_key": "node-17-down",
    "event_id": "evt-20261007-001",
    "template": "node_down",
    "params": {"node": "node-17"}
  }'
```

要点：

- `urgency` 缺省取品类默认，带了就按你的（ refused 而非钳制——拼错的紧急度是 422 不是静默降级）；
- `dedup_key`/`state`/`event_id` 进 §11 去重闸：同键同态折叠（受理成功、`suppressed: true`、不产生投递），状态翻转重发；
- 内容走 `template`+`params`（命名空间自己的模板表，看不见全局模板）或直接 `title`+`body`；
- 响应是**受理结果**：谁命中（dispatched）、谁被拒及原因（refused：`filtered`/`phone_disabled`/`intensity_exceeded`/`no_channels`）、投递计划摘要（plan，升级链全链展示，首段先行投递）。

app 域策略覆盖（渠道强度、投递模式、升级链参数、去重频控按品类）走 `PUT /api/v1/apps/{app}/policies/*` 四族端点。§5 渠道×关系矩阵是安全底线，**没有**应用级放宽覆盖。

事件语义在自己词汇里的整合方（告警源：kind/severity/target + 自有事件主键）可以不走 dispatch 而走**事件接入适配面** `POST /api/v1/apps/{app}/events`：kind→品类（须已注册）、severity→紧急度（critical/warning/info → critical/urgent/normal）、target→受众（ref 形 `user:5` 映射为 `user.5`）、自有主键进 `event_id`——它既进 §11 幂等，也会在 §13.5 投递结果回调里**回带**，回执按它对回原始事件。

## 5. 查状态：deliveries 与 audit

```bash
# 这个 app 的投递记录（自动按命名空间隔离，只看得到自己触发的）
# audience 过滤按落库的受众 id 精确匹配——ref 形（user:alice）先映射为 id（user.alice），见 §3
curl -H "Authorization: Bearer $DEMO_QUERY_TOKEN" \
  "http://herald:8080/api/v1/apps/demo-app/deliveries?audience=user.alice&status=failed"

# 这个 app 的审计流（去重折叠、关系变更）
curl -H "Authorization: Bearer $DEMO_QUERY_TOKEN" \
  "http://herald:8080/api/v1/apps/demo-app/audit?since=2026-10-07T00:00:00Z"
```

投递行带 `audience_id`/`category`/`source` 维度；审计流按时间序，`delivery.deduped` 事件答「为什么这条没投」。匿名 `/notify` 的流量不会出现在任何 app 的查询里。

## 6. 回调：投递结果与退订回流

配置回调面（config 权限），herald 把**投递结果**与**退订事件**推给你：

```bash
curl -X PUT http://herald:8080/api/v1/apps/demo-app/callback \
  -H "Authorization: Bearer $DEMO_CONFIG_TOKEN" \
  -d '{"url":"https://app.example.com/herald/callback","secret":"16字节以上的共享密钥"}'
```

回调语义：

- **至少一次**：事件经投递同一套队列与重试管道投出，失败重试，可能重复；
- **幂等**：每个事件带 `event_id`（重复投递同 ID），你的端点按它去重；
- **签名**：请求头 `X-Herald-Signature: sha256=<hex>` 是请求体精确字节的 HMAC-SHA256（密钥即你配置的 secret），`X-Herald-Event-ID` 重复事件 ID；
- 两类事件：`delivery_result`（任务落定状态：success/failed + provider/受众/品类）与 `unsubscribe`（退订回流：受众×品类×渠道×关系类型，来自你命名空间的退订）。

验签（接收端骨架）：

```go
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(rawBody) // 未解析的原始字节
want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
if r.Header.Get("X-Herald-Signature") != want {
    http.Error(w, "bad signature", http.StatusUnauthorized)
    return
}
// 2xx 即确认；非 2xx 会被重试
```

## 7. Go SDK

`apps-sdk/go`（对齐 worker-sdk 先例）把上面全部端点收进一个客户端——一枚 token 一个 Client，权限由 token 决定：

```go
import sdk "github.com/cuihairu/herald/apps-sdk/go"

c := sdk.New("http://herald:8080", "demo-app", os.Getenv("DEMO_CONFIG_TOKEN"))

_ = c.RegisterCategory(ctx, "alerts", "urgent")
_ = c.SetCallback(ctx, "https://app.example.com/herald/callback", secret)

out, err := trigger.Dispatch(ctx, sdk.DispatchRequest{ // trigger 是另一枚 trigger 权限的 Client
    Category:  "alerts",
    Audiences: []string{"user:alice", "group:oncall"},
    DedupKey:  "node-17-down",
    EventID:   "evt-20261007-001",
    Template:  "node_down",
    Params:    map[string]any{"node": "node-17"},
})
// out.Dispatched / out.Refused / out.Plan / out.Suppressed

page, _ := query.Deliveries(ctx, sdk.DeliveriesQuery{Status: "failed"}) // query 是第三枚
events, _ := query.Audit(ctx, since)
```

错误区分两类：`*sdk.Error`（herald 的拒绝，带 HTTP 状态与 message，重试无意义）与非 herald 应答（网络层故障、无法解析的响应，`sdk.IsTransport` 判别，是否重试自行决定）。

## 边界速查

| 想做什么 | 用什么 |
| --- | --- |
| 注册品类/模板/策略/回调 | config token + `POST|PUT /api/v1/apps/{app}/*` |
| 触发一次投递 | trigger token + `POST /api/v1/apps/{app}/dispatch` |
| 查投递/审计/受众关系 | query token + `GET .../deliveries`、`.../audit`、`/api/v1/audiences/{id}/relations` |
| 匿名单发（无命名空间） | 既有 `/api/v1/notify`，保持兼容 |
| 放宽渠道×关系矩阵 | 没有这个端点——§5 是安全底线 |
