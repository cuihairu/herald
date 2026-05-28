# 核心设计

## 系统定位

Herald 是：
- ✅ **事件驱动的投递基础设施**
- ✅ **Runtime 系统**
- ✅ **统一事件分发平台**

Herald 不是：
- ❌ Plugin 系统
- ❌ 简单的 Webhook 聚合工具
- ❌ 消息推送 SDK

## 核心概念

### Provider vs Runtime

**Provider**：消息渠道的抽象，描述"往哪里发"

**Runtime**：Provider 的运行环境，描述"怎么发"

不同 Provider 需要不同的 Runtime 环境：

| Provider | Runtime 类型 | 原因 |
|----------|-------------|------|
| Telegram | Builtin | HTTP API，简单直接 |
| Email | Builtin | SMTP，标准协议 |
| 微信公众号 | Worker | 需要特殊认证和会话管理 |
| 微信个人推送 | Worker | 需要第三方服务或特殊环境 |

### Runtime Capability

Runtime 通过 capability 声明自己支持的能力：

```json
{
  "worker_id": "wechat-worker-01",
  "capabilities": ["wechatmp", "wechat"]
}
```

Herald Core 根据 capability 选择合适的 Worker：
- 需要 `wechatmp` 时 → 分发给支持 `wechatmp` 的 Worker
- 需要 `wechat` 时 → 分发给支持 `wechat` 的 Worker

## Delivery 抽象

### Notification → DeliveryTask

API 层接收的是 **Notification**（用户意图），Provider 层接收的是 **DeliveryTask**（投递指令）。

转换过程：

```
Notification
    ↓ (路由)
目标渠道列表
    ↓ (模板渲染)
DeliveryTask[] (每个渠道一个)
    ↓ (入队)
Queue
    ↓ (调度)
Dispatcher → Runtime → Provider
```

### Payload 类型

| PayloadKind | 说明 | 适用 Provider |
|-------------|------|--------------|
| `content` | 渲染后的内容 | Telegram、Email、Feishu 等 |
| `provider_template` | 服务商模板 | 阿里云 SMS、腾讯云 SMS |
| `raw` | 原始透传 | Worker Provider |

## Runtime vs Plugin

| 特性 | Runtime | Plugin |
|------|---------|--------|
| 生命周期 | 持久运行 | 按需加载 |
| 连接方式 | WebSocket | 进程内 |
| 崩溃隔离 | 是 | 否 |
| 状态管理 | 独立 | 共享 |
| 适用场景 | 复杂环境、特殊权限 | 简单逻辑 |

Worker Runtime 本质上更像：
- Jenkins Agent
- Buildkite Agent
- Discord Gateway Bot

而不是简单的"插件"。

> 它们是 **长期在线的 Runtime 节点**，不是临时加载的插件。

## 设计原则

### 1. HTTP First

主要接口是 HTTP REST API，而非 SDK。

**收益：**
- curl 即可使用
- 自动化友好
- CI/CD 友好
- 多语言天然兼容

### 2. Event First

处理的是"事件"而非简单的"消息"。

**区别：**
- 消息：我要发什么
- 事件：发生了什么

事件可以通过路由规则自动决定：
- 发送到哪些渠道
- 使用什么模板
- 什么级别

### 3. Template First

模板定义与渠道无关，一次定义，多渠道复用。

```yaml
templates:
  server_alert:
    name: "服务器告警"
    title: "【{{.Level}}】{{.Service}}"
    bindings:
      email: { format: html }
      telegram: { format: markdown }
```

### 4. Runtime First

承认 Provider 的复杂度来自 Runtime 环境，而非 Provider 本身。

因此支持：
- Builtin Runtime（简单场景）
- Worker Runtime（复杂场景）
