# 协议设计

## 外部协议

### HTTP REST + JSON

这是 **唯一公开协议**。curl、shell 脚本、CI/CD、监控系统的 webhook、任何语言，都直接调 REST 接口，不需要额外的客户端约定。

## 关于业务 SDK

线协议始终只有 REST：notify API 一个 POST 就能调，

```bash
curl /notify
```

任何语言都够用。在此之上，仓库随批次 11 落地了两枚**便利层** Go SDK（不改线协议）：`apps-sdk/go`（集成方 app 命名空间接入）与 `worker-sdk/go`（远程 Worker 侧对接）。

## 内部协议

Herald 内部使用 **Persistent Session Protocol**，承载 Worker 注册、任务分发、心跳、重连与事件流五类交互。

### 实际实现

线上跑的线格式是 **WebSocket + JSON**。传输用 `gorilla/websocket`（go.mod 依赖），消息体由 `protocol.MarshalMessage` 编码，`Message{Type, Payload}` 序列化后注入 type 字段。选型时曾对比过下表，protobuf / Raw TCP 两条都未实现：

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
