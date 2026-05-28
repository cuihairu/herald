# 微信公众号推送

本指南介绍如何使用自己的微信公众号实现消息推送。

## 两种方式

Herald 支持两种微信公众号推送方式：

| 方式 | 说明 | 适用场景 |
|------|------|---------|
| **Builtin Provider** | 直接使用微信模板消息 API | 简单场景，已有公众号 |
| **Worker Provider** | 通过 Worker 进程处理 | 复杂场景，需要额外逻辑 |

## 方式一：Builtin Provider（推荐）

### 配置

```yaml
providers:
  wechatmp:
    type: wechatmp
    enabled: true
    config:
      app_id: "${WECHATMP_APP_ID}"
      app_secret: "${WECHATMP_APP_SECRET}"
      template_id: "${WECHATMP_TEMPLATE_ID}"
      default_url: "https://your-domain.com"
```

### 发送消息

```bash
curl -X POST http://localhost:8080/api/v1/notify \
  -H "Content-Type: application/json" \
  -d '{
    "type": "order",
    "title": "订单通知",
    "body": "您的订单已发货",
    "level": "info",
    "channels": ["wechatmp"],
    "recipients": {
      "wechatmp": ["用户OpenID"]
    }
  }'
```

### 模板数据

Herald 会自动将内容映射到微信模板字段：

| 微信字段 | Herald 内容 | 说明 |
|---------|------------|------|
| `thing1` | title | 标题（最多20字符） |
| `thing2` | body | 内容（最多30字符） |
| `character_string1` | level | 级别 |
| `time3` | 当前时间 | 时间戳 |

### 前置要求

**企业资质：**
- ✅ 营业执照
- ✅ 对公账户
- ✅ 300元/年认证费
- ⏱ 审核周期：7-30个工作日

**技术要求：**
- ✅ 微信服务号
- ✅ 已通过微信认证
- ✅ 已开通模板消息功能

### 申请流程

1. **注册服务号**：访问 https://mp.weixin.qq.com/ 注册服务号
2. **企业认证**：提交营业执照、对公账户信息，缴纳认证费
3. **开通模板消息**：认证后在公众号后台开通模板消息功能
4. **创建模板**：在模板库中选择或创建模板，记录模板 ID

### 配置说明

| 参数 | 说明 | 示例 |
|------|------|------|
| `app_id` | 公众号 AppID | `wx1234567890abcdef` |
| `app_secret` | 公众号 AppSecret | `abcdef1234567890abcdef` |
| `template_id` | 模板消息 ID | `AT0001` |
| `default_url` | 点击模板跳转的 URL（可选） | `https://your-domain.com` |

### 限制说明

| 限制项 | 说明 |
|--------|------|
| **推送频率** | 单用户每分钟 100 条 |
| **模板数量** | 最多 25 个模板 |
| **模板审核** | 需要微信审核（1-7天） |
| **行业限制** | 需要与公众号行业匹配 |
| **关键词** | 内容不能包含敏感词 |

## 方式二：Worker Provider

适用需要复杂处理逻辑的场景，如：
- 需要访问数据库获取用户信息
- 需要调用其他服务
- 需要自定义消息格式

### 配置

```yaml
providers:
  wechatmp:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"
```

### Worker 开发

使用 Worker SDK 开发：

```go
package main

import (
    "context"
    workersdk "github.com/cuihairu/herald/worker-sdk/go"
    "github.com/cuihairu/herald/protocol"
)

func main() {
    config := &protocol.WorkerConfig{
        WorkerID:          "wechat-worker-01",
        CoreURL:           "ws://localhost:8081",
        ReconnectDelay:    5 * time.Second,
        HeartbeatInterval: 30 * time.Second,
        Capabilities:      []string{"wechatmp"},
    }

    client := workersdk.NewClient(config)

    client.OnTask(func(task *protocol.DispatchMessage) error {
        // 处理任务
        err := sendWechatTemplateMessage(task)
        client.Ack(task.TaskID, err == nil, "")
        return err
    })

    client.Connect(context.Background())
    select {}
}
```

详见 [Worker SDK](/runtime/sdk)。

## 获取用户 OpenID

用户关注公众号后需要获取 OpenID 才能发送消息：

### 方式 1: 通过用户授权

生成授权链接让用户授权：

```
https://open.weixin.qq.com/connect/oauth2/authorize?appid=APPID&redirect_uri=REDIRECT_URI&response_type=code&scope=snsapi_base#wechat_redirect
```

### 方式 2: 通过关注事件

在公众号服务器配置中处理关注事件获取 OpenID。

## 常见问题

### Q: 模板被拒怎么办？

A: 根据拒绝原因修改：
- 内容不符合行业规范
- 包含推广信息
- 模板格式不规范

### Q: 用户收不到消息？

A: 检查：
- 用户是否已关注公众号
- OpenID 是否正确
- 模板是否已通过审核
- 是否触发了频率限制

### Q: Access Token 过期？

A: Provider 会自动管理 Access Token，无需手动处理。

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider
- [微信个人推送](./wechat.md) - 使用第三方服务的简单方案
- [Worker SDK](/runtime/sdk) - 了解 Worker 开发
