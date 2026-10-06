---
layout: home

hero:
  name: Herald
  text: 统一消息通知分发平台
  tagline: 一条 HTTP 请求，送达任何渠道 —— 即时通讯 · 短信 · 邮件 · Webhook · App 推送。内置路由、重试、去重、限流与规则引擎，单二进制部署，也可作为 Go 库嵌入。
  actions:
    - theme: brand
      text: 快速开始
      link: /guide/getting-started
    - theme: alt
      text: 使用场景
      link: /guide/use-cases
    - theme: alt
      text: Provider 总览
      link: /providers/overview
    - theme: alt
      text: 架构设计
      link: /architecture/overview

features:
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 9.5h18"/><circle cx="6.3" cy="7.2" r="0.9"/><circle cx="9.3" cy="7.2" r="0.9"/><path d="M7 14h6"/><path d="m11 12 2 2-2 2"/></svg>'
    title: HTTP First
    details: curl 友好，无业务 SDK 依赖，天然支持多语言和自动化
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><rect x="5" y="5" width="14" height="14" rx="2.5"/><path d="M9.5 9.5v5l4-2.5z"/><path d="M9 3v2"/><path d="M15 3v2"/><path d="M9 19v2"/><path d="M15 19v2"/></svg>'
    title: Runtime First
    details: 承认 Provider 的本质差异来自运行环境，支持 Builtin 和 Worker 两种 Runtime
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><path d="M2.5 12.5H7l2-6.5 3.5 13 2.5-9 1.8 5.5h4.7"/></svg>'
    title: Event First
    details: 处理事件而非简单发送消息，支持路由、重试、去重、限流
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><path d="M4 5.5v4a3 3 0 0 0 3 3h9.5"/><path d="M4 18.5v-4"/><circle cx="19" cy="12.5" r="2"/><path d="m17.6 11.1 2.8-2.8"/><path d="M14 12.5h1.5"/></svg>'
    title: Rule Engine
    details: 表达式规则决定放行/抑制/改道：优先级、默认策略、shadow 观察、for/group_by/inhibit/silence/escalation
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><circle cx="8.5" cy="8.5" r="3"/><circle cx="16.5" cy="9.5" r="2.4"/><circle cx="12" cy="16.5" r="3"/><path d="m10.6 10.7 1.6 3.4"/><path d="m14.7 11.4-1.6 3"/></svg>'
    title: Notification Groups
    details: 命名受众，任何渠道位可写 group:&lt;id&gt;，投递时展开成员，API 热更新花名册
  - icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" width="24" height="24"><rect x="3.5" y="7" width="10" height="8" rx="1.5"/><rect x="10.5" y="12" width="10" height="7" rx="1.5"/><path d="M7 10.5h3"/><path d="M14 15h3"/><path d="M14 10.5h3.5"/></svg>'
    title: Worker Model
    details: 独立 Runtime 节点，支持浏览器自动化等复杂场景
---

## Herald 是什么

Herald 是一个**轻量、Provider 无关的统一订阅与投递中枢**：业务方只描述「发生了什么、通知什么、通知谁」，把通知作为一条 HTTP 请求交给它，由 Herald 负责受众解析、模板渲染、渠道路由与可靠送达；谁在什么渠道、以什么频率收到什么品类，由受众自己的订阅关系决定。内置 Provider 覆盖即时通讯（飞书、企业微信、钉钉、Slack、Discord、Telegram、微信）、短信（阿里云、腾讯云、网易）、邮件、Webhook、日志调试与 App 推送（FCM、APNs、极光 JPush、个推 Getui）；受众 - 渠道 - Provider 的解耦模型见 [受众领域模型总纲](/design-audience-model)。

去重、重试、限流、模板多渠道复用、规则引擎放行/抑制/改道，这些通知系统的公共部分在平台侧统一处理，业务方只关心「发什么、发给谁」。

## 使用场景

- **系统告警推送**：监控、巡检产生的事件经规则引擎分级路由，IM 群即时弹窗、短信兜底，超时未确认按升级策略自动加码。
- **运营消息触达**：模板一次定义、多渠道复用；命名受众（Notification Groups）维护花名册，运营公告一发即达飞书/企微/邮件。
- **验证码与事务短信**：对接阿里云/腾讯云/网易短信通道，内置限流防刷与失败重试，验证码这类高时效消息不丢单。
- **多通道兜底重试**：同一事件可路由到多个渠道，主通道故障自动重试、换道送达，指数退避直到成功或达到重试上限。
- **CI/CD 与自动化接入**：curl 一行接入，无 SDK 依赖；构建、部署、备份任务的结束状态直接进群，或经 Webhook 联动自建系统。

## 界面速览

管理台（Dashboard）的界面速览，自动轮播，也可用两侧箭头、下方圆点或键盘 ←/→ 翻看，点击任意一屏可看大图：

<script setup>
const showcaseSlides = [
  { image: '/screenshots/overview.png', title: '仪表盘', caption: '仪表盘：系统状态、通知计数与 Provider 健康一览' },
  { image: '/screenshots/providers.png', title: 'Providers', caption: 'Providers：渠道实例的启用、健康与配置入口' },
  { image: '/screenshots/rules.png', title: '通知规则', caption: '通知规则：优先级、生效/观察模式与影子命中统计' },
  { image: '/screenshots/groups.png', title: '通知群组', caption: '通知群组：命名受众与渠道收件人花名册' },
  { image: '/screenshots/workers.png', title: 'Workers', caption: 'Workers：本地/远端 Runtime 节点与心跳状态' },
  { image: '/screenshots/logs.png', title: '投递日志', caption: '投递日志：每条通知的渠道、级别、状态与耗时' },
  { image: '/screenshots/send.png', title: '发送消息', caption: '发送消息：控制台手工发一条通知调试链路' },
]
</script>

<ShowcaseCarousel :slides="showcaseSlides" />

以上截图来自本地实际运行的实例（内存队列 + 日志 Provider），数据为演示种子。

## 快速入口

- [快速开始](/guide/getting-started) —— 60 秒零凭据发出第一条通知
- [使用场景与接入](/guide/use-cases) —— 告警推送 / 运营触达 / 验证码短信 / 多通道兜底怎么接
- [Provider 总览](/providers/overview) —— 每个渠道：申请凭据 → 配置 → 第一条消息
- [排错指南](/guide/troubleshooting) —— 启动失败、消息没到、重试不生效
- [架构设计](/architecture/overview) —— Runtime、队列与投递模型
