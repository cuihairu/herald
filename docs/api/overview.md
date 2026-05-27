# API 概述

## 协议

Herald 使用 **HTTP REST + JSON** 作为唯一公开协议。

## 基础 URL

```
http://your-host:8080/api/v1
```

## 认证

（待实现）

## API 列表

| 方法   | 路径         | 描述    |
| ---- | ---------- | ----- |
| POST | /notify    | 发送通知  |
| GET  | /status    | 查询状态  |
| GET  | /providers | 查询 Providers |
| GET  | /templates | 获取模板列表 |
| POST | /templates | 创建模板 |
| GET  | /templates/{id} | 获取单个模板 |
| PUT  | /templates/{id} | 更新模板 |
| DELETE | /templates/{id} | 删除模板 |

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
