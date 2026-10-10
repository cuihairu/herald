# API 概述

## 协议

Herald 使用 **HTTP REST + JSON** 作为唯一公开协议。

## 基础 URL

```
http://your-host:8080/api/v1
```

## 认证

端点按四个鉴权域区分（路由与中间件见 `api/server.go` 的注册表）：

1. **Dashboard/API token**（`auth.enabled` 开启时要求 `Authorization: Bearer <token>`）：
   notify、providers、workers、queue、logs、config、templates、rules、groups、rosters、
   alerts、incidents 以及 audiences 的 relations/subscriptions/surfaces。token 来自
   `/auth/login`；`/auth/refresh` 凭旧 token 换新；`/auth/me` 不走该中间件，在 handler
   内部自行验证 token。Dashboard 登录后把 token 存在 `localStorage`，由 axios 请求拦截器统一带上。
2. **App token**（`/api/v1/apps/{app}/**` 集成者面）：按 app 播种的 token 鉴权，
   `Authorization: Bearer <app token>` 或 `X-API-Key`，每枚 token 带 config/trigger/query
   权限集（见[应用接入](/guide/integration)）。未播种的 app 名与错误 secret 统一 401，
   不区分两种情况。
3. **自证回调**（不走 Bearer）：`/callbacks/feishu`（飞书服务器以回调加密密钥认证）、
   `/callbacks/bot` 与 `/callbacks/wechat-mp`（平台回调，按各自共享密钥自证）。
4. **公开读口**：`/api/v1/status` 与 `/feeds/**`（RSS 拉式）匿名可用。

未开启认证时第 1 域全部匿名可用（第 2 域仍需 app token——app 鉴权独立于 `auth.enabled`）。

## API 列表

| 方法   | 路径         | 描述    |
| ---- | ---------- | ----- |
| POST | /auth/login | 登录，换取 JWT token 与用户信息 |
| POST | /auth/refresh | 凭 Bearer token 换发新 token |
| GET  | /auth/me | 查询当前登录用户 |
| POST | /notify    | 发送通知  |
| GET  | /status    | 查询状态（公开读口） |
| GET  | /providers | 查询 Providers |
| POST | /providers/{name}/enable | 启用 Provider |
| POST | /providers/{name}/disable | 禁用 Provider |
| GET  | /config/{name} | 查询 Provider 配置（敏感值脱敏） |
| PUT  | /config/{name} | 更新 Provider 配置 |
| GET  | /workers | 查询已连接的 Workers |
| GET  | /queue | 查询队列深度 |
| GET  | /logs | 查询投递日志 |
| GET  | /logs/stats | 投递日志统计 |
| GET  | /logs/{id} | 查询单条投递日志 |
| GET  | /rules     | 查询规则列表 |
| POST | /rules     | 创建规则（保存即编译表达式） |
| GET  | /rules/{id} | 查询单个规则 |
| PUT  | /rules/{id} | 整体替换规则（启停开关走这条） |
| DELETE | /rules/{id} | 删除规则 |
| GET  | /groups    | 查询通知群组列表 |
| POST | /groups    | 创建群组 |
| GET  | /groups/{id} | 查询单个群组 |
| PUT  | /groups/{id} | 整体替换群组 |
| DELETE | /groups/{id} | 删除群组 |
| GET  | /rosters    | 查询值班表列表 |
| POST | /rosters    | 推送值班表（整表全量） |
| GET  | /rosters/{id} | 查询单个值班表 |
| PUT  | /rosters/{id} | 整体替换值班表时段 |
| DELETE | /rosters/{id} | 删除值班表 |
| GET  | /alerts/{id} | 查询告警确认状态 |
| POST | /alerts/{id}/ack | 确认告警（及时确认会取消待触发的升级） |
| GET  | /incidents | 查询事件账本 |
| GET  | /incidents/{id} | 查询单个事件（含完整时间线） |
| GET  | /templates | 获取模板列表 |
| POST | /templates/create | 创建模板 |
| GET  | /templates/{id} | 获取单个模板 |
| PUT  | /templates/{id} | 更新模板 |
| DELETE | /templates/{id} | 删除模板 |
| POST | /callbacks/feishu | 飞书卡片回调（ack 按钮 / URL 验证；不走 Bearer） |
| POST | /callbacks/bot | Bot 平台回调（/start /stop 等来源入口；共享密钥自证） |
| POST | /callbacks/wechat-mp | 公众号回调（关注/取关来源入口；共享密钥自证） |
| GET  | /audiences/{id}/relations | 查询受众关系（订阅/指派） |
| POST | /audiences/{id}/subscriptions | 偏好中心订阅/退订（来源适配器入口） |
| POST | /audiences/{id}/surfaces | 管理侧代绑定联系面（可顺带落默认订阅） |
| DELETE | /audiences/{id}/surfaces | 解绑联系面（`?channel=`） |

**集成者 app 面**（`/api/v1/apps/{app}/**`，app token 鉴权、命名空间隔离；未播种的 app 名统一 401）：

| 方法 | 路径 | 权限 | 描述 |
| --- | --- | --- | --- |
| GET | /apps/{app} | app token | 自检：回显 token 权限集 |
| GET / POST | /apps/{app}/categories | query / config | 品类列表 / 注册品类（含默认紧急度） |
| GET | /apps/{app}/policies | query | 读策略覆盖（未设置读回空对象） |
| GET / PUT | /apps/{app}/policies/intensity | query / config | 渠道强度覆盖 |
| GET / PUT | /apps/{app}/policies/delivery-mode | query / config | 投递模式覆盖 |
| GET / PUT | /apps/{app}/policies/escalation | query / config | 升级链参数覆盖 |
| GET / PUT | /apps/{app}/policies/dedup | query / config | 去重频控按品类覆盖 |
| GET / POST | /apps/{app}/templates | query / config | 命名空间模板列表 / 注册 |
| GET / DELETE | /apps/{app}/templates/{id} | query / config | 读 / 删命名空间模板 |
| GET / PUT / DELETE | /apps/{app}/callback | config | 回调面配置（URL + secret；读口不回显密钥） |
| POST | /apps/{app}/dispatch | trigger | 触发投递（三方交集匹配，返回受理结果与投递计划） |
| POST | /apps/{app}/events | trigger | 事件接入适配面（kind/severity/target 词汇映射） |
| GET | /apps/{app}/deliveries | query | 投递状态查询（默认 50 条，上限 500） |
| GET | /apps/{app}/audit | query | 审计流（关系变更、去重折叠） |

**RSS 拉式**（挂 `/feeds/**`，不在 `/api/v1` 下，公开读口）：

| 方法 | 路径 | 描述 |
| --- | --- | --- |
| GET | /feeds/{name} | 公共 feed（每品类一个，只含无受众引用的公开内容） |
| GET | /feeds/private/{name} | 私密 feed（按受众 rss_token 寻址，token 重置即失效） |

规则、群组、值班表、告警与事件端点依赖对应组件已配置（如 `rules:`、`groups_store`、
`rosters_store`），未配置时这些端点返回 503。

完整的请求/响应字段见 [REST API 详情](./rest.md)。

## 响应格式

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

错误响应：

```json
{
  "code": 400,
  "message": "invalid request"
}
```

错误响应**省略** `data` 字段（omitempty），而不是输出 `null`。业务层失败（如通知
全部渠道投递失败）返回 HTTP 200，错误码写在 body 的 `code` 里；传输层错误
（参数不合法、资源不存在等）才是对应的 HTTP 状态码。
