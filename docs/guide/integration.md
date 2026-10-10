# 接入指南

这一页回答「**我的项目怎么把 Herald 当投递中枢用**」：从拿到服务地址到发出第一条告警，照抄示例即可跑通，不用来问。逐端点的字段级参考在[应用接入 API](/api/apps)，通知级别、去重与投递保证的口径在[事件与告警模型](/guide/events)。

## 两条接入路径

| | 路径 A：`/notify` | 路径 B：app 命名空间 |
| --- | --- | --- |
| 适合 | 脚本、CI、一次性告警、内部小工具 | 有命名空间的业务系统、告警源、需要回执与审计的平台 |
| 凭证 | 无（或 `auth.enabled` 开启后的 API Key） | 按应用播种的 app token，带 config/trigger/query 权限 |
| 内容与路由 | 直接给 `title`/`body` + `channels`，或 `type`+`level` 按路由表走 | 品类 + 受众，渠道由受众关系与投递策略决定，**应用不配渠道** |
| 状态回查 | `/logs`（全局视图，需管理面权限） | `/apps/{app}/deliveries`（只看自己，token 即权限） |
| 投递结果回调 | 无 | 有（签名 webhook，至少一次） |

不确定就先走路径 A 跑通链路，再决定要不要命名空间。两边可以并存。

## 前置：服务地址与鉴权

服务地址由部署方提供，形如 `http://herald.example.com:8080`，本页统一写 `http://127.0.0.1:8080`。

开启 `auth.enabled` 的部署，管理面接口要带凭证，三种写法等价（选一种）：

```http
Authorization: Bearer <api-key-or-jwt>
X-API-Key: <api-key>
GET /api/v1/logs?api_key=<api-key>
```

`/api/v1/status`、`/feeds/**` 匿名可用；app 面（`/api/v1/apps/**`）独立鉴权，即使 `auth.enabled` 关着也要带 app token。app token 只从请求头取，**没有 query 参数回退**。

## 路径 A：一分钟发出第一条告警

`channels` 直接点名渠道实例名，服务端照单投递：

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

受理结果（`code:0` 只代表受理，送达与否看 `logs`）：

```json
{"code":0,"message":"ok","data":{"accepted":["log"],"notification_id":"8e1ea3c3-...","task_ids":["21a2a819-..."]}}
```

不点名 `channels` 时按 `type`+`level` 查路由表（`routes`/`level_routes`），都没命中报 422 `no route: no route found for type=...`。字段全表见 [REST API 的 notify 一节](/api/rest#notify-request)。

网络重试场景给同一个 `idempotency_key`：进程生命周期内同键重复请求返回首次结果、不再投递。

## 路径 B：app 命名空间

### 1. 播种 app（有 herald 配置权的人做一次） {#seed-app}

app 与 token 在 heraldd 配置文件里播种，**没有运行时铸造 token 的 API**。权限三档：`config` 改品类/模板/策略/回调，`trigger` 触发投递，`query` 读投递状态与审计流。一枚 token 一个权限集，按需多枚。

```yaml
apps:
  demo-app:
    tokens:
      - secret: "demo-config-token-0123456789abcdef"
        scopes: [config]
      - secret: "demo-trigger-token-0123456789abcdef"
        scopes: [trigger]
      - secret: "demo-query-token-0123456789abcdef"
        scopes: [query]
```

本页示例统一用这组演示凭证，照抄即可跑通；生产环境替换成自己的值（token secret 全局不可重复，非空即可，无长度下限；回调面的 secret 才有 16–128 字符要求）。配置写错（scope 拼写超出三级词汇、app 名非法、secret 重复）heraldd 拒起。品类名与 app 名共用 `^[a-zA-Z0-9._-]{1,64}$`。

app token 的用法与 API Key 相同：`Authorization: Bearer <secret>` 或 `X-API-Key: <secret>`。

### 2. 自检

```bash
curl http://127.0.0.1:8080/api/v1/apps/demo-app \
  -H "X-API-Key: demo-query-token-0123456789abcdef"
```

```json
{"code":0,"message":"ok","data":{"name":"demo-app","scopes":["query"]}}
```

回显的 `scopes` 就是这枚 token 的权限。凭据错误与未知 app 统一返回 401 `invalid app credentials`（不区分，避免探测）。

### 3. 注册品类

品类是触发面的分类键，默认紧急度决定没带 `urgency` 的触发按哪档强度窗口匹配渠道：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/apps/demo-app/categories \
  -H "X-API-Key: demo-config-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{"name":"alerts","default_urgency":"urgent"}'
```

同名同默认值重复注册幂等；改成别的默认值是 409 冲突（品类已注册后默认紧急度不可漂移）。品类名各 app 互相隔离。

### 4. 绑受众

触发面只投给**有出路的受众**：关系 × 紧急度强度区间 × 联系面绑定，缺一都投不出去。三个绑定入口：

| 入口 | 端点 | 谁来操作 |
| --- | --- | --- |
| 受众自助 | `POST /api/v1/audiences/{id}/subscriptions` | 偏好中心，受众自己勾 |
| 平台收敛 | bot `/start`、公众号关注 | 来源适配器自动落表 |
| 管理侧代绑定 | `POST /api/v1/audiences/{id}/surfaces` | 联系信息在整合方库里的场景 |

代绑定一并落默认订阅：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/audiences/user.5/surfaces \
  -H "Authorization: Bearer <operator-token>" \
  -H "Content-Type: application/json" \
  -d '{"channel":"email","target":"dev@example.com","categories":["alerts"]}'
```

受众 id 不含 `:`（保留给 `user:`/`group:` 引用前缀），整合方的 ref 形目标 `user:5` 对应受众 id `user.5`。查某受众现有关系：`GET /api/v1/audiences/{id}/relations`。

### 5. 触发投递

```bash
curl -X POST http://127.0.0.1:8080/api/v1/apps/demo-app/dispatch \
  -H "X-API-Key: demo-trigger-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{
    "category": "alerts",
    "audiences": ["user.5", "group:oncall"],
    "dedup_key": "node-17-down",
    "event_id": "evt-20261010-001",
    "title": "node-17 down",
    "body": "cpu 98%, ping lost"
  }'
```

响应是**受理结果**，不是送达回执：

```json
{
  "code": 0, "message": "ok",
  "data": {
    "notification_id": "a7c6f2bc-...",
    "category": "alerts", "urgency": "urgent", "mode": "escalation",
    "plan": [{"channels": ["email"], "ack_timeout": "15m0s"}],
    "dispatched": [{"audience": "user.5", "channels": ["email"]}],
    "refused": [{"audience": "bob", "channel": "sms", "reason": "phone_disabled"}],
    "task_ids": ["61d35a0f-..."], "accepted": ["email"]
  }
}
```

要点：

- `urgency` 缺省取品类默认，带了就按你的；拼错的紧急度是 422 而不是静默降级；
- `dedup_key`/`state`/`event_id` 进去重闸：同键重复触发返回 `suppressed: true`、不产生投递；状态翻转（`state` 变化）重发；
- 内容走 `template`+`params`（命名空间自己的模板表，看不见全局模板）或直接 `title`+`body`；模板变量语法是 Go text/template 的 <code v-pre>{{.field}}</code>；
- `refused[].reason` 词汇：`filtered` / `phone_disabled` / `intensity_exceeded` / `no_channels`——拒绝即答案，HTTP 仍是 200；
- 全部渠道投递失败才把 body `code` 写成 422。

### 6. 告警源直连：事件适配面

事件语义在自己词汇里的整合方（告警源：kind/severity/target + 自有事件主键）可以不走 dispatch，走 `POST /api/v1/apps/{app}/events`，herald 侧完成映射：

| 你的字段 | herald 侧映射 |
| --- | --- |
| `kind` | → 品类（须已注册） |
| `severity` | `critical`→critical、`warning`→urgent、`info`→normal；其他值 422 |
| `target` | → 受众（ref 形 `user:5` 映射为 `user.5`） |
| `id` | → `event_id`，进去重幂等，投递结果回调原样回带 |

```bash
curl -X POST http://127.0.0.1:8080/api/v1/apps/demo-app/events \
  -H "X-API-Key: demo-trigger-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{"id": 42, "kind": "alerts", "severity": "critical", "title": "db down", "target": "user:5", "meta": {"host": "db-1"}}'
```

响应与 dispatch 完全相同（`DispatchOutcome`）。`meta` 必须是 JSON object，映射为模板参数。

### 7. 查状态与审计

```bash
# 投递记录（自动按命名空间隔离，只看得到自己触发的）
curl -H "X-API-Key: demo-query-token-0123456789abcdef" \
  "http://127.0.0.1:8080/api/v1/apps/demo-app/deliveries?status=failed"

# 审计流：去重折叠、关系变更，按时间升序
curl -H "X-API-Key: demo-query-token-0123456789abcdef" \
  "http://127.0.0.1:8080/api/v1/apps/demo-app/audit"
```

`deliveries` 支持按 `status`/`category`/`audience` 过滤（默认 50 条，上限 500）。被去重折叠的通知不会出现在 deliveries 里，答案在 audit 流的 `delivery.deduped` 事件上：

```json
{"kind":"delivery.deduped","category":"alerts","source":"app:demo-app","detail":"throttled: node-17-down (×1)","at":"2026-10-10T12:57:58+08:00"}
```

### 8. 回调：投递结果与退订回流 {#callback}

配置回调面（config 权限），herald 把投递结果与退订事件推给你：

```bash
curl -X PUT http://127.0.0.1:8080/api/v1/apps/demo-app/callback \
  -H "X-API-Key: demo-config-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://app.example.com/herald/callback","secret":"demo-callback-secret-0123456789"}'
```

约束：url 必须是绝对 http(s) 地址；secret 16–128 字符；读口永不回显 secret。两类事件：

- `delivery_result`：任务落定状态（success/failed + provider/受众/品类），**重试是管道内部事务，应用只看落定**；
- `unsubscribe`：受众退订回流（受众×品类×渠道×关系类型）。

投递保证与验签：

- **至少一次**：回调事件走与投递同一套队列和重试管道，非 2xx 视为失败、按退避策略重试，可能重复；
- **幂等**：每个事件带 `event_id`（UUID，重复投递同 ID），你的端点按它去重；
- **签名**：请求头 `X-Herald-Signature: sha256=<hex>` 是请求体精确字节的 HMAC-SHA256（密钥即你配置的 secret）；`X-Herald-Event-ID` 是同一事件的 ID；
- **没有时间戳头**，重放防护靠接收方按 `event_id` 去重，这是 at-least-once 契约的一部分。

验签代码（接收端骨架，两步：算 HMAC、常量时间比较）：

```go
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "net/http"
)

func verifyHerald(w http.ResponseWriter, r *http.Request, secret []byte) bool {
    body := mustReadAll(r.Body) // 未解析的原始字节，先验签后解析
    mac := hmac.New(sha256.New, secret)
    mac.Write(body)
    want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    if !hmac.Equal([]byte(r.Header.Get("X-Herald-Signature")), []byte(want)) {
        http.Error(w, "bad signature", http.StatusUnauthorized)
        return false
    }
    return true // 2xx 即确认；非 2xx 会被重试
}
```

同一算法的 Python 版：

```python
import hmac, hashlib

def verify(secret: str, body: bytes, signature: str) -> bool:
    want = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(signature, want)
```

以上签名流程已用真实投递往返验证：配置回调 → 触发 dispatch → 接收端用共享 secret 重算 HMAC，比对一致。

### 9. 策略覆盖（可选）

app 域可以按品类/渠道覆盖强度、投递模式、升级链参数、去重频控，四族端点整族替换：

```bash
curl -X PUT http://127.0.0.1:8080/api/v1/apps/demo-app/policies/intensity \
  -H "X-API-Key: demo-config-token-0123456789abcdef" \
  -H "Content-Type: application/json" \
  -d '{"email": "L3"}'
```

强度词汇 `L0..L5`（RSS→电话），模式 `fixed|escalation|parallel`，频控档 `once|throttle|always`。app 覆盖只叠加在运营方全局表上，**手机闸门不可被 app 覆盖**，渠道×关系矩阵是安全底线，没有应用级放宽端点。逐字段参考见[应用接入 API](/api/apps#policies)。

## 接入方清单

| 接入方 | 角色 | 接入点 | 状态 |
| --- | --- | --- | --- |
| croupier | 状态源/告警出口：capture 告警、healthprobe 故障窗口、构建失败 | app 命名空间 `croupier`，trigger token，走事件适配面 `POST /api/v1/apps/croupier/events`；自有事件主键（outbox id）进 `event_id`；herald 不可达只记日志与失败计数、不阻塞告警落库 | 已对接 |
| cockpit | 检测编排，告警出口预留 | `Alerter` 接口留位，推送通道对接留批 | 留位未接 |
| 其他外部系统 | 通用业务接入 | 路径 A（`/notify`）或播种独立 app 命名空间 | 按需 |

## SDK

官方维护的 Go SDK 在 `apps-sdk/go`，一枚 token 一个 Client，权限由 token 决定：

```go
import sdk "github.com/cuihairu/herald/apps-sdk/go"

c := sdk.New("http://herald:8080", "demo-app", os.Getenv("DEMO_CONFIG_TOKEN"))
_ = c.RegisterCategory(ctx, "alerts", "urgent")
_ = c.SetCallback(ctx, "https://app.example.com/herald/callback", secret)
```

错误分两类：`*sdk.Error`（herald 的拒绝，带 HTTP 状态与 message，重试无意义）与传输层故障（`sdk.IsTransport` 判别，是否重试自行决定）。

其他语言不提供手写 SDK：接入协议就是 REST + 签名 webhook，任何 HTTP 客户端都能接。需要代码生成时，用[应用接入 API 的 OpenAPI 规范](/api/apps#openapi-规范)喂给 openapi-generator，无需等官方支持。

## 常见错误速查

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 401 `invalid app credentials` | token 错、app 名错、或忘了带头 | 核对播种配置里的 secret；用 `GET /apps/{app}` 自检 |
| 403 `token lacks trigger scope` | token 权限不够 | 换对应权限的 token，或播种时补 scope |
| 404 `category X is not registered` | dispatch 用的品类没注册 | 先 `POST categories`；`delivery.categories` 配置块不算注册 |
| 422 `severity must be critical/warning/info` | 事件适配面 severity 拼错 | 三个值只认小写全拼，`warn` 不行 |
| 422 `function "node" not defined`（模板渲染） | 模板用了 <code v-pre>{{node}}</code> | Go text/template 语法是 <code v-pre>{{.node}}</code> |
| 200 + `suppressed: true` | 去重闸折叠（同 dedup_key 窗口内重复） | 预期行为；确需重发换 dedup_key 或等窗口过 |
| 200 + `refused` | 受众没有出路（关系/强度/联系面缺一） | 看 `reason` 字段对症绑定，见上文第 4 步 |
| 200 `code:422` + `all channels failed` | 全部投递失败 | 查 `deliveries` 与 audit 流定位单渠道错误 |

更多传输层错误的定位见[排错指南](/guide/troubleshooting)。
