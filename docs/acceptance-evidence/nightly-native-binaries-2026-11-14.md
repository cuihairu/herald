# nightly 通用产物矩阵验收证据 — herald（2026-11-14）

任务令：`~/.hermes/cache/scratch/tasks/herald-nightly-products.md`
（2026-11-14 用户令「补全」：nightly 在 OpenWrt 三 ipk 之外补通用产物）

提交：`6c225aa` fix（默认 config 真 bug）、`1b83d19` ci（nightly 矩阵扩展 +
README 双语节）、`794afba` fix（首轮 dispatch 两处 smoke 修复）。

## 产物矩阵与实滚落位

实滚：`gh workflow run nightly.yml` → **run 43602721523 六 job 全绿**
（gate 2m19s、build×2 双腿、build-windows 1m45s、build-macos 47s、
publish 18s）。rolling nightly release 落位 9 资产 + SHA256SUMS：

| 资产 | 字节 |
|---|---|
| herald-nightly-linux-x86_64.tar.gz | 8170694 |
| herald-nightly-linux-aarch64.tar.gz | 7358929 |
| herald-nightly-windows-x86_64.zip | 8402491 |
| herald-nightly-macos-arm64.tar.gz | 7539243 |
| herald-nightly-macos-x86_64.tar.gz | 8173841 |
| herald-nightly-openwrt-x86_64.ipk | 8136574 |
| herald-nightly-openwrt-aarch64.ipk | 7317549 |
| herald-nightly-openwrt-luci-app.ipk | 5623 |
| herald-nightly-openwrt-app-meta.ipk | 4506 |

tar.gz/zip 平铺四文件：`heraldd`（.exe）、`config.yaml`、`LICENSE`、
`README.md`（解包实测）；资产名滚动无版本号，`SHA256SUMS` 覆盖全
`herald-nightly-*`。

## 平台 smoke 口径（每平台产物 build 后实跑）

统一两重断言：

1. **Usage 面**：无参调用打印 `Usage: heraldd` 且 exit 1（heraldd 无
   `--help` flag，无参即 usage 面）。
2. **serve 活体**：写最小 config（127.0.0.1:18793/18794 冷端口对、auth
   off、log provider、memory queue）→ `heraldd serve -c` → 轮询断言
   `/api/v1/status` 返回 `"status":"running"` → kill。

| 平台 | runner | 执行方式 |
|---|---|---|
| linux x86_64 | ubuntu-latest | 原生实跑 |
| linux aarch64 | ubuntu-latest | qemu-user-static（`qemu-aarch64-static`，二进制纯静态 Go） |
| windows x86_64 | windows-latest | 原生实跑 `.exe` |
| macos arm64 | macos-latest | 原生实跑 |
| macos x86_64 | macos-latest | Rosetta 2（`softwareupdate --install-rosetta` 后实跑） |

本地预演（同口径先跑后提交）：linux x86_64 与 aarch64 双跑
`serve smoke ok`（后者经 qemu）；Usage 面 `rc=1` + `Usage: heraldd`。

## 首轮 dispatch 失败根因（run 43601610399，已修）

首跑由并行会话 dispatch，gate/build(macos)/build(linux amd64) 全绿，
败两处：

1. **linux arm64 smoke**：Usage 检查写在 qemu-user-static 安装**之前**，
   aarch64 ELF 在 x86_64 runner 上 `Exec format error`，输出为空 → case
   落 `*)` → exit 1。修：qemu 探测前置并包裹 Usage 与 serve 两重检查。
   （本地预演被 binfmt 注册掩盖：本机装过 qemu-user，`./heraldd` 经
   binfmt 自动转译，故首轮未在本地暴露。）
2. **windows smoke**：step 实际输出 `serve smoke ok (windows x86_64)`，
   但 exit 1——actions runner 对 pwsh step 隐式追加
   `exit $LASTEXITCODE`，而 Usage 检查里的 heraldd exit 1 残留在
   `$LASTEXITCODE`，结尾 `Write-Host` 不重置它。修：成功路径显式
   `exit 0`。

## 顺带修的真 bug：默认 config 的 disabled telegram 段

`packaging/openwrt/etc/herald/config.yaml` 含 `telegram: enabled: false`
段。herald 对**存在但 disabled** 的 provider 仍执行工厂创建（telegram
工厂无条件要求 token）→ `failed to create provider: telegram: token is
required` → assembleServe 失败 → 进程退出。该文件正是上一任务
OWRT-001 的 advanced 模式 conffile，当时只验过 conffile 落位、从未实跑。

本机实证：修复前 `heraldd serve -c` 报 `failed to create provider
"name":"telegram","error":"telegram: token is required"`，
`scheduler started` 计数 0（起不来）；修复后 scheduler started=1、
`/api/v1/status` 返回 `code:0` + providers + `status: running`。

修复：删掉 disabled 段，替换为说明该约束的注释（与 OWRT-001 中 init.d
「未启用不渲染 telegram 块」同口径），同时保留手动启用 telegram 的
写法示例。该文件改为通用默认 config 单源（openwrt advanced 与 native
包共用，文件头注释同步改为双通道口径）。

## 下载链路复验

`gh release download nightly -p herald-nightly-linux-x86_64.tar.gz -p
SHA256SUMS` → `sha256sum -c` 该资产 **OK** → 解包四文件齐
（heraldd 21725346B / config.yaml 1635B / LICENSE / README.md）→
包内 config（端口改冷门对、store 改本地路径）实跑：
`scheduler started addr=127.0.0.1:18795 workers=29` + 探活
`"status":"running"`。README 一句用法
`heraldd serve --config config.yaml` 与实跑一致。包内 config 的
`telegram` 字样仅出现在注释中（3 处，无实际段）。

## 本地门禁

r25 全量：`REAL_EXIT=0 / FAILCOUNT=0`；worker-sdk example 子档按既定
口径 netns 回填（本机 `:8081` 被 atlas 容器占）；`covermerge` 6663 块、
4 个 main() 入口全非零；`zero_check --gate 100` →
`equivalent stmt coverage 100.00%`、`GREEN`（16 ledger-exempted）。
`actionlint` + 内嵌 shellcheck 对 nightly.yml 全绿。
push 后 CI run 43602720579 success（3m21s）。

## 诚实边界

- linux **aarch64** 无真机：qemu-user 全功能模拟执行（静态纯 Go 二进制，
  非结构检查了事），但与 aarch64 真机行为仍非同一证据。
- **windows / macOS** 无本机 runner，全部由 GitHub-hosted
  windows-latest / macos-latest 原生执行（macos x86_64 经 Rosetta 2，
  非原生 Apple Silicon/x86 硬件实跑）。
- 本地预演只覆盖 linux 两架构（含 qemu），windows/macos 路径首验即
  CI 实滚。
- 首轮 dispatch 的失败 run 43601610399 与并行会话 push 时序交叠；最终
  远端 main = 本地 `794afba`，历史无中间态残留。
