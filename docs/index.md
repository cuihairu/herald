---
layout: home

hero:
  name: Herald
  text: Event-driven Delivery Infrastructure
  tagline: 统一事件 · 统一 Runtime · 统一调度 · 统一投递
  actions:
    - theme: brand
      text: 快速开始
      link: /guide/getting-started
    - theme: alt
      text: 作为 Go 库使用
      link: /library-usage
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
