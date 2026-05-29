# 目录结构

## 项目结构

```
herald/
├── cmd/
│   └── heraldd/              # 统一入口（serve / worker 子命令）
│
├── core/                     # 核心模块
│   ├── types.go              # 核心类型（Notification, DeliveryTask, Queue）
│   ├── provider.go           # Provider/CapableProvider 接口
│   ├── worker/               # 统一 Worker 架构
│   │   ├── pool.go           # Worker Pool（管理 local worker goroutine）
│   │   └── registry.go       # Worker 注册表（local + remote）
│   ├── dispatch/             # 调度器（Worker Pool 的薄封装）
│   │   └── dispatcher.go
│   ├── queue/                # 任务队列
│   │   ├── memory.go         # 内存队列实现
│   │   └── factory.go        # 队列工厂
│   ├── route/                # 路由引擎
│   ├── retry/                # 重试机制
│   ├── dedup/                # 去重机制
│   ├── runtime/              # Runtime 管理（Provider 注册 + 投递 + 重试）
│   │   └── manager.go
│   ├── service/              # 服务层
│   │   ├── notification.go   # NotificationService 编排层
│   │   └── planner.go        # DeliveryPlanner
│   ├── template/             # 模板系统
│   │   ├── types.go
│   │   ├── engine.go
│   │   ├── renderer.go
│   │   └── manager.go
│   ├── auth/                 # 认证
│   ├── logstore/             # 投递日志存储
│   ├── websocket/            # WebSocket（远程 Worker 管理通道）
│   │   ├── server.go         # WebSocket 服务器
│   │   └── hub.go            # Worker 注册/心跳管理
│   └── limiter/              # 限流机制
│
├── api/                      # HTTP API
│   ├── handler.go            # 请求处理器
│   └── server.go             # HTTP 服务器
│
├── protocol/                 # 内部协议定义
│   └── message.go            # WebSocket 消息类型
│
├── providers/                # Provider 实现
│   └── builtin/              # 内置 Provider
│       ├── registry/         # Provider 注册
│       ├── log/              # 日志
│       ├── telegram/         # Telegram
│       ├── feishu/           # 飞书
│       ├── wecom/            # 企业微信
│       ├── dingtalk/         # 钉钉
│       ├── slack/            # Slack
│       ├── discord/          # Discord
│       ├── email/            # 邮件
│       ├── webhook/          # Webhook
│       ├── aliyunsms/        # 阿里云 SMS
│       ├── tencentsms/       # 腾讯云 SMS
│       ├── neteasesms/       # 网易云 SMS
│       ├── wechat/           # 微信（Server酱）
│       ├── wechatmp/         # 微信公众号
│       └── worker/           # Worker Provider（代理远程 Worker）
│
├── worker-sdk/               # Worker SDK
│   └── go/                   # Go SDK + 示例
│
├── internal/                 # 内部包
│   ├── config/               # 配置管理
│   └── logger/               # 日志管理
│
├── docs/                     # 文档
├── config.yaml               # 配置文件
├── Makefile                  # 构建脚本
└── README.md
```

## 数据流

```
API Request → Handler → NotificationService → DeliveryPlanner → Queue → Worker Pool → Provider
                          │                      │                        │
                          ├─ Template 渲染       ├─ Binding 解析          ├─ local: 直接调用
                          ├─ Dedup 去重          └─ SMS 参数适配          └─ remote: Queue 消费
                          └─ Route 路由
```

## 配置示例

```yaml
# 调度器模式
queue:
  type: memory          # memory | redis
  workers: 0            # 0 = CPU*2+1

# 分布式模式
queue:
  type: redis
  workers: 4
  redis:
    addr: "localhost:6379"
    stream: "herald:tasks"
    group: "herald-workers"
```
