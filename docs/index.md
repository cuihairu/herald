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
  - title: HTTP First
    details: curl 友好，无业务 SDK 依赖，天然支持多语言和自动化
  - title: Runtime First
    details: 承认 Provider 的本质差异来自运行环境，支持 Builtin 和 Worker 两种 Runtime
  - title: Event First
    details: 处理事件而非简单发送消息，支持路由、重试、去重、限流
  - title: Rule Engine
    details: 表达式规则决定放行/抑制/改道：优先级、默认策略、shadow 观察、for/group_by/inhibit/silence/escalation
  - title: Notification Groups
    details: 命名受众，任何渠道位可写 group:&lt;id&gt;，投递时展开成员，API 热更新花名册
  - title: Worker Model
    details: 独立 Runtime 节点，支持浏览器自动化等复杂场景
