# Log

`log` Builtin Provider 把通知直接写入进程日志，**不做任何外部投递**。用于本地开发、冒烟测试和 CI 里验证路由与派发链路。

## 配置项

| 键 | 必填 | 说明 | 默认值 |
|----|------|------|--------|
| `name` | ❌ | Provider 实例名（显示在日志与状态接口里） | `log` |

创建永不失败；投递除 task 为 nil 外恒成功。

## 配置示例

```yaml
providers:
  log:
    type: log
    enabled: true
    config:
      name: "log"          # 可选，默认 "log"
```

## 输出形态

每条通知同时产生两行标准输出：

1. 结构化 JSON 日志（`log/slog` JSONHandler，可直接进采集器；通知的 `level` 以同名字段附在 slog 自身的 `level` 之后）：

```json
{"time":"2026-09-28T10:00:00+08:00","level":"INFO","msg":"delivering task","provider":"log","task_id":"…","title":"磁盘告警","body":"/data 92%","level":"error"}
```

2. 一行人类可读文本：

```
[error] 磁盘告警: /data 92%
```

## 典型用法

- **开发联调**：把路由 `channels` 指向 `log`，先验证通知构造、模板渲染、规则决策是否正确，再切真实渠道
- **CI 冒烟**：配置里只启用 `log`，启动即可端到端跑通 API → 队列 → 派发全链路而不发任何外部请求
- **影子对照**：与真实 Provider 并存，观察派发行为不影响线上接收者

## 下一步

- [Provider 概览](./overview.md) - 查看所有 Provider 与启用方式
- [快速开始](/guide/getting-started) - 用 log provider 跑通第一个通知
- [Telegram](./telegram.md) - 接入第一个真实外部渠道
