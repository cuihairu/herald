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
      text: 架构设计
      link: /architecture/overview

features:
  - title: HTTP First
    details: curl 友好，无业务 SDK 依赖，天然支持多语言和自动化
  - title: Runtime First
    details: 承认 Provider 的本质差异来自运行环境，支持 Builtin 和 Worker 两种 Runtime
  - title: Event First
    details: 处理事件而非简单发送消息，支持路由、重试、去重、限流
  - title: Worker Model
    details: 独立 Runtime 节点，支持 Hook/GUI/DLL 等复杂场景
