# Herald 代码深审报告（安全静态扫描 + 隐藏 bug 猎捕 + 质量门）

- 日期：2026-11-02
- 范围：本仓全量 Go 面——`cmd/heraldd`、`api/`、`core/`（31 包）、`config/`、`protocol/`、`providers/builtin/`（18 渠道 + worker + registry）、`apps-sdk/go`、`worker-sdk/go`、`examples/quickstart`、根包；生产代码 28,519 行。Dashboard 前端不在本轮（2026-10-31 已有 UI 审计报告）。
- 方法：①安全静态扫描（硬编码密钥 / shell 注入 / SQL 拼接 / eval / 反序列化 / 路径穿越 / TLS 校验 / SSRF / 认证旁路 / 请求体上限）→ ②隐藏 bug 猎捕（错误处理缺失 / 竞态 / 越界 / 资源泄漏 / off-by-one / 并发）→ ③质量门（gofmt / go vet / go build / go test / go test -race / golangci-lint）。
- 工具链：go1.26.6（`go.mod` 钉定，与 CI `go-version: '1.26'` 一致）；golangci-lint v2.14.0。本地须 `GOTOOLCHAIN=go1.26.6` 运行，否则 `auto` 会选 go1.27 与 v2.14.0 二进制错配而 panic（本地工具链问题，非代码问题）。
- 处置规则：P0/P1 本轮直接修复（一问题一提交，测试全绿后推送）；P2/P3 记入 `todo.md`。

## 结论一览

| 严重度 | 数量 | 明细 |
| --- | --- | --- |
| P0 | 2 | F-1 出厂配置 API key 方向反了；F-2 JWT 硬编码兜底密钥 |
| P1 | 2 | F-3 飞书卡片明文回调绕过加密凭据；F-4 worker WebSocket 帧无上限 |
| P2 | 4 | F-5 `/worker` 管理通道无认证；F-6 登录无爆破限速；F-7 飞书回调 CBC 无 MAC；F-8 回调 URL 未拒私网（上轮结转） |
| P3 | 6 | F-9 Redis `Nack` nil 格式化；F-10 绑定 token 过期不清理；F-11 秘密比较非常量时间；F-12 `herald_token` cookie 无 CSRF 上下文；F-13 `Default()` 内置 `admin:admin`；F-14 `.env.example` 占位密钥 |

**发现总数 14；本轮已修 4（P0×2、P1×2）；剩余 10（P2×4、P3×6，均已入 `todo.md`）。**

## 发现明细（按严重度排序）

### F-1 · P0 · 安全 · 出厂配置把 API key 方向写反，字面量 `operator` 成为真实密钥

- 证据（修复前）：`config.yaml:8` 写作 `operator: "$HERALD_ADMIN_API_KEY"`。但 `auth.APIKeys` 的类型是 `map[string]string`，语义是 **key → description**（`config/config.go:195`；`core/auth/auth.go:115-119` 遍历 map 的 key 做比较）。因此真正的密钥是字面量 `operator`，`$HERALD_ADMIN_API_KEY` 只是描述；同时 `ExpandEnv()`（修复前 `config/config.go:519-527`）只展开 provider 自由配置，从不展开 `auth.api_keys` / `auth.secret_key` / `admin_user` / app token / `card_callback.encrypt_key` / source secret，环境变量永远读不到。
- 影响：任何拿到仓库示例配置的部署，管理面 API key 都是公开的 `operator`。这比“环境变量未生效”更严重——不是拒绝服务，而是静默接受一个公开密钥。上轮报告（`docs/review/2026-11-01-code-review.md:45`）把“config.yaml 全部 `$VAR` 引用”判为干净，正是因为只看了字符串形态、没有核对 map 方向，属一次假阴性；本轮明确记录。
- 修复：`1a66791`。`config.yaml:16` 改为 `"$HERALD_ADMIN_API_KEY": "operator"`；`ExpandEnv()` 覆盖 api_keys 的 key 与 description、`auth.secret_key`、`admin_user` 密码、app token、`card_callback.encrypt_key`、bot/WeChat source secret、Redis/RulesState 密码（`config/config.go:523-566`）。`Validate()` 对仍未解析的 `$VAR` 直接拒绝启动（`config/config.go:608-621,660-679`），把“配置错误”从静默弱口令改成 fail-closed。
- 测试：`config/config_test.go` 新增 `TestExpandEnvSecrets`、`TestShippedConfigAuthOrientation`（直接加载出厂 `config.yaml`，断言 map key 是环境变量值、description 是 `operator`）、`TestValidateRejectsUnresolvedSecrets`。
- 文档：`README.md`、`README.zh.md`、`.env.example`、`docs/guide/configuration.md`、`docs/zh/guide/configuration.md` 同步正确方向。

### F-2 · P0 · 安全 · JWT 未配置密钥时回退到源码可见常量，可伪造任意身份令牌

- 证据（修复前）：`core/jwt/jwt.go:34` 在 `secretKey == ""` 时赋值 `"herald-default-secret-key-change-in-production"`。`NewManager("")` 是 `auth.New` 的默认路径（未配置 `auth.secret_key` 时），而 `authenticateJWT`（`core/auth/auth.go:127-133`）只验签不查吊销。任何读过源码的人都能用该常量签出 `role=admin` 的 token，通过 `auth.Middleware`。
- 影响：未显式配置 `auth.secret_key` 的部署，管理面 JWT 认证完全可伪造；且该常量同时出现在仓库与历史制品中，无法靠“换掉部署配置”补救已泄露的信任根。
- 修复：`e31aad7`。空密钥改为 `crypto/rand` 生成 32 字节、base64url 编码的 **每进程随机密钥**（`core/jwt/jwt.go:38-50`）；熵源失败时记录 `initErr`，`Generate`/`Validate` 双双 fail-closed（`core/jwt/jwt.go:52-58,85-91`），绝不再用可预测值签名。语义变化：未配置密钥时 token 不跨进程/重启存活，需稳定会话的部署必须显式配置 `auth.secret_key`。
- 测试：`core/jwt/jwt_test.go` 新增 `TestNewManagerEmptySecretIsRandom`（两个 manager 互不验签）、`TestManagerRejectsLegacyDefaultSecret`（旧常量签出的 token 被拒）；`core/jwt/jwt_extra_test.go` 新增 `TestManagerInitFailureFailsClosed`。

### F-3 · P1 · 安全 · 配置了飞书加密密钥仍接受明文卡片动作，绕过端点凭据

- 证据（修复前）：`api/handler_callbacks.go:69-83` 只在 `payload.Encrypt != ""` 时走解密；`else` 分支无条件继续处理明文 `payload.Action`。该端点故意不走 API token 认证（`api/server.go:293` 公开注册），`card_callback.encrypt_key` 就是它唯一的凭据。配置密钥后仍收明文，等于凭据形同虚设。
- 影响：任何能访问 `/api/v1/callbacks/feishu` 的人都能伪造卡片动作，`extractFeishuCardAck` 之后会执行告警确认/升级取消（`api/handler_callbacks.go:91-150`），造成告警被静默 ack、升级链被切断。无需知道 encrypt key。
- 修复：`b34fcf8`。`cardEncryptKey != ""` 时，明文动作返回 401（`api/handler_callbacks.go:84-90`）；未配置密钥时行为不变（继续接受明文、拒绝密文）；`url_verification` challenge 仍在鉴权分支之前应答，保留飞书控制台配置能力。
- 测试：`api/handler_callbacks_test.go` 新增 `TestFeishuCallbackRejectsPlaintextWhenKeyConfigured`、`TestFeishuCallbackChallengeWithKeyConfigured`。
- 文档：`docs/api/rest.md`、`docs/zh/api/rest.md`、`docs/guide/configuration.md`、`docs/zh/guide/configuration.md` 明确“配置密钥后只收密文”。

### F-4 · P1 · 安全 · `websocket.Config.MaxMessageSize` 声明未接线，未认证连接可撑爆内存

- 证据（修复前）：`core/websocket/server.go:103-140` 的 `Config` 声明了 `MaxMessageSize`，但 `NewServer` 不保存它，`handleWebSocket`（`:213-252`）也从未调用 `conn.SetReadLimit`。`/worker` 在 `core/websocket/server.go:154` 无认证注册，`handleConnection`（`:259-300`）直接 `ReadMessage` 并把整帧交给 `json.Unmarshal`。
- 影响：远端未认证客户端可发送任意大帧（gorilla 默认无上限），scheduler 按帧长分配内存；并发连接下是远程内存耗尽/进程 OOM。属可利用的可用性漏洞，且配置项存在会误导运维以为已限流。
- 修复：`8bcb8db`。`NewServer` 读取配置，`<=0` 兜底 1 MiB（`core/websocket/server.go:136-139`），升级后、读取前 `conn.SetReadLimit(s.maxMessageSize)`（`:231-237`）；`config.Load` 与 `config.Default` 都补默认值（`config/config.go:406-408,478`），`cmd/heraldd/main.go:579` 把 `cfg.WebSocket.MaxMessageSize` 传入；新增 `websocket.max_message_size` 配置项。
- 测试：`core/websocket/integration_test.go` 新增 `TestWebSocketRejectsOversizedMessage`（真实连接发超限帧，断言 1009 close）；`core/websocket/server_test.go` 新增 `TestNewServerCustomMaxMessageSize` 并断言默认 1 MiB。

### F-5 · P2 · 安全 · `/worker` 管理通道完全无认证，任何人可注册/心跳/伪造 worker 事件

- 证据：`core/websocket/server.go:154` `mux.HandleFunc("/worker", s.handleWebSocket)`，`handleWebSocket`（`:213-256`）无任何 token/证书校验；`handleMessage`（`:302-340`）接受 `register` / `heartbeat` / `ack` / `event` 四类消息，直接驱动 `core/websocket/hub.go:26-52` 的注册表、任务 ack 与事件回调。worker 客户端连接串见 `cmd/heraldd/main.go:859`。
- 场景：能连到 worker 端口的攻击者可注册任意 `workerID`（抢占/污染调度视图）、伪造 heartbeat 让死 worker 保持“在线”、伪造 ack 把未完成任务标记成功、注入 worker event 影响投递审计。当前部署假设 worker 端口在可信内网，但代码没有任何边界声明或校验。
- 处置：P2 记 todo（SEC-WORKER-AUTH）。修法：worker 侧共享 token/双向 TLS（`worker-sdk/go` 已有配置面可扩展），或至少把监听地址限制在 loopback/私网并在启动日志显式警告；补未认证连接拒绝测试。与 F-4 同面，但 F-4 是内存耗尽、本条是身份伪造，故分开记录。

### F-6 · P2 · 安全 · 登录端点无失败限速，可在线爆破管理员密码

- 证据：`api/server.go:256` 公开注册 `/api/v1/auth/login`；`core/auth/auth.go:135-171` 对每次请求直接 `userManager.Authenticate`，无 IP/账号维度计数、无退避、无锁定；`core/user/user.go:208` bcrypt cost 10，单次校验约几十毫秒，正好给攻击者一个可预测的在线尝试预算。
- 场景：弱口令或口令复用环境下，攻击者可对 `admin` 持续尝试；bcrypt 只提高单次成本，不限制尝试次数。当前也没有审计事件记录失败登录，无法事后发现爆破。
- 处置：P2 记 todo（SEC-LOGIN-RATE）。修法：按 IP + 用户名做令牌桶/指数退避（可复用 `core/limiter`），失败登录写审计事件，阈值触发临时锁定；测试覆盖 429 与恢复。

### F-7 · P2 · 安全 · 飞书回调 AES-CBC 无 MAC，且填充/JSON 错误可区分

- 证据：`api/handler_callbacks.go:157-176` 用 `SHA-256(encrypt_key)` 直接作 AES-256 密钥、密文首块作 IV，`CBCDecrypter` 后只做 PKCS7 去填充，没有 MAC/认证标签；`:74-81` 把解密错误原文返回（`err.Error()`），`:82-85` 对 JSON 解析失败返回另一条消息。`pkcs7Unpad` 的注释（`:179-190`）也承认长度守卫只服务于该调用路径。
- 场景：CBC 无 MAC 本身不直接给明文恢复，但结合可区分的填充错误与 JSON 错误，构成 padding-oracle 面；攻击者可反复提交篡改密文，借响应差异逐字节推断明文（卡片动作内容、open_id 等）。需要能访问端点，属半可信网络下的信息泄露。
- 处置：P2 记 todo（SEC-FEISHU-CBC）。修法：优先按飞书协议校验签名/时间戳；若只能改本地实现，改为 Encrypt-then-MAC（HMAC-SHA256 覆盖 IV+密文）或 AES-GCM，并把所有解密/解析失败折叠为同一条 400 响应。注意保持与飞书实际协议的兼容性，不能单方面换算法。

### F-8 · P2 · 安全 · 回调 URL 未拒私网/环回段（上轮结转，SSRF 加固面）

- 证据：`core/apps/callback.go:26-40` 的 `SetCallback` 只校验“可解析 + scheme 为 http/https + Host 非空”，未拒绝 `127.0.0.1`、`::1`、`10/8`、`172.16/12`、`192.168/16`、`169.254/16`；`core/callback/emitter.go:110-135` 随后把签名事件 POST 到该 URL。
- 场景与上轮一致：持 `config` scope 的集成方可把回调指向内网元数据地址或本机管理面，使 herald 成为内网请求代理。缓解因素（单组织自托管、集成方半可信、secret 属集成方、响应体不回传）未变，故维持 P2 而非升级。
- 处置：P2 已在 `todo.md`（SEC-CALLBACK），本轮复核仍成立，报告指针更新为 F-8。修法见该条：CIDR 拒绝列表 + 可配置豁免 + 测试。

### F-9 · P3 · 隐藏 bug · Redis `Nack` 在消息不存在时用 nil 错误格式化，产出 `%!w(<nil>)`

- 证据：`core/queue/redis.go:259-262`——`if err != nil || len(msgs) == 0 { return fmt.Errorf("failed to load message for retry: %w", err) }`。当 `XRange` 成功但返回空（pending 条目已被 trim/删除）时 `err == nil`，`%w` 对 nil 产生 `%!w(<nil>)`，日志与上层错误分类拿到的是无意义字符串而非“消息已不存在”。
- 场景：Redis Stream 被 trim、消费者组重建或 pending 条目被其他实例清理后，worker Nack 会记录误导性错误；若上层按错误文本分类重试，可能把“无需重试”判成可重试。
- 处置：P3 记 todo（BUG-NACK-ERR）。修法：拆成两个分支——`err != nil` 用 `%w`；`len(msgs)==0` 返回明确的哨兵错误（如 `ErrMessageGone`）或直接 `return nil`（消息已不存在，无需重投）。补单测覆盖空结果分支。

### F-10 · P3 · 隐藏 bug · 过期未使用的绑定 token 永不清理，内存随签发量单调增长

- 证据：`core/audience/surfaces.go:148-159` `IssueBinding` 每次 `s.tokens[token] = ...`；过期删除只发生在 `RedeemBinding` 命中该 token 时（`:182-185`）。从未被兑换的 token 永远留在 map 里，`SurfaceRegistry` 也没有周期清理器。
- 场景：长期运行的实例里，机器人深链签发量 = 潜在 map 增长量；15 分钟 TTL 只影响“能否兑换”，不影响“是否占内存”。攻击者若能触发大量 `IssueBinding`（需 app token），可把它当低频内存增长面。
- 处置：P3 记 todo（BUG-BINDING-GROWTH）。修法：`IssueBinding` 时顺带清扫过期项（摊销 O(n) 或按过期时间建最小堆/分桶），或注入时钟的周期 `Sweep` 由 daemon 定时调用；补“过期未兑换被清理”测试。

### F-11 · P3 · 安全 · API key / app token / RSS token 用 `==` 比较，非常量时间

- 证据：`core/auth/auth.go:115-119`（`apiKey == validKey`）、`core/apps/apps.go:119-125`（`t.Secret == secret`）、`core/audience/surfaces.go:364-375`（`tok == token`）。Go 字符串比较在长度/内容上提前返回，理论上泄露前缀匹配长度。
- 场景：远程计时侧信道需要极稳定的网络与大量样本；对本项目的自托管部署属低现实风险，但 API key 是高价值秘密，修法成本极低。
- 处置：P3 记 todo（SEC-CONSTTIME）。修法：统一 `crypto/subtle.ConstantTimeCompare`（长度不等先返回 0，注意不要把长度差异变成新的早退面，可先对 SHA-256 摘要比较）；补等价性测试。

### F-12 · P3 · 安全 · `herald_token` cookie 被当作 Bearer 接受，但仓库内无任何代码设置它，缺少 CSRF 约束

- 证据：`core/auth/auth.go:230-243` `getJWTToken` 先读 `Authorization: Bearer`，再回退读 `herald_token` cookie。全仓 grep 没有任何 `SetCookie("herald_token", ...)`（登录 `HandleLogin` 只回 JSON token，`:135-171`），说明该路径要么是外部反向代理注入，要么是遗留面。
- 场景：一旦部署方自行用 cookie 承载 JWT，浏览器会自动附带，管理面写操作就暴露在 CSRF 下；当前代码没有 SameSite/Origin/CSRF token 校验。因为默认不设置 cookie，现实暴露取决于部署方式，故 P3。
- 处置：P3 记 todo（SEC-JWT-COOKIE）。修法：要么删除 cookie 回退（只支持 Bearer），要么在启用 cookie 时强制 `SameSite=Strict` + 写操作校验 `Origin`/CSRF token；文档明确二选一。

### F-13 · P3 · 安全 · `config.Default()` 内置 `admin:admin`

- 证据：`config/config.go:489-491` `AdminUser: map[string]string{"admin": "admin"}`。
- 场景：`heraldd serve` 走 `config.Load`（缺文件时 `Default()` 再 `Validate`），当前 `Load` 路径下该默认值不会被直接启用；但任何库调用方/测试/未来子命令若用 `Default()` 启动，就会得到公开默认口令。属“危险默认值”而非当前可利用漏洞。
- 处置：P3 记 todo（SEC-DEFAULT-ADMIN）。修法：`Default()` 留空 map，`Validate()` 在 `auth.enabled` 且无任何凭据时拒绝启动；示例配置继续用环境变量。

### F-14 · P3 · 安全 · `.env.example` 用 `your-admin-api-key` 作为可直接生效的默认值

- 证据：`.env.example:12` `HERALD_ADMIN_API_KEY=your-admin-api-key`。修复 F-1 后该变量会被真正展开成 API key；若部署方直接 `cp .env.example .env` 而不改，管理面密钥就是公开占位串。
- 场景：与 F-1 同类的“示例即弱口令”，但需要运维直接复用示例文件；`Validate()` 只拒绝未解析的 `$VAR`，不拒绝已知占位串。
- 处置：P3 记 todo（SEC-ENV-PLACEHOLDER）。修法：占位值留空（`HERALD_ADMIN_API_KEY=`）并在注释说明必填，或 `Validate()` 拒绝 `your-*`/`change-me` 一类占位前缀；同步 README 快速开始。

## 已核查且干净的面

**① 安全静态扫描矩阵**：

| 向量 | 结论 | 证据 |
| --- | --- | --- |
| 硬编码密钥 | 修复后 0 处 | 全仓非测试代码 `password/secret/api_key/token = "字面量"` grep；`.env.example` 仅剩占位（F-14 另记），`config.yaml` 全 `$VAR` 引用且方向已修正 |
| shell 注入 | 0 处 | `exec.Command`/`syscall.Exec` 全仓零命中——heraldd 不起子进程 |
| SQL 拼接 | 不适用 | 无 `database/sql`、无 SQL 字面量（存储面是内存 + Redis Stream + JSON 文件） |
| eval/动态执行 | 0 处 | `eval(`/`reflect.`/`unsafe.` 非测试代码零命中；规则引擎走 expr-lang/expr 沙箱求值器 |
| 反序列化 | 安全 | `json.Unmarshal` 全部进类型化 struct；yaml 仅解析本地配置文件（`config/config.go:366`） |
| 路径穿越 | 0 处 | 所有文件路径来自启动配置，无 HTTP 输入拼接路径 |
| TLS 校验 | 干净 | `InsecureSkipVerify` 仅测试出现；生产 `core/httpclient/client.go:57` 用默认校验 |
| 头部注入 | 0 处 | `Header.Set` 全为固定值或配置值，自定义头来自 provider 配置而非 HTTP 请求 |
| 请求体上限 | 覆盖完整 | 全局中间件 `api/server.go:353-374` 包住整个 mux；apps/sources/callback 另有 `io.LimitReader(1<<20)` 双保险；WebSocket 面由 F-4 修复补齐 |
| 认证旁路 | 符合设计 | `withAuth` 在 auth 关闭时直通，受“只许 loopback”门禁守护（`config/config.go:550-554`）；`/status`、`/feeds/**` 匿名是文档承诺；飞书回调凭据面由 F-3 修复 |

**② 隐藏 bug 猎捕**：

- 竞态：`go test -race -count=1 ./...` 62 包全绿、0 DATA RACE、0 panic；websocket `s.mu` 护 `workers` map，digest 聚合器单 `mu` + 注入时钟，memory 队列 `mu.RLock` 与 chan 分离有注释论证，audience 注册表加锁正确。上轮 F-3 的停机窄竞态仍记 todo（BUG-WSSTOP），本轮未复现。
- 越界/off-by-one：分页参数默认 50、封顶 500（`api/handler_delivery.go:17-23`，logs 同型）；`plan.Stages[0]` 有空计划守卫；未发现 `[len()-1]` / `i<=len` 型索引。
- 资源泄漏：`http.Client` 均带超时；`resp.Body` 均 defer Close；goroutine 由 `wg`/`ctx` 收口；`time.After` 仅上轮 BUG-LIMITER 一处。
- 错误处理：非测试代码 `_ =` 逐条看过，均为清理性 Close/响应编码/降级路径；本轮新增 F-9 的 nil `%w`。
- panic 面：非测试代码仅构造器不变量两处（`core/audience/sources.go:27`、`reconcile.go:59`），非用户输入可达。

**③ 质量门**：

| 门 | 基线（修复前） | 终态（修复后） |
| --- | --- | --- |
| `gofmt -l .` | 0 文件 | 0 文件 |
| `go vet ./...` | 0 告警 | 0 告警 |
| `go build ./...` | 通过 | 通过 |
| `go test ./...` | 62 包 ok、0 FAIL | 62 包 ok、0 FAIL |
| `go test -race -count=1 ./...` | 62 包 ok、0 DATA RACE | 62 包 ok、0 DATA RACE |
| `golangci-lint run ./...` | 0 issues | 0 issues |

新增测试覆盖四个修复点，均随对应提交落库（见下）。

## 处置与提交

| 提交 | 类型 | 内容 |
| --- | --- | --- |
| `1a66791` | P0 | `fix(config): expand auth secret env refs and fail closed on unresolved keys` |
| `e31aad7` | P0 | `fix(jwt): replace hardcoded default secret with a random per-process key` |
| `b34fcf8` | P1 | `fix(callbacks): reject plaintext Feishu actions when an encrypt key is configured` |
| `8bcb8db` | P1 | `fix(websocket): bound worker frames on the unauthenticated management channel` |

- 一问题一提交；每个提交都带对应回归测试，提交后全量门禁复跑绿。
- P2/P3 共 10 项已记入 `todo.md`：SEC-WORKER-AUTH、SEC-LOGIN-RATE、SEC-FEISHU-CBC、SEC-CALLBACK（结转）、BUG-NACK-ERR、BUG-BINDING-GROWTH、SEC-CONSTTIME、SEC-JWT-COOKIE、SEC-DEFAULT-ADMIN、SEC-ENV-PLACEHOLDER。
- 本报告与 `todo.md` 更新单独提交，随后推送 `origin/main`。

## 独立审核意见（2026-11-02）

> 异模型审核（独立核验，不采信报告自述）。审核基准：报告写作时点（HEAD=be7c4b7）的代码状态 + 本审核时点（HEAD=955e11b）的现状。

### 核验方法

- ① 每条发现的 file:line 证据打开源码逐条对照；证据不实 = 打回（记「证伪」）。
- ② 声称已修的 P0/P1（F-1~F-4）：核对 commit 存在 + 在隔离 worktree（`git worktree add /tmp/herald-audit HEAD`）上复跑相关包测试，确认真绿（主工作树当时被另一会话在途改动占用，不用其跑测试）。
- ③ 抽查 3 个报告未覆盖的高危面：认证（`withAuth` 直通/`HandleRefresh`/`HandleMe`/`/feeds/private` 匿名面）、输入处理（分页参数/路径参数/notify body 上限）、资源释放（`time.After`/`resp.Body`/ticker/`time.AfterFunc` map/goroutine 收口）。

### 逐条真实性判定

| 条目 | 严重度 | 真实性 | 判定 |
| --- | --- | --- | --- |
| F-1 | P0 | 属实 | `config.yaml:16` 方向已修正（key=`$HERALD_ADMIN_API_KEY`、value=`operator`）；`config.go:523-566` ExpandEnv 覆盖 api_keys/secret_key/admin_user/card_callback/bot/wechat；`:611-619,:660-666` Validate fail-closed；测试 `TestExpandEnvSecrets`/`TestShippedConfigAuthOrientation`/`TestValidateRejectsUnresolvedSecrets` 均在。 |
| F-2 | P0 | 属实 | `jwt.go` `crypto/rand` 32 字节 + base64url 每进程密钥；`NewManager` 熵失败记 initErr，`Generate`:60-62 与 `Validate`:93-95 双 fail-closed；旧常量生产代码无残留；测试三个在。 |
| F-3 | P1 | 属实 | `handler_callbacks.go:84-89` 配置 `cardEncryptKey` 后明文 401（拒绝响应在 `:88`）、`url_verification` challenge 保留；测试两个在。 |
| F-4 | P1 | 属实 | `server.go:141-143` 读取配置、`<=0` 兜底 1 MiB、`:256` SetReadLimit；`config.go:406-408,:478` 默认值；`main.go:582` 接线；测试两个在。 |
| F-5 | P2 | 属实 | `server.go:159` `/worker` 无认证注册、`handleMessage`:328 收 register/heartbeat/ack/event、`hub.go` 驱动 registry；main.go:859 `/worker` 连接串。 |
| F-6 | P2 | 属实 | `server.go:256` login 公开、`auth.go:148` 直调 `Authenticate`、`user.go:208` bcryptCost=10、HandleLogin 无限速/退避。 |
| F-7 | P2 | **部分证伪** | CBC 无 MAC、错误可区分（`decrypt` err 原文 vs JSON 单独消息）属实；但「密文首块作 IV」表述不实——`handler_callbacks.go:171` `cipher.NewCBCDecrypter(block, key[:aes.BlockSize])` 的 IV 是 **SHA-256(encrypt_key) 摘要的前 16 字节**（密钥派生 IV），非密文首块。发现（padding-oracle 面）仍成立。 |
| F-8 | P2 | 属实（当时） | 报告写作时（be7c4b7）`callback.go:26-40` 仅验绝对 http(s)、不拒私网，属实；**本审核时已由 `37f35c4` 修复**（`defaultCallbackDenyCIDRs` 含 loopback/private/link-local/unspecified IPv4+IPv6 + `SetCallbackPolicy` 豁免），todo SEC-CALLBACK 已勾。 |
| F-9 | P3 | 属实 | `redis.go:263` `%w` 对 nil 产生 `%!w(<nil>)`，空结果分支无哨兵错误。 |
| F-10 | P3 | 属实 | `surfaces.go:158` IssueBinding 只写不删；过期删除仅在 RedeemBinding（`:186-191`，过期删除在 `:187`）；无周期清理器。 |
| F-11 | P3 | 属实 | `auth.go:116`/`apps.go:121`/`surfaces.go:371` 三处 `==` 比较。 |
| F-12 | P3 | 属实 | `auth.go:241` `herald_token` cookie 回退存在；全仓无 `SetCookie("herald_token")`。 |
| F-13 | P3 | 属实 | `config.go:489-490` `AdminUser: map[string]string{"admin": "admin"}`。 |
| F-14 | P3 | **部分证伪** | 占位值 `HERALD_ADMIN_API_KEY=your-admin-api-key` 属实；但行号 `:12` 不实——值在 `:16`（`:12-15` 是注释）。发现本身成立。 |

**核验 14 条；证伪 2 条（F-7、F-14，均为证据细节行号/表述小误，发现本身均成立）；其余 12 条属实。**

### 已修 P0/P1 复核

| 修复 | 声称 | 复核 |
| --- | --- | --- |
| `1a66791` | F-1 | commit 存在；config 包测试 HEAD 绿 |
| `e31aad7` | F-2 | commit 存在；jwt 包测试 HEAD 绿 |
| `b34fcf8` | F-3 | commit 存在；api 包测试 HEAD 绿 |
| `8bcb8db` | F-4 | commit 存在；websocket 包测试 HEAD 绿 |

4 个修复 commit 均存在，对应包测试在稳定 HEAD 上全绿（主工作树当时含另一会话在途改动故用隔离 worktree 复跑）。

### 漏网抽查（报告未覆盖的高危面）

| 面 | 抽查点 | 结论 |
| --- | --- | --- |
| 认证 | `withAuth` 直通（auth 关闭→受 loopback 门禁 `config.go:624` 守护）；`HandleRefresh`/`HandleMe` 自身 `getJWTToken`+`Validate`；`/feeds/private/{token}` 走 `RSSAudience(token)`；`/status`、`/feeds/{name}` 为文档承诺公开面 | 无漏网 |
| 输入处理 | 分页 `limit<=0→50、>500→500`；路径参数走 `PathValue` 类型化；`handleNotify` 裸 `json.NewDecoder` 被全局中间件 `limitRequestBodies`（`server.go:346` Handler 链包裹整个 mux）覆盖 | 无漏网 |
| 资源释放 | `httpclient.Get:288` defer Close；probe 用已读闭的 `Response.Body`；`escalation` `time.AfterFunc` map 在 `fire`:200-201、取消 `:247-249`、`Close`:182-184 三处清理；ticker/`time.After` 均收口 | 无漏网 |

### 总评

- **报告可信度：高（约 92%）**。14 条发现全部真实存在，4 个 P0/P1 修复属实且测试绿，抽查的 3 个高危面无漏网。扣分项：F-7 与 F-14 两处证据细节不实（IV 来源表述、行号），且 F-7 的「密文首块作 IV」若被当作攻击面建模依据会低估/误导实际方案（实为密钥派生 IV），建议修正该表述。
- **审后现状注记**：报告写作后另一会话已推进——`37f35c4`（修 SEC-CALLBACK/F-8）、`2de2fed`（修 BUG-LIMITER）、`3b0696f`（修 BUG-WSSTOP 停机窄竞态）、`955e11b`（修 SEC-WORKER-AUTH/F-5：`websocket.auth_token` 共享密钥 + 非 loopback 强制）。这些不改变报告当时真实性。
- **遗留待办（仍开放）**：F-6 登录限速、F-7 CBC 加固、F-9 Nack 哨兵、F-10 绑定清理、F-11 常量时间、F-12 cookie、F-13 默认 admin、F-14 占位密钥（均在 todo.md）。
