# 协议设计

## 外部协议

### HTTP REST + JSON

这是 **唯一公开协议**。

**原因：**

- curl
- shell
- automation
- CI/CD
- webhook
- 任意语言兼容

## 为什么没有业务 SDK

因为：

> notify API 太简单

例如：

```bash
curl /notify
```

已经足够。业务 SDK 的收益很低。

## 内部协议

Herald 内部使用 **Persistent Session Protocol**，用于：

- Worker Runtime
- Runtime Dispatch
- Heartbeat
- Reconnect
- Streaming

### 推荐协议

| 协议                       | 推荐度 |
| ------------------------ | --- |
| WebSocket + protobuf | 高   |
| Raw TCP + protobuf   | 高   |
| gRPC                 | 不推荐 |

### 为什么不使用 gRPC

- C++ 体积巨大
- Windows 编译复杂
- Debug PDB 爆炸
- 不适合 Runtime Node
- 不适合事件流

Herald 更像 **Session System**，而不是 **RPC Framework**。
