# 目录结构

## 项目结构

```
herald/
├── cmd/
│   └── heraldd/              # 主服务入口
│
├── core/                     # 核心模块
│   ├── types.go              # 核心类型定义
│   ├── provider.go           # Provider 接口
│   ├── queue/                # 事件队列
│   │   ├── queue.go          # 队列接口
│   │   ├── memory.go         # 内存队列实现
│   │   └── factory.go        # 队列工厂
│   ├── route/                # 路由引擎
│   ├── retry/                # 重试机制
│   ├── dedup/                # 去重机制
│   ├── runtime/              # Runtime 管理
│   └── limiter/              # 限流机制（待实现）
│
├── api/                      # HTTP API
│   ├── handler.go            # 请求处理器
│   └── server.go             # HTTP 服务器
│
├── protocol/                 # 内部协议定义
│   └── message.go            # 消息类型
│
├── providers/                # Provider 实现
│   └── builtin/              # 内置 Runtime
│       ├── registry/         # Provider 注册
│       ├── log/              # 日志 Provider
│       ├── telegram/         # Telegram Provider
│       ├── feishu/           # 飞书 Provider
│       └── wecom/            # 企业微信 Provider
│
├── worker-sdk/               # Worker SDK
│   ├── go/                   # Go SDK
│   │   ├── client.go         # 客户端实现
│   │   └── example/          # 示例代码
│   ├── cpp/                  # C++ SDK（计划中）
│   ├── python/               # Python SDK（计划中）
│   └── rust/                 # Rust SDK（计划中）
│
├── internal/                 # 内部包
│   ├── config/               # 配置管理
│   └── logger/               # 日志管理
│
├── docs/                     # 文档
│   └── .vitepress/           # VitePress 配置
│
├── bin/                      # 编译输出
├── config.yaml               # 配置文件
├── Makefile                  # 构建脚本
└── README.md                 # 项目说明
```

## Provider 配置示例

```yaml
providers:
  # Builtin Providers
  log:
    type: builtin
    config:
      name: "log"

  telegram:
    type: builtin
    config:
      token: "${TELEGRAM_BOT_TOKEN}"
      chat_id: "${TELEGRAM_CHAT_ID}"

  feishu:
    type: builtin
    config:
      webhook_url: "${FEISHU_WEBHOOK_URL}"

  wecom:
    type: builtin
    config:
      webhook_url: "${WECOM_WEBHOOK_URL}"

  # Worker Providers（计划中）
  wechat:
    type: worker
    platform: windows
```
