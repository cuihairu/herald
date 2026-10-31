# Herald Dashboard UI 审计报告（impeccable audit）

- 日期：2026-10-31
- 目标：`herald` 仓库 `dashboard/`（Vite + React 19 + Ant Design 6 + react-router 7；darkAlgorithm 深色主题，colorPrimary `#3b82f6`）
- 工具：impeccable skill v4.8.0 `audit`；Playwright 1.63（chromium，桌面 1440×900 / 窄屏 390×844）；Go 编排后端（:8080）未运行，用 `/api/v1/**` 路由拦截注入数据态渲染。
- 健康评分：**76 / 100**（详见「总体评判」）

> 审计截图存放于本文件同目录的 `2026-10-31-ui-audit/`（下文截图引用均相对此目录）。

## 总体评判（不留情面）

**这是一套底子很稳、但"桌面端认真、移动端失守"的运维后台。** 桌面端（1440px）是它的主场：antd v6 深色主题统一、每页懒加载分包、Provider 卡片网格与状态/级别彩色标签都做得干净利落，看得出是给"盯监控"的人做的。**问题全在窄屏——而且不是小瑕疵，是整页废掉：**

1. **侧边栏 200px 固定不收敛（P0）。** 390px 视口下，`Sider` 没有 `breakpoint`，永远撑开 200px，内容列被挤到只剩 142px——仪表盘的统计卡片被右缘裁切（"running"只露出"runni"、成功数"1,210"裁断）。手机上第一屏就是残的。
2. **表格页在窄屏丢列 + 整页横滚（P1）。** 任务日志/投递状态/通知规则/通知群组/值班表/事件台账六张表用固定列宽（如日志表 Provider 120 + 状态 80 + 级别 80 + 耗时 80 + 时间 180 = 540px），没设 `scroll`——于是最关键的**标题、错误两列被压成 0 宽直接消失**，页面自身横向滚到 664px，筛选器和表头一起被拖走。

这两类本次已修复（见下），修复后 24 个「路由 × 视口」组合（登录页 2 + 受保护页 22）全部 `overflow=false`、0 页面异常。

**没修的三件小事，我如实记录**：antd v6 的几个弃用告警（`Statistic valueStyle`、`Space direction`、`Drawer width`）、`ProviderConfigPage` 的 `useForm` 未连接告警，以及一个**在 pristine main 上就存在的失败单测**（`DeliveriesPage.test.tsx` 分页点击，jsdom 下 `pointer-events:none`）——这个失败与我本次改动无关，我用 stash 验证过：去掉我的改动它照样失败。它属于既有技术债，不应由 UI 审计背锅，但也不该假装它绿。

说句公道话：**桌面端的排版、表格、状态用色、分包策略都相当成熟**；这次的问题纯粹是"移动端没测过"。修复用的是 antd 标准响应式手法（`breakpoint` + `scroll.x`），没有动任何视觉方向。

## 修复的问题（2 个问题 → 2 个 commit）

### P0 · 窄屏侧边栏不收敛，内容列被压碎

- **现象**：390px 视口下 `Sider` 恒为 200px，内容列仅 142px；仪表盘统计卡片右缘被裁切（"runni"/"1,284"/"1,210"）。
- **根因**：`src/layouts/DashboardLayout.tsx` 的 `<Sider>` 未设 `breakpoint`/`collapsedWidth`。
- **修复**：`<Sider collapsible collapsed={collapsed} onCollapse={setCollapsed} breakpoint="lg" collapsedWidth={64}>`——≤992px 自动收敛为 64px 图标栏，内容列恢复 278px，统计卡片完整显示。
- **验证**：修复后 390px 下 sider=64px、content=278px、右缘 374px < 390，无裁切。
- **截图**：`2026-10-31-ui-audit/narrow-dashboard.png`（修复后）、`desktop-dashboard.png`（桌面参照）。
- **Commit**：`7fd9778`。

### P1 · 表格页窄屏丢列 + 整页横滚

- **现象**：任务日志等六张表在窄屏把标题/错误列压成 0 宽消失，页面横向滚到 664px。
- **根因**：antd `Table` 未设 `scroll`，固定列宽总和超容器时，弹性列被挤没、表格撑破文档。
- **修复**：为 7 处 `Table` 增加 `scroll={{ x: 'max-content' }}`（Logs、Deliveries、Rules×2、Groups、Rosters、Incidents）——列按自然宽度渲染、在容器内横向滚动，文档不再溢出。
- **验证**：窄屏日志表 `.ant-table-content` scrollWidth 628px 收敛于 278px 容器内滚动，7 列（含标题/错误）全部渲染。
- **文件**：`src/pages/LogsPage.tsx`、`DeliveriesPage.tsx`、`RulesPage.tsx`、`GroupsPage.tsx`、`RostersPage.tsx`、`IncidentsPage.tsx`。
- **截图**：`2026-10-31-ui-audit/narrow-logs.png`（修复后）。
- **Commit**：`82ca3c3`。

## 遗留问题（记录，未改动）

### P3 · antd v6 弃用告警（开发控制台）

- `Statistic` 的 `valueStyle` 弃用（`DashboardPage`、`LogsPage`、`DeliveriesPage`，改用 `styles.content`）；`Space` 的 `direction` 弃用（`RostersPage`，改用 `orientation`）；`Drawer` 的 `width` 弃用（`RulesPage`，改用 `size`）。纯弃用告警，无运行时影响；建议随升级统一迁移。

### P3 · `ProviderConfigPage` 的 `useForm` 未连接告警

- `form` 在 `config` 加载前创建，`<Form>` 在数据就绪后才挂载，antd 因此告警。良性但嘈杂；建议按需创建或忽略该告警。

### Info · 既有失败单测（与本次无关）

- `src/pages/DeliveriesPage.test.tsx`「paginates to page 2 with the right offset」在 **pristine main** 上即失败（`pointer-events: none`，jsdom 点击分页 '2'）。已用 `git stash` 验证：去掉本次 `scroll` 改动后依旧失败，属既有技术债，非本次引入。

## 缺失上下文

- 仓库无 `PRODUCT.md` / `DESIGN.md`。既有视觉实现（antd 深色后台）即设计权威，本次按精修执行，未臆造缺失上下文。

## 正面发现

- **主题一致性**：`ConfigProvider` 单点 `darkAlgorithm` + `colorPrimary #3b82f6`，全站统一深色。
- **分包策略**：每页懒加载（route-level code splitting）+ `vite.config.js` 按 antd 真实依赖分层（react / antd-icons / antd-rc / antd-cssinjs / antd），产物最大 chunk 470KB→gzip 141KB，结构成熟。
- **桌面排版**：仪表盘 6 张统计卡 + Provider 健康网格（xl=6/lg=8/sm=12/xs=24 响应式）干净；表格状态/级别用彩色标签，深底对比清晰。
- **修复后回归**：24 个「路由 × 视口」组合 `overflow=false`、0 页面异常；`tsc -b` 通过；`vite build` 通过（5.33s）。

## 结论

本次修复 2 类响应式硬伤（侧边栏收敛、表格容器内滚动），全部通过回归验证；桌面端本就成熟。剩余为 antd v6 弃用告警与 1 个既有失败单测（均与本次无关）。**这座后台的桌面体验值得保留，短板只在"移动端从未测过"——修的是标准手法，没动视觉方向。**
