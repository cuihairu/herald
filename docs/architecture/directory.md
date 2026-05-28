# 目录结构

## 项目结构

```
herald/
├── cmd/
│   └── heraldd/              # 主服务入口
│
├── core/                     # 核心模块
│   ├── types.go              # 核心类型（Notification, DeliveryTask, Queue）
│   ├── provider.go           # Provider/CapableProvider 接口
│   ├── dispatch/             # 调度器（队列消费 + Runtime 分发）
│   │   └── dispatcher.go     # Dispatcher 实现
│   ├── queue/                # 任务队列
│   │   ├── memory.go         # 内存队列实现
│   │   └── factory.go        # 队列工厂
│   ├── route/                # 路由引擎
│   ├── retry/                # 重试机制
│   ├── dedup/                # 去重机制（基于内容稳定 key）
│   ├── runtime/              # Runtime 管理（Provider 注册 + 投递 + 重试）
│   │   └── manager.go        # Runtime Manager 实现
│   ├── service/              # 服务层
│   │   ├── notification.go   # NotificationService 编排层
│   │   └── planner.go        # DeliveryPlanner（Binding + Renderer）
│   ├── template/             # 模板系统
│   │   ├── types.go          # Template/Binding/RenderedData 类型
│   │   ├── engine.go         # Go template 引擎
│   │   ├── renderer.go       # 内容渲染器（HTML/Markdown/Plain/JSON）
│   │   └── manager.go        # 模板管理（CRUD + 渲染）
│   ├── auth/                 # 认证
│   ├── logstore/             # 投递日志存储
│   ├── websocket/            # WebSocket（Worker 连接）
│   │   ├── server.go         # WebSocket 服务器
│   │   └── hub.go            # Worker 注册与任务分发
│   └── limiter/              # 限流机制
│
├── api/                      # HTTP API
│   ├── handler.go            # 请求处理器
│   └── server.go             # HTTP 服务器 + 路由 + 消费循环
│
├── protocol/                 # 内部协议定义
│   └── message.go            # WebSocket 消息类型
│
├── providers/                # Provider 实现
│   └── builtin/              # 内置 Provider
│       ├── registry/         # Provider 注册
│       ├── log/              # 日志 Provider
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
│   └── .vitepress/           # VitePress 配置
│
├── config.yaml               # 配置文件
├── Makefile                  # 构建脚本
└── README.md
```

## 数据流

```
API Request → Handler → NotificationService → DeliveryPlanner → Queue → Dispatcher → Runtime → Provider
                          │                      │
                          ├─ Template 渲染       ├─ Builtin 投递
                          ├─ Dedup 去重          └─ Worker 分发
                          └─ Route 路由
```

## Provider 配置示例

```yaml
providers:
  log:
    type: log
    enabled: true
    config:
      name: "log"

  telegram:
    type: telegram
    enabled: true
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"

  feishu:
    type: feishu
    enabled: false
    config:
      webhook_url: "${FEISHU_WEBHOOK_URL}"

  wechatmp:
    type: worker
    enabled: true
    config:
      target: "wechat-worker-01"

  email:
    type: email
    enabled: true
    config:
      host: "smtp.example.com"
      port: 587
      username: "${EMAIL_USER}"
      password: "${EMAIL_PASS}"
      from: "notify@example.com"
```

## 模板配置示例

```yaml
templates:
  server_alert:
    name: "服务器告警"
    title: "服务器 {{.host}} 告警"
    level: error
    fields:
      - { label: "主机", value: "{{.host}}" }
      - { label: "状态", value: "{{.status}}" }
    bindings:
      email:        { format: html }
      telegram:     { format: markdown }
      aliyunsms:
        template_code: "SMS_123456"
        params: { "主机": "host", "状态": "status" }
      tencentsms:
        template_id: "789"
        param_order: ["主机", "状态"]
```
