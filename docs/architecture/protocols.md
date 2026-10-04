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

### 实际实现

线上跑的线格式是 **WebSocket + JSON**：传输用 `gorilla/websocket`（go.mod 依赖），消息体由 `protocol.MarshalMessage` 编码——`Message{Type, Payload}` 序列化后注入 type 字段。选型时曾对比过下表，protobuf / Raw TCP 两条都未实现：

| 协议                       | 选型结论 | 实现状态 |
| ------------------------ | --- | --- |
| WebSocket + JSON | 采用 | 已实现（`core/websocket` + `protocol/message.go`） |
| WebSocket + protobuf | 备选 | 未实现 |
| Raw TCP + protobuf   | 备选 | 未实现 |
| gRPC                 | 不采用 | — |

### 为什么不使用 gRPC

- C++ 体积巨大
- Windows 编译复杂
- Debug PDB 爆炸
- 不适合 Runtime Node
- 不适合事件流

Herald 更像 **Session System**，而不是 **RPC Framework**。
