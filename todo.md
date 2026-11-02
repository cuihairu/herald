# Herald 受众层改造需求清单

Phase 0-9 与受众层三阶段全部收官。事实底稿、差异与留批拍板见 design-audience-audit。测试数为该批收官提交时的 Go 测试函数数。

## 已完成

- [x] Phase 0 现状审计：方案 vs 实现差异清单与测试底稿盘点 — `d4a6f8c`（1079 例）
- [x] Phase 1 领域模型：user: 受众配置化 MVP — `3b56660`（1095 例）
- [x] Phase 2 Routing 边界：expandRefs 纯决策步骤与投递执行分离 — `efaae6b`（1097 例）
- [x] Phase 3 Delivery 状态机：枚举化 + 同步重试 — `3b56660`（1095 例）
- [x] Phase 4 Queue/Worker：MaxAttempts/LastError 回填与全链路核对 — `d65d3e9`（1100 例）
- [x] Phase 5 Provider 错误分类：errclass 六类词汇接入重试判定 — `35b9b37`（1107 例）
- [x] Phase 6 API 领域字段：notify 增 channel/audience/data/idempotency_key — `8dddeb8`（1096 例）
- [x] Phase 7 Configuration：channels 独立配置块与优先级链 — `edc81d3`（1111 例）
- [x] Phase 8 测试：Multi Provider 隔离全真链路补齐 — `ba10542`（1112 例）
- [x] Phase 9 Documentation：七文档对齐收官、差异总表清零 — `523746a`（1112 例）
- [x] 留批补齐：RateLimited per-class 退避（Retry-After）— `39979bb`（1126 例）
- [x] 留批补齐：next_retry_at 异步重新入队 — `20e25d4`（1160 例）
- [x] 插批 A 概念边界与分层审计：五层地图与八词裁决成文 — `de4bbac`（1220 例）
- [x] 受众层 1 关系模型与受众注册表：RelationType 枚举与 Subscribe/Enroll API — `3fc1a82`（1166 例）
- [x] 受众层 2 联系面与绑定 API：SurfaceRegistry 三态与一次性 token 绑定 — `19cf16c`（1179 例）
- [x] 受众层 3 偏好中心：Frequency/Preference/DefaultPolicy 与读写 API — `4df837a`（1184 例）
- [x] 受众层 4 投递管道关系过滤：ChannelClass 四族与 MatrixAllows 门 — `4b18b4a`（1190 例）
- [x] 受众层 5 投递审计：core/audit 流水与去重折叠明细 — `de05e94`（1195 例）
- [x] 受众层 6 Digest 聚合器：窗口聚合与 FlipLoop 定时器 — `3204e90`（1220 例）
- [x] 受众层 7 RSS 拉式渠道：feeds 存储、公开/私密 feed 与投递投影 — `a96e1a1`（1244 例）
- [x] 受众层 8 来源适配器：三端点入口与定期对账 Reconciler — `dc5092e`/`2f3d867`（1282 例）
- [x] 受众层 9 渠道强度与投递模式：Intensity/Urgency/Mode 与 DeliveryPolicy — `0c20459`（1314 例）
- [x] 受众层 10 去重与频控：Gate 去重、折叠与频控三档 — `3c6c395`/`ca6ee39`（1297 例）
- [x] 受众层 11 集成者 API 与 Go SDK：app 命名空间、触发/查询 API 与 SDK — `4899d8f`…`f062c20`（1355 例）

## 留批（显式设计决策，不实施；待拍板 D4F1）

- [x] Logs 与 Delivery 状态分离 — `0effddf`…`6534269`（1432 例）
- [x] audiences/recipients/channels 运行时 API — audiences 写口已落地（`8193172`）；recipients/channels 写口代决维持纯配置（见 design-audience-deferrals）
- [x] group 成员渠道块展开 — `2a857ff`（1398 例）

## docs 双语补齐（2026-10-16 执行单）

- [x] docs 根目录 13 份文档中英配对与首屏语言切换器
- [x] docs 子目录 45 份文档双语配对：api 3、architecture 7、guide 9、providers 21、runtime 5；判据同根目录轮（中文文档补对应英文版，两份顶部首屏内语言切换器，真实翻译不得机翻占位，命令路径代码块专有名词不译；排除 CHANGELOG、CONTRIBUTING、LICENSE、CODE_OF_CONDUCT、SECURITY 与 node_modules、dist、build、target、coverage、vendor）— 45/45 配对，切换器 90/90，死链 0，docs build 绿；`coverage.*` 忽略规则吞掉 architecture/coverage.en.md 已补反向放行

## Dependabot 清零（2026-10-21 执行单）

- [x] 清理 open 安全告警 #99/#98/#93/#97/#96/#95/#94 — `5ffb5343fd0be9336176f3e7526c0b4fbfb4c85d`（docs build 绿；dashboard typecheck 绿、197/197 tests、coverage 100%）

- [ ] DOC-GIF · P2 · 文档 · 配置开关效果动态图：为每个「开启后有效果」的配置开关生成 off→on 对比 GIF（GUI 用截图+切换录屏、CLI 用终端录屏 asciinema→GIF/ffmpeg），嵌入 docs「配置效果」页（双语），每个开关都有 off/on+GIF 且流水线可重跑（兼配置效果回归测试）。
- [ ] SEC-CALLBACK · P2 · 安全 · 回调 URL 私网加固：`core/apps/callback.go` SetCallback 只验绝对 http(s)，未拒私网/环回段（127.0.0.1、::1、10/8、172.16/12、192.168/16、169.254/16）；多租户部署下持 config scope 的集成方可把签名回调指向内网。补 CIDR 拒绝列表（可配置豁免）+ 测试。证据见 docs/review/2026-11-01-code-review.md F-1；2026-11-02 复核仍成立，见 docs/review/2026-11-02-code-review-opencode.md F-8。
- [ ] BUG-LIMITER · P3 · 并发 · limiter.Wait 计时器 churn：`core/limiter/limiter.go:169` 在 Wait 循环每轮 `time.After(waitTime)`，ctx 取消时计时器不能提前回收；高频取消下短期堆积。改 NewTimer+Stop 复用。证据见同报告 F-2。
- [ ] BUG-WSSTOP · P3 · 并发 · websocket 关闭窄竞态：`core/websocket/server.go` Stop() 关连接后 Shutdown 再 wg.Wait()，升级中的连接若在 Shutdown 生效前进入 handleConnection 的 wg.Add，与已启动的 Wait 竞争；加关闭门闩。证据见同报告 F-3。
- [ ] SEC-WORKER-AUTH · P2 · 安全 · `/worker` 管理通道无认证：`core/websocket/server.go:154` 公开注册，`handleWebSocket`（:213-256）无 token/证书校验，`handleMessage`（:302-340）接受 register/heartbeat/ack/event 并直接驱动 `core/websocket/hub.go:26-52`；可伪造 worker 身份、保活、任务 ack 与事件。补 worker 共享 token 或双向 TLS，或至少限制监听网段并显式告警。证据见 docs/review/2026-11-02-code-review-opencode.md F-5。
- [ ] SEC-LOGIN-RATE · P2 · 安全 · 登录无爆破限速：`api/server.go:256` 公开注册 `/api/v1/auth/login`，`core/auth/auth.go:135-171` 每次直接 `userManager.Authenticate`，无 IP/账号计数与退避；`core/user/user.go:208` bcrypt cost 10 只提高单次成本。补令牌桶/指数退避 + 失败审计 + 429 测试。证据见同报告 F-6。
- [ ] SEC-FEISHU-CBC · P2 · 安全 · 飞书回调 CBC 无 MAC 且错误可区分：`api/handler_callbacks.go:157-176` 用 SHA-256(key) 作 AES-256 密钥、密文首块作 IV，仅 PKCS7 去填充无认证标签；`:74-81` 返回解密错误原文、`:82-85` 返回另一条 JSON 错误，构成 padding-oracle 面。优先按飞书协议校验签名；否则 Encrypt-then-MAC/GCM，并把所有解密失败折叠为同一响应。证据见同报告 F-7。
- [ ] BUG-NACK-ERR · P3 · 隐藏 bug · Redis `Nack` nil 格式化：`core/queue/redis.go:259-262` 在 `XRange` 成功但结果为空时仍 `fmt.Errorf("...: %w", err)`，err 为 nil 产出 `%!w(<nil>)`，丢失“消息已不存在”语义。拆分支返回哨兵错误或直接成功，补空结果测试。证据见同报告 F-9。
- [ ] BUG-BINDING-GROWTH · P3 · 隐藏 bug · 过期未兑换绑定 token 永不清理：`core/audience/surfaces.go:148-159` IssueBinding 只写入，过期删除仅在 RedeemBinding 命中时（:182-185）；长期运行下 map 随签发量单调增长。补签发时摊销清扫或周期 Sweep + 测试。证据见同报告 F-10。
- [ ] SEC-CONSTTIME · P3 · 安全 · 秘密比较非常量时间：`core/auth/auth.go:115-119` API key、`core/apps/apps.go:119-125` app token、`core/audience/surfaces.go:364-375` RSS token 均用 `==`，理论上前缀计时侧信道。统一改 `crypto/subtle.ConstantTimeCompare`（可先比 SHA-256 摘要避免长度早退）。证据见同报告 F-11。
- [ ] SEC-JWT-COOKIE · P3 · 安全 · `herald_token` cookie 无 CSRF 约束：`core/auth/auth.go:230-243` 接受 cookie 作为 JWT，但全仓无 `SetCookie("herald_token")`；部署方若自行注入 cookie，管理面写操作暴露于 CSRF。删除该回退，或启用时强制 SameSite=Strict + Origin/CSRF 校验。证据见同报告 F-12。
- [ ] SEC-DEFAULT-ADMIN · P3 · 安全 · `config.Default()` 内置 `admin:admin`：`config/config.go:489-491`；`heraldd serve` 走 Load 暂不可达，但库调用方/未来子命令会得到公开默认口令。Default 留空 map，Validate 在 auth.enabled 且无凭据时拒绝启动。证据见同报告 F-13。
- [ ] SEC-ENV-PLACEHOLDER · P3 · 安全 · `.env.example` 占位密钥可直接生效：`.env.example:12` `HERALD_ADMIN_API_KEY=your-admin-api-key`，修复 F-1 后会被真正展开；直接复用示例即弱口令。占位留空并注明必填，或 Validate 拒绝 `your-*`/`change-me` 前缀。证据见同报告 F-14。

## 审核发现

- [x] 审核发现：README「Run Locally」/`make run` 用出厂 config.yaml 直接启动时二进制不读 .env，`HERALD_ADMIN_API_KEY` 未导出即触发 Validate 拒起；本 diff 给 config.yaml 开鉴权却没同步本地运行说明（README.md:116, config.yaml:16）— 属实已修；`go test ./config` 34 tests green，`make -n run` 显式 source .env。
- [ ] 审核发现：出厂 config.yaml 开鉴权却未配 `admin_user`，README 快速开始仍称「登录后…」，实际 `/auth/login` 因无用户恒 401，Dashboard 登录不可达（README.md:131, config.yaml:13, core/user/user.go:73）。
- [ ] 审核发现：KNOWN_UNCOVERABLE.md 已把 server.go 行号 370 更正为 382，但文档一致性台账仍声称「server.go:370 落在所述块上」，行号漂移未同步（docs/审计-文档一致性.md:391, KNOWN_UNCOVERABLE.md:61）。
