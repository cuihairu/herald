# 应用接入 API

`/api/v1/apps/{app}/**` 是集成者命名空间：品类、模板、策略、触发、回执全部按 app 隔离，凭证是播种在配置文件里的 app token。接入动作的引导见[接入指南](/guide/integration)，本页是逐端点参考。

## 认证与通用约定

- **凭证**：`Authorization: Bearer <secret>` 或 `X-API-Key: <secret>`。只从请求头取，没有 query 参数回退（URL 会进日志）。
- **401**：未知 app 与错误 secret 统一返回 `{"code":401,"message":"invalid app credentials"}`——不区分两种情况，防命名空间探测（用未播种的 app 名打过来也是这个响应）。
- **403**：凭据有效但 scope 不够：`{"code":403,"message":"token lacks trigger scope"}`。scope 三档 `config | trigger | query`，互不继承：config **不隐含** query。
- **请求体上限 1 MiB**，超出按非法请求处理。
- **信封**：成功 `{"code":0,"message":"...","data":{...}}`；错误 `code` 等于 HTTP 状态码、`data` 键整体缺席。app 面的所有错误（含 405）都是 JSON 信封。
- **限流**：HTTP 层无每客户端限流；投递层有按渠道的令牌桶（`providers.<name>.rate_limit`），对触发接口无感。

响应示例里的演示凭证与[接入指南](/guide/integration#seed-app)一致。

## GET /api/v1/apps/{app}

自检端点：确认凭据有效并回显 token 权限集。无请求参数，任意 HTTP 方法等价。

```bash
curl -H "X-API-Key: demo-query-token-0123456789abcdef" \
  http://127.0.0.1:8080/api/v1/apps/demo-app
```

```json
{"code":0,"message":"ok","data":{"name":"demo-app","scopes":["query"]}}
```

`scopes` 按固定契约顺序（config, trigger, query）输出。

## 品类：POST / GET /api/v1/apps/{app}/categories

品类是触发面的分类键。POST 需 `config`，GET 需 `query`。

**POST 请求体**：

| 字段 | 类型 | 必填 | 取值 | 说明 |
| --- | --- | --- | --- | --- |
| name | string | 是 | `^[a-zA-Z0-9._-]{1,64}$` | 品类名，命名空间内唯一 |
| default_urgency | string | 是 | `routine`/`normal`/`urgent`/`critical`（含中文别名） | 未带 `urgency` 的触发按此档匹配强度窗口 |

幂等：同名同值重复注册返回 200；同名不同值返回 409 `category already registered with a different default urgency`——品类注册后默认紧急度不可漂移，要改就换品类名。

**GET 响应**（按 name 排序，新命名空间为 `[]`）：

```json
{"code":0,"message":"ok","data":{"categories":[{"name":"alerts","default_urgency":"urgent"}]}}
```

## 策略：GET /policies 与 PUT /policies/{family} {#policies}

app 域对投递策略的覆盖集。GET 需 `query`；PUT 四族都需 `config`，**整族替换**（body 里没出现的键被清掉），各族之间互不影响。

**GET /api/v1/apps/{app}/policies**（聚合读回，未设置的族不出现，全空时 `data` 为 `{}`）：

```json
{"code":0,"message":"ok","data":{
  "channel_intensity": {"email": "L3"},
  "mode_by_category": {"alerts": "escalation"},
  "ack_timeout": "3m0s",
  "dedup_tiers": {"alerts": "always"},
  "dedup_windows": {"alerts": "10m0s"}
}}
```

**四个 PUT 子端点**：

| 端点 | 请求体形状 | 有效值 |
| --- | --- | --- |
| PUT /policies/intensity | `{"<channel>": "<intensity>", ...}` | `L0`–`L5`（也接受 RSS/Mail/Inbox/IM/SMS/Phone 别名） |
| PUT /policies/delivery-mode | `{"<category>": "<mode>", ...}` | `fixed` / `escalation` / `parallel`（含中文别名） |
| PUT /policies/escalation | `{"ack_timeout": "<duration>"}` | Go duration（`"3m"`、`"1h30m"`）；必填、非负；`"0s"` 清除覆盖 |
| PUT /policies/dedup | `{"tiers": {"<category>": "<tier>"}, "windows": {"<category>": "<duration>"}}` | tier ∈ `once`/`throttle`/`always`；window 必须为正 duration |

成功 200，`data` 是聚合后的完整覆盖集。坏值 422 且点名键名，如 `tiers[alerts]: dedup: unknown frequency tier "hourly" (want once|throttle|always)`。

语义边界：app 覆盖只叠加在运营方全局表上；**手机闸门（must-deliver 双重同意）不可被 app 覆盖**；渠道×关系矩阵是安全底线，没有应用级放宽端点。

## 模板：POST / GET /api/v1/apps/{app}/templates

命名空间内独立的模板表，渲染只在命名空间内、全局模板不可见。POST（upsert）需 `config`，GET 需 `query`。

**POST /templates 请求体**：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| id | string | 是 | 命名空间内唯一键 |
| name | string | 是 | 显示名 |
| title | string | 是 | 标题模板，Go text/template 语法 <code v-pre>{{.field}}</code>；缺失 422 |
| level | string | 否 | 惯用 `error`/`warning`/`info` |
| fields | array | 否 | 元素 `{"label","value","type"}`，label/value 非空 |
| bindings | object | 否 | 渠道名 → `{format, template_code, template_id, params, param_order}` |

幂等 upsert：重复 POST 同 id 返回 200 并整体替换。成功 200，`data` 回显模板对象（带 created_at/updated_at）。

**GET /templates**：`data` 直接是模板对象数组（不是包 map），新命名空间为 `[]`。
**GET /templates/{id}**：未知 id 404 `template <id>: template not found: <id>`（示例 id 为 ghost 时即 `template ghost: template not found: ghost`）。
**DELETE /templates/{id}**：成功 `{"code":0,"message":"deleted"}`；未知 id 404 `template not found: <id>`（删除未知 id 是错误，不是幂等 no-op）。

## 回调配置：GET / PUT / DELETE /api/v1/apps/{app}/callback

登记「herald 往哪里推投递结果与退订事件」。GET 需 `query`，PUT/DELETE 需 `config`。

**PUT 请求体**：

| 字段 | 类型 | 必填 | 校验 |
| --- | --- | --- | --- |
| url | string | 是 | 绝对 http(s) URL，否则 422 |
| secret | string | 是 | 16–128 字符，否则 422 |

成功 200。整体替换语义。**GET 读口永不回显 secret**：

```json
{"code":0,"message":"ok","data":{"url":"http://127.0.0.1:29090/callback","has_secret":true}}
```

未配置时 `{"url":"","has_secret":false}`。DELETE 未配置也返回 200 `deleted`。

**出站回调载荷**（herald → 你的 url，POST application/json）：

```json
{
  "event_id": "751c5b97-0191-40a9-9291-153944909201",
  "app": "demo-app",
  "kind": "delivery_result",
  "at": "2026-10-10T12:54:45+08:00",
  "delivery": {
    "task_id": "944f0c97-...",
    "event_id": "evt-docs-001",
    "audience_id": "log",
    "category": "alerts",
    "channel": "log",
    "status": "success",
    "error": ""
  }
}
```

| 字段 | 说明 |
| --- | --- |
| event_id | 本次回调事件的 UUID，**接收方幂等键**（至少一次投递，可能重复） |
| kind | `delivery_result`（投递落定）或 `unsubscribe`（退订回流） |
| delivery.event_id | **回显你在 dispatch/events 里声明的 event_id**，回执按它对回原始事件；`status` ∈ success/failed |
| unsubscribe | `{audience_id, category, channel, relation_type, actor?}` |

请求头：`X-Herald-Signature: sha256=<hex>`（请求体精确字节的 HMAC-SHA256，密钥即 PUT 的 secret）与 `X-Herald-Event-ID`（等于 body 的 event_id）。验签代码与重放防护见[接入指南](/guide/integration#callback)。无时间戳头，不做时间窗校验；非 2xx 走投递同一套重试管道（默认 3 次指数退避）。未配置回调的 app 静默跳过，回调失败不影响原投递。

## 触发：POST /api/v1/apps/{app}/dispatch

需 `trigger`。品类语义的触发面：**品类必须已在本命名空间注册**（运营方 `delivery.categories` 配置块不算），未注册 422。

**请求体**：

| 字段 | 类型 | 必填 | 缺省 | 说明 |
| --- | --- | --- | --- | --- |
| category | string | 是 | — | 已注册品类；未注册 422 |
| urgency | string | 否 | 品类默认 | `routine`/`normal`/`urgent`/`critical`；非法 422，不钳制 |
| relation_type | string | 否 | `subscription` | `subscription`（订阅型）或 `enrollment`（指派型，默认并行投递）；其他值 422 |
| audiences | string[] | 是 | — | 受众 id、ref 形（`user:5`/`group:oncall`）或裸渠道名；空数组 422 |
| dedup_key | string | 否 | 内容派生 | 去重键；同键窗口内重复触发返回 `suppressed:true` |
| event_id | string | 否 | — | 你方事件身份，进去重幂等层，投递结果回调原样回带 |
| state | string | 否 | — | 状态机维度（`down`/`ok`）；同键同态折叠，翻转重发 |
| template | string | 否 | — | 命名空间模板 id；渲染失败/未知 422 |
| params | object | 否 | — | 模板变量 |
| title / body | string | 否 | — | 直投内容，与 template 二选一 |

**成功 200 响应**（受理结果，不是送达回执）：

```json
{
  "code": 0, "message": "ok",
  "data": {
    "notification_id": "a7c6f2bc-4d70-4374-8d89-e01a39714275",
    "category": "alerts",
    "urgency": "urgent",
    "mode": "escalation",
    "plan": [{"channels": ["email"], "ack_timeout": "15m0s"}],
    "dispatched": [{"audience": "log", "channels": ["log"]}],
    "refused": [{"audience": "bob", "channel": "sms", "reason": "phone_disabled"}],
    "task_ids": ["61d35a0f-6196-46c9-87da-76b81715a37f"],
    "accepted": ["log"]
  }
}
```

| 键 | 说明 |
| --- | --- |
| plan | 升级链全链展示，首段先行投递；`mode` ∈ fixed/escalation/parallel |
| dispatched | 命中并已生成投递的受众 |
| refused | 被拒受众与原因：`filtered` / `phone_disabled` / `intensity_exceeded` / `no_channels` |
| suppressed | 去重折叠时为 `true`，此时**只有** notification_id/category/urgency/suppressed 四个键，无 task_ids |

拒绝即答案：有 refused 也是 200；全部渠道投递失败才把 body `code` 写 422。无投递策略组件的部署 404 `delivery policy not configured`。

## 事件适配：POST /api/v1/apps/{app}/events

需 `trigger`。给事件语义在自己词汇里的告警源用，映射到与 dispatch 完全相同的管线，响应同 dispatch。

**请求体**：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| kind | string | 是 | 映射为品类，须已注册；空 422 |
| severity | string | 是 | `critical`→critical、`warning`→urgent、`info`→normal；其他值 422 |
| target | string | 是 | 单受众；ref 形 `user:1` 映射为受众 id `user.1` |
| id | number | 否 | 你方事件主键（如 outbox id），转成 `event_id`，回调回带 |
| title / body | string | 否 | 直投内容 |
| dedup_key | string | 否 | 同 dispatch |
| meta | object | 否 | 模板参数；非 object 400 |
| occurred_at | string | 否 | RFC3339；herald 仅解析，时间戳以 herald 侧为准 |

不支持 dispatch 的 `relation_type`/`urgency`/`template`/`state`——固定走 subscription 关系与 severity→urgency 映射。

## 查询：GET /deliveries 与 GET /audit

都需 `query`，命名空间边界强制：只看得到 `source=app:<name>` 的行。

**GET /deliveries**：

| 参数 | 缺省 | 说明 |
| --- | --- | --- |
| offset | 0 | 非法/负值按 0 |
| limit | 50 | `<=0` 归 50，`>500` 截 500 |
| status | 全部 | `success`/`failed`/`pending`/`shadow` |
| category | 全部 | 品类精确匹配 |
| audience | 全部 | 精确匹配触发时传入的受众串（dispatch 原样落库；events 的 `target` 先经 `user:5`→`user.5` 映射再落库，按映射后的 id 查） |

```json
{"code":0,"message":"ok","data":{"total":1,"offset":0,"limit":50,"logs":[{
  "id": "6af218e5-...", "provider": "log", "payload_kind": "content",
  "level": "warning", "status": "success",
  "created_at": "2026-10-10T12:55:04+08:00", "completed_at": "2026-10-10T12:55:04+08:00",
  "relation_type": "subscription", "source": "app:demo-app",
  "audience_id": "log", "category": "alerts"
}]}}
```

行在任务真正执行后才出现；被去重折叠的触发不出现在这里（去 audit 找 `delivery.deduped`）。

**GET /audit**：审计流，时间升序。参数 `since`（可选 RFC3339，非法 400）。审计存储未配置时**先于认证**返回 404 `audit trail not configured`。

```json
{"code":0,"message":"ok","data":{"events":[{
  "kind": "delivery.deduped", "category": "alerts",
  "relation_type": "subscription", "source": "app:demo-app",
  "detail": "throttled: node-17-down (×1)", "at": "2026-10-10T12:57:58+08:00"
}]}}
```

`kind` 词汇：`relation.subscribe` / `relation.enroll` / `relation.terminate` / `surface.bind` / `surface.rebind` / `surface.invalidate` / `delivery.deduped`。

## OpenAPI 规范

接入协议只有 REST + 签名 webhook，语言不设限。规范文件：**`docs/api/openapi.yaml`**（OpenAPI 3.1，覆盖 `/notify` 与 `/apps/{app}/**` 全部端点）。用 openapi-generator 自助生成客户端：

```bash
npx @openapitools/openapi-generator-cli generate \
  -i docs/api/openapi.yaml \
  -g python \        # 或 typescript-axios / java / go 等
  -o ./herald-client
```

规范里 `servers.url` 填你的部署地址；app token 对应的安全方案名 `AppToken`（apiKey，header `X-API-Key`），管理面 token 用 `BearerAuth`。Go 用户直接用官方 `apps-sdk/go`（`apps-sdk/go/example` 附带一个走完全部面的可运行自检示例），不必生成。

## 错误码总表

| HTTP | 何时发生 | 消息示例 |
| --- | --- | --- |
| 400 | 请求体非法 JSON / meta 非 object | `invalid JSON body`（callback PUT 例外，报 `invalid request body`） |
| 401 | token 错、app 名错、缺凭据（含用未播种的 app 名访问） | `invalid app credentials` |
| 403 | scope 不够 | `token lacks trigger scope` |
| 404 | audit 存储未配置；无投递策略组件 | `audit trail not configured` / `delivery policy not configured` |
| 405 | 端点不支持的方法（每个端点都有显式检查——四个策略族只收 PUT、dispatch/events 只收 POST、show/policies/deliveries/audit 只收 GET） | `method not allowed` |
| 409 | 品类同名不同默认紧急度 | `category already registered with a different default urgency` |
| 422 | 字段校验失败（品类未注册/severity 非法/urgency 非法/relation_type 非法/模板渲染失败/策略坏值） | `severity must be critical/warning/info` |

全部触发与查询成功路径都返回 200；`suppressed`、`refused`、`failed` 都是受理结果里的字段，不是 HTTP 错误。
