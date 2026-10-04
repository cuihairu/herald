# API 概述

## 协议

Herald 使用 **HTTP REST + JSON** 作为唯一公开协议。

## 基础 URL

```
http://your-host:8080/api/v1
```

## 认证

开启 `auth.enabled` 后，除 `/auth/login`、`/auth/refresh`（登录/刷新）与飞书卡片回调
`/callbacks/feishu`（由飞书服务器以回调加密密钥认证）外，所有端点都要求
`Authorization: Bearer <token>`；`/auth/me` 不走该中间件，在 handler 内部自行验证
token。Dashboard 登录后把 token 存在 `localStorage`，由 axios 请求拦截器统一带上。

未开启认证时全部端点匿名可用。

## API 列表

| 方法   | 路径         | 描述    |
| ---- | ---------- | ----- |
| POST | /auth/login | 登录，换取 JWT token 与用户信息 |
| POST | /auth/refresh | 凭 Bearer token 换发新 token |
| GET  | /auth/me | 查询当前登录用户 |
| POST | /notify    | 发送通知  |
| GET  | /status    | 查询状态  |
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
