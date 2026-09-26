# API 概述

## 协议

Herald 使用 **HTTP REST + JSON** 作为唯一公开协议。

## 基础 URL

```
http://your-host:8080/api/v1
```

## 认证

除 `/auth/login`、`/auth/refresh` 与 Provider 回调外，所有端点都要求
`Authorization: Bearer <token>`。Dashboard 登录后把 token 存在
`localStorage`，由 axios 请求拦截器统一带上。

## API 列表

| 方法   | 路径         | 描述    |
| ---- | ---------- | ----- |
| POST | /notify    | 发送通知  |
| GET  | /status    | 查询状态  |
| GET  | /providers | 查询 Providers |
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
| GET  | /templates | 获取模板列表 |
| POST | /templates | 创建模板 |
| GET  | /templates/{id} | 获取单个模板 |
| PUT  | /templates/{id} | 更新模板 |
| DELETE | /templates/{id} | 删除模板 |

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
  "message": "invalid request",
  "data": null
}
```
