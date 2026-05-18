# Runtime 概述

## Provider Runtime Architecture

这是 Herald 的核心。

## Runtime Type 1：Builtin

直接运行在 Core 内。

### 适合

- Telegram
- Discord
- 飞书
- 企业微信 Bot
- Email
- Webhook

### 特点

- 轻量
- 无 IPC
- 高性能
- 简单

### 接口

```go
type BuiltinProvider interface {
    Deliver(ctx context.Context, task *Task) error
}
```

## Runtime Type 2：Worker

独立 Runtime 节点。

### 适合

- 微信 Hook
- QQ Hook
- GUI 自动化
- Outlook COM
- DLL Injection

### 模型

```
Core
  ↕ persistent session
Worker Runtime
```

### 特点

- 崩溃隔离
- 独立权限
- 特殊 OS 环境
- GUI Session
- DLL 支持
