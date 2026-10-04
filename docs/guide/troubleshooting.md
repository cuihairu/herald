# 排错指南

按「卡在哪一步」排查：先看启动，再看发送，最后看送达。

## 启动就失败

| 现象 / 日志 | 原因与处理 |
|-------------|-----------|
| `Unknown command: --config` | 启动命令少了子命令。正确写法：`heraldd serve --config config.yaml`（或 `heraldd worker --config ...`），`serve` 不能省 |
| `factory not found: builtin` | provider 的 `type` 写了 `builtin`。`type` 必须是具体类型名：`type: telegram`、`type: aliyunsms`… 完整列表见 [Provider 手册](/providers/overview) |
| `fcm: project_id is required` / `telegram: token is required` 等 | 必填配置缺失，错误前缀就是 provider 名。对照该 provider 文档的配置项表 |
| `unknown provider type` / `no-such` | `type` 拼写错误，对照 Provider 手册 |
| `unknown queue type: banana` | `queue.type` 只支持 `memory`（默认）/ `redis`（远程 worker 不接受 memory） |
| `auth enabled but no api_keys configured` | 开了 `auth.enabled` 却没配 `auth.api_keys` |
| `address already in use` | `server.addr`（默认 `:8080`）或 `websocket.addr`（默认 `:8081`）被占用，改端口或停掉占用进程 |
| `invalid config` | YAML 语法错误，错误信息会带行号/字段 |

启动成功的标志是日志里出现 `herald scheduler started`。

## 环境变量没生效

配置里写 `$VAR` 但 provider 收到的还是字面量：

- **只支持 `$VAR` 写法**（值以 `$` 开头整体替换）；`${VAR}` **不会**被展开，会原样保留
- 环境变量必须在**启动 heraldd 的进程环境**里存在（`export` 或 `.env` 由部署方式注入），不是前端或 curl 的环境
- 配置文件是**启动时读取**的，改完要重启服务

```yaml
# 对
token: "$TELEGRAM_BOT_TOKEN"
# 错（不会展开）
token: "${TELEGRAM_BOT_TOKEN}"
```

## 发送失败（notify 调用就报错）

| 现象 | 原因与处理 |
|------|-----------|
| `no route found for type=xxx level=xxx` | 请求没带 `channels`，且 `routes`（按 type）和 `level_routes`（按 level）都没命中。补路由表，或请求里显式带 `channels` |
| HTTP 422 + `data.failed` 列出通道 | 通知受理但某个通道投递失败，错误详情在 `failed[].error`；确定性错误不会重试，可重试错误会按退避策略自动重投 |
| `provider not found` | `channels` 里写了未注册的实例名（配置里没有这个 provider 名，或它 `enabled: false`） |
| 请求 401 | 启用了 `auth.enabled`，`notify` 接口需要带 API Key（见[配置参考](/guide/configuration)认证一节） |

**判定「受理」与「送达」是两件事**：`{"code":0,...}` 只代表进入投递管线，最终每个通道的成败看 `data.accepted` / `data.failed` 和投递日志：

```bash
curl "http://127.0.0.1:8080/api/v1/logs?limit=20"   # status 字段：success / failed / pending / shadow
```

## 消息没到（受理成功但没收到）

1. **查投递日志**：`curl "http://127.0.0.1:8080/api/v1/logs?limit=20"`，看该任务的 `status` 与错误信息
2. **查 worker**：`queue.workers` 是否为 0、worker 是否起了（启动日志有 `worker pool started`）
3. **查路由**：请求带的 `channels` 是否真是配置里的实例名（`curl /api/v1/providers` 可列出）
4. **查去重**：`dedup.enabled` 窗口内重复通知会被折叠（这是预期行为），换个 `type` 或关掉 dedup 验证
5. **查通道侧错误**：按 provider 查各自的常见错误表——

| 通道 | 常见错误表 |
|------|-----------|
| Telegram | [telegram](/providers/telegram#常见错误) |
| 飞书 | [feishu](/providers/feishu#常见错误) |
| 企业微信 | [wecom](/providers/wecom#常见错误) |
| 钉钉 | [dingtalk](/providers/dingtalk#常见错误) |
| Slack / Discord | [slack](/providers/slack#常见错误) · [discord](/providers/discord#常见错误) |
| 微信系 | [wechat](/providers/wechat) · [wechatmp](/providers/wechatmp) |
| 邮件 | [email](/providers/email#常见错误) |
| 短信 | [aliyunsms](/providers/aliyunsms) · [tencentsms](/providers/tencentsms) · [neteasesms](/providers/neteasesms) |
| Webhook / Log | [webhook](/providers/webhook#常见错误) · [log](/providers/log) |

## 重试没生效

- 只有**可重试错误**才重试：请求超时、HTTP 408/429/5xx、网络不通。凭据错误、token 无效、号码不存在这类 4xx 是确定性错误，重试不可能成功，直接记失败
- 重试次数与间隔看 `retry` 配置（`max` / `backoff` / `initial_delay` / `max_delay`），见[配置参考](/guide/configuration)
- 邮件（SMTP 直连）不走 HTTP 重试语义，失败即返回

## 远程 Worker 问题

- `heraldd worker` 拒绝 `queue.type: memory`：远程 worker 必须用共享队列（redis）
- worker 连不上调度中心：检查 `worker.server_url`（WebSocket 地址）与调度中心 `websocket.addr` 是否互通
- worker 在但投递没走它：provider 是配置在**调度中心**还是 **worker 本机**，见 [Worker Runtime](/runtime/worker)

## 还是不行

1. 开 debug：把 provider `enabled: false` 时不会被选中；用 `log` provider 旁路验证链路本身（见[快速开始](/guide/getting-started)第 1 步）
2. 查日志与运行时状态：`heraldd` 的结构化日志 + `curl /api/v1/status`
3. 提 issue：带上 config（脱敏）、`/api/v1/logs` 的相关条目、heraldd 版本
