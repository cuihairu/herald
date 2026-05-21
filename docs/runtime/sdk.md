# Worker SDK

虽然业务 SDK 不重要，但 **Worker SDK 非常重要**。

## 为什么需要 Worker SDK

- 自动处理连接和重连
- 心跳管理
- 任务分发和确认
- 事件上报
- 状态管理

## Worker SDK 职责

- register - 向 Core 注册
- heartbeat - 定时心跳
- reconnect - 断线重连
- dispatch - 接收任务
- ack - 确认任务完成
- stream - 消息流处理

## Go SDK 使用示例

### 基本使用

```go
import (
    "context"
    workersdk "github.com/cuihairu/herald/worker-sdk/go"
    "github.com/cuihairu/herald/protocol"
)

func main() {
    // 创建配置
    config := &protocol.WorkerConfig{
        WorkerID:         "my-worker-001",
        CoreURL:          "ws://localhost:8080/worker",
        ReconnectDelay:   5 * time.Second,
        HeartbeatInterval: 30 * time.Second,
        Capabilities:     []string{"wechat"},
    }

    // 创建客户端
    client := workersdk.NewClient(config)

    // 设置任务处理器
    client.OnTask(func(task *protocol.DispatchMessage) error {
        // 处理任务
        err := processTask(task)

        // 确认任务
        client.Ack(task.TaskID, err == nil, "")

        return err
    })

    // 设置事件处理器
    client.OnEvent(func(event *protocol.EventMessage) {
        fmt.Printf("Received event: %s\n", event.EventType)
    })

    // 设置回调
    client.OnConnect(func() {
        fmt.Println("Connected to Herald core")
    })

    client.OnDisconnect(func(err error) {
        fmt.Printf("Disconnected: %v\n", err)
    })

    client.OnStateChange(func(state protocol.ConnectionState) {
        fmt.Printf("State: %s\n", state)
    })

    // 连接
    ctx := context.Background()
    if err := client.Connect(ctx); err != nil {
        panic(err)
    }

    // 等待退出
    select {}
}
```

### 任务处理

```go
client.OnTask(func(task *protocol.DispatchMessage) error {
    // 任务信息
    taskID := task.TaskID
    provider := task.Provider
    title := task.Title
    body := task.Body
    level := task.Level
    target := task.Target
    data := task.Data

    // 处理任务...
    success := doSomething(task)

    // 确认
    client.Ack(taskID, success, "")

    return nil
})
```

### 发送事件

```go
// 上报 worker 状态
client.SendEvent("status", map[string]interface{}{
    "online": true,
    "contacts": 150,
})

// 上报错误
client.SendEvent("error", map[string]interface{}{
    "message": "connection timeout",
    "code": "CONNECTION_TIMEOUT",
})
```

## SDK 选择指南

| 语言     | 用途           | 状态    |
| ------ | ------------ | ----- |
| Go     | 普通 Worker     | ✅ 完成  |
| C++    | Native Addon | 计划中   |
| Python | Automation   | 计划中   |
| Rust   | Native Worker | 计划中   |

## 连接状态

```
Disconnected → Connecting → Connected → Registered → Ready
     ↓              ↓              ↓            ↓
   (error)       (error)        (error)     (error)
```

| 状态            | 说明     |
| ------------- | ------ |
| Disconnected  | 未连接    |
| Connecting    | 连接中    |
| Connected     | 已连接    |
| Registered    | 已注册    |
| Ready         | 就绪，可接收 |

## 协议消息

### 注册消息

```json
{
  "type": "register",
  "worker_id": "wechat-node-01",
  "platform": "windows",
  "version": "1.0.0",
  "capabilities": ["wechat"]
}
```

### 心跳消息

```json
{
  "type": "heartbeat",
  "worker_id": "wechat-node-01",
  "timestamp": 1716000000,
  "status": {
    "contacts": 150,
    "memory": 100
  }
}
```

### 分发消息

```json
{
  "type": "dispatch",
  "task_id": "task-123",
  "provider": "wechat",
  "title": "Node Offline",
  "body": "node-17 is offline",
  "level": "error",
  "target": "group_xxx",
  "timestamp": 1716000000
}
```

### 确认消息

```json
{
  "type": "ack",
  "task_id": "task-123",
  "success": true,
  "timestamp": 1716000001
}
```

### 事件消息

```json
{
  "type": "event",
  "worker_id": "wechat-node-01",
  "event_type": "offline",
  "timestamp": 1716000002
}
```
