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
| 响应体 `data.failed` 列出通道 | 通知受理但某个通道投递失败，错误详情在 `failed[].error`（**HTTP 状态始终 200**：部分失败 body `code` 仍为 0，全部失败才把 body `code` 写成 422）；确定性错误不会重试，可重试错误会按退避策略自动重投 |
| `provider not found` | `channels` 里写了未注册的实例名（配置里没有这个 provider 名，或它 `enabled: false`） |
| 请求 401 | 启用了 `auth.enabled`，`notify` 接口需要带 API Key（见[配置参考](/guide/configuration)认证一节） |

**判定「受理」与「送达」是两件事**：`{"code":0,...}` 只代表进入投递管线，最终每个通道的成败看 `data.accepted` / `data.failed` 和投递日志：

```bash
curl "http://127.0.0.1:8080/api/v1/logs?limit=20"   # status 字段：success / failed / pending / shadow
```

`pending` 还包括正在等重试的任务：可重试失败会带 `next_retry_at` 回队列到点重投（`/api/v1/logs` 该任务只有一行，`pending` 直到终态才翻 `success`/`failed`）。

## 消息没到（受理成功但没收到）

1. **查投递日志**：`curl "http://127.0.0.1:8080/api/v1/logs?limit=20"`，看该任务的 `status` 与错误信息
2. **查 worker**：`queue.workers` 是否为 0、worker 是否起了（启动日志有 `worker pool started`）
3. **查路由**：请求带的 `channels` 是否真是配置里的实例名（`curl /api/v1/providers` 可列出）
4. **查去重**：`dedup.enabled` 窗口内重复通知会被折叠（这是预期行为），换个 `type` 或关掉 dedup 验证
5. **查通道侧错误**：按 provider 查各自的常见错误表：

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

1. 旁路验证：把可疑 provider `enabled: false`，它就不会被选中；再用 `log` provider 验证链路本身（见[快速开始](/guide/getting-started)第 1 步）
2. 查日志与运行时状态：`heraldd` 的结构化日志 + `curl /api/v1/status`
3. 提 issue：带上 config（脱敏）、`/api/v1/logs` 的相关条目、heraldd 版本

## API 报错速查

响应形态先分清两类：**业务结果失败**（HTTP 200，错误码写在 body 的 `code` 里，notify 受理部分/全部失败、app 面 suppressed/refused 都属这类）与**传输层错误**（HTTP 状态码即 body 的 `code`）。

| 现象（实测） | 原因 | 处理 |
| --- | --- | --- |
| HTTP 401 `{"code": 401, "message": "unauthorized"}`（注意有空格，手写信封） | 未带凭证或凭证无效 | 管理面补 `Authorization: Bearer` / `X-API-Key`；app 面核对播种 token |
| HTTP 401 `invalid username or password` | 登录口令错 | 对照 `auth.admin_user` 配置 |
| HTTP 401 `invalid app credentials` | app token 错、app 名错、未带头 | 用 `GET /api/v1/apps/{app}` 自检，先排除这两类 |
| HTTP 403 `token lacks trigger scope` | token 权限集不够 | 换对应权限 token，或播种时补 scope |
| HTTP 404 纯文本 `404 page not found`（app 面） | app 未在配置播种 | 让有配置权的人播种 app（整面关闭） |
| HTTP 400 `invalid request` | 请求体非法 JSON | 检查 JSON 语法与 Content-Type |
| HTTP 404 `log not found` / `provider not found: x` / `rule not found: x` | 资源不存在 | 用列表端点（`/logs`、`/providers`、`/rules`）确认名字 |
| HTTP 405 `method not allowed`（纯文本） | 端点不支持该方法（`templates/{id}` 例外，返回 JSON） | 对照各端点允许的方法表 |
| HTTP 422 `no route: no route found for type=... level=...` | 没带 channels 且路由表没命中 | 请求带 `channels`，或补 `routes`/`level_routes` |
| HTTP 422（body `code:422`）`all channels failed: [...]` | 全部渠道投递失败 | 看 `data.failed[].Error` 逐渠道定位（键首字母大写） |
| HTTP 200 `code:0` 但 `data.accepted: null` + `task_ids: null` | 通知被去重闸折叠（窗口内同内容重复） | 预期行为；换个 type 或等 5 分钟窗口 |
| HTTP 200 `suppressed: true` | app dispatch 同 dedup_key 窗口内重复 | 预期行为；确需重发换 dedup_key |
| HTTP 200 含 `refused` 列表 | 受众没有出路（关系/强度/联系面缺一） | 按 `reason` 绑定：`intensity_exceeded` 换高紧急度品类，`phone_disabled` 查手机闸门，`no_channels` 查联系面，`filtered` 查渠道×关系矩阵 |
| HTTP 409 `rule already exists: x` / `group already exists: x` / `category already registered with a different default urgency` | 资源已存在（规则/组/品类默认紧急度冲突） | 规则走 PUT 替换；组走 PUT；品类换名或改回原默认值 |
| HTTP 503 `rules engine is not configured` / `ack store is not configured` / `audit trail not configured` 等 | 对应组件未在配置启用 | 补对应配置块后重启 |
| app 回调收不到 | 回调面未配置 / 非 2xx / secret 不匹配 | `GET /apps/{app}/callback` 看 url/has_secret；签名校验用 PUT 的 secret 重算 HMAC |
