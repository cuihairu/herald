# 架构概述

## 项目定位

Herald 是一个：

> **事件驱动的通知投递基础设施**（Event-driven Delivery Infrastructure）

**不是：**

- ❌ Webhook 聚合工具
- ❌ 消息推送 SDK

**核心流程：**

```
Event → Route → Dispatch → Delivery Runtime
```

## 核心设计思想

### 1. HTTP First

Herald 的主要用户接口是 **HTTP REST API**，而不是 SDK。

**原因：**

- curl 即可使用
- 自动化友好
- CI/CD 友好
- 多语言天然兼容
- 降低接入成本

### 2. Runtime First

Herald 的复杂度核心不是 Provider，而是 **Provider Runtime**。

不同 Provider 有不同的 Runtime 要求：

| Provider       | Runtime 要求      |
| -------------- | --------------- |
| Telegram       | HTTP            |
| 企业微信 Bot      | HTTP            |
| 飞书             | HTTP            |
| 邮件             | SMTP            |
| 微信公众号         | HTTP            |
| 微信个人推送        | HTTP            |

> Provider 的本质差异来自运行环境。

### 3. Event First

Herald 不只是"发送消息"，而是"处理事件"。

**示例事件：**

```json
{
  "type": "node.offline",
  "labels": {
    "region": "shanghai"
  }
}
```

然后通过 `route → delivery` 进行投递。

## 总体架构

```
                   +------------------+
                   | External Client  |
                   |------------------|
                   | curl             |
                   | requests         |
                   | axios            |
                   | automation       |
                   +--------+---------+
                            |
                        HTTP REST
                            |
                  +---------v---------+
                  |    Herald API     |
                  +---------+---------+
                            |
                      Event Queue
                            |
                  +---------v---------+
                  |    Herald Core    |
                  |-------------------|
                  | Route Engine      |
                  | Retry             |
                  | Dedup             |
                  | Rate Limit        |
                  | Runtime Manager   |
                  +---------+---------+
                            |
          +-----------------+-----------------+
          |                                   |
          v                                   v

 +----------------------+       +--------------------------+
 | Builtin Runtime      |       | Worker Runtime           |
 |----------------------|       |--------------------------|
 | Telegram             |       | WeChat MP (公众号)          |
 | Feishu               |       | WeChat Push (第三方推送)      |
 | Email                |       | Browser Automation       |
 | Discord              |       | SMS                      |
 +----------------------+       +--------------------------+
```

## 最终定位

Herald：**Event-driven Delivery Infrastructure**

**核心价值：**

- 统一事件
- 统一 Runtime
- 统一调度
- 统一投递
