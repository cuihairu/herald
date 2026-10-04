# 简介

## 什么时候需要 Herald

当你发现自己在**到处写「发通知」的代码**——告警系统里拼钉钉报文、运营后台里接短信 SDK、CI 里塞 curl——就该考虑 Herald 了。它是一个事件驱动的**消息通知分发平台**：业务方只管把「发生了一件事」交给 Herald，由它负责渲染格式、选对渠道、可靠送达。

典型场景：

- **系统告警推送**：监控事件按级别路由，error 进值班群、warning 留群消息，超时未确认自动升级
- **运营消息触达**：公告模板一次定义，飞书 / 企微 / 邮件多点分发，受众用命名分组管理
- **验证码与事务短信**：对接阿里云 / 腾讯云 / 网易云信，限流防刷 + 失败自动重试
- **多通道兜底**：一条告警同时扇出多个通道，单通道故障有重试与投递日志兜底

每个场景的完整「怎么配、怎么发、跑起来什么样」见[场景与接入](/guide/use-cases)。

## 它长什么样

Herald 是单个二进制（或 Docker 容器），**轻量、Provider 无关**：业务方描述「发生了什么、通知什么、通知谁」，渠道细节（Telegram / 飞书 / 邮件 / 短信… 具体是哪个、怎么送达、失败怎么重试）全部交给 Herald。核心是一条通知流水线：

```
Notification → 受众解析(通知谁) → 路由(走哪条通道) → Delivery Task → Queue → Worker → Provider
                 ↑ 模板(内容怎么生) · 规则引擎(放行/抑制/改道) · 去重 · 重试 · 限流
```

- **Notification ≠ Delivery**：一条通知可展开成多条投递（多渠道扇出），每条 Delivery 独立状态、独立重试
- **Audience ≠ Channel ≠ Provider**：「通知谁」「哪类通知」「怎么发送」三个维度解耦——当前版本受众的 `group:` 形态即[通知群组](/design-notification-groups)，`user:` 级细分属于规划中的[Audience 领域模型](/design-audience-model)

- **HTTP First**：`POST /api/v1/notify` 一条 curl 即可发通知，curl 友好、无业务 SDK 依赖
- **多渠道 Provider**：内置即时通讯（飞书、企微、钉钉、Slack、Discord、Telegram、微信）、邮件、短信（阿里云、腾讯云、网易）、Webhook 等十余个通道，统一接口面，见 [Provider 手册](/providers/overview)
- **Runtime First**：Provider 可以内置在核心进程（Builtin），也可以跑在独立的 [Worker 节点](/runtime/worker)上——承认浏览器自动化这类场景的运行环境差异
- **Event First**：处理的是事件而非裸消息——路由（按 type/level）、去重窗口、指数退避重试、按 Provider 限流、规则引擎决策（优先级、默认策略、shadow 观察、for/group_by/inhibit/silence/escalation）

## 两种跑法

**独立服务**：`heraldd serve` 起一个 API + 队列 + worker 池的调度中心，任何语言的业务往它发 HTTP 即可。按[快速开始](/guide/getting-started)跑通第一条通知约 1 分钟。

**Go 库内嵌**：也可以把 Herald 的投递核心当作库引到自己程序里，见[作为 Go 库使用](/library-usage)。

## 与其他方案的区别

| 特性 | Herald | Webhook 工具 | 消息推送 SDK |
| ---- | ------ | ------------ | ------------ |
| 事件驱动 | ✅ | ❌ | ❌ |
| 多渠道聚合 | ✅ | 部分 | ❌ |
| HTTP API | ✅ | ✅ | 部分 |
| 模板系统 | ✅ | ❌ | ❌ |
| 统一路由 / 规则引擎 | ✅ | ❌ | ❌ |
| 去重 / 重试 / 限流 | ✅ | 部分 | ❌ |

## 下一步

- [快速开始](/guide/getting-started) - 1 分钟跑通全链路
- [场景与接入](/guide/use-cases) - 四个典型场景的完整接入
- [Provider 手册](/providers/overview) - 渠道凭据申请与配置
