# 简介

## 什么是 Herald

Herald 是一个 **事件驱动的通知投递基础设施**（Event-driven Delivery Infrastructure）。

## 核心特性

- **HTTP First** - curl 友好，无 SDK 依赖
- **Runtime First** - 支持 Builtin 和 Worker 两种 Runtime
- **Event First** - 处理事件而非简单发送消息
- **Worker Model** - 支持复杂场景如 Hook/GUI/DLL

## 核心流程

```
Event → Route → Dispatch → Delivery Runtime
```

## 典型使用场景

- 监控告警聚合
- CI/CD 通知
- 事件驱动通知
- 多平台消息投递

## 与其他方案的区别

| 特性    | Herald    | Webhook 工具 | 消息推送 SDK |
| ----- | --------- | -------- | ------- |
| 事件驱动  | ✅         | ❌        | ❌       |
| 多平台聚合 | ✅         | 部分       | ❌       |
| Hook 支持 | ✅         | ❌        | ❌       |
| SDK 依赖 | ❌         | 部分       | ✅       |
| HTTP API | ✅         | ✅        | 部分       |
