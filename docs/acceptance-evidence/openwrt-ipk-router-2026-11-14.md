# OpenWrt .ipk 真机验收证据 — herald（2026-11-14）

目标机：192.168.5.11（iStoreOS 24.10.8 x86_64，opkg 38eccbb1）
被测包：`herald_0.1.2-232-g4eeefe5-{1,2}_x86_64.ipk`、
`luci-app-herald_0.1.2-232-g4eeefe5-1_all.ipk`、
`app-meta-herald_0.1.2-232-g4eeefe5-1_all.ipk`（本地 build-ipk.sh 产出，
二进制 = CGO_ENABLED=0 GOOS=linux GOARCH=amd64 静态链接，~21.7MB strip 后）

## 装 / 升 / conffile 保留

- `opkg install` 三包依次 Configuring 成功，无 error。
- 升级路径：`--rel 2` 重建后 `opkg install` 报
  `Upgrading herald on root from 0.1.2-232-g4eeefe5-1 to 0.1.2-232-g4eeefe5-2`；
  升级中 opkg 报标准 conffile 分歧提示
  `resolve_conffiles: Existing conffile /etc/config/herald is different ... placed at /etc/config/herald-opkg`
  ——升级前 `uci get herald.general.enabled` = `1`，升级后仍 = `1`（conffile 保留实证）。
- `opkg files herald` 四文件齐：`/usr/bin/heraldd`、`/etc/init.d/herald`、
  `/etc/herald/config.yaml`、`/etc/config/herald`。

## procd 服务

- postinst 已 `enable`（boot-start wired：`/etc/init.d/herald enabled` exit 0）。
- `/etc/init.d/herald start` → `status` = running；`restart` 生效。
- init 渲染 `/var/etc/herald-config.yaml` 内容核对（127.0.0.1:8080、
  auth off + store_path、log provider、routes 三级 [log]、memory queue、
  websocket 127.0.0.1:8081）。

## 探活真数据

`curl http://127.0.0.1:8080/api/v1/status`（匿名 liveness 面，非 auth 中间件路径）：

```json
{"code":0,"message":"ok","data":{"providers":[
  {"name":"app-callback","type":"app-callback","status":"available","enabled":true,...},
  {"name":"log","type":"log","status":"available","enabled":true,...}],"status":"running"}}
```

## 资源画像（实测）

`/proc/<pid>/status`：`VmRSS = 16436 kB`（≈16.4MB）、`VmSize = 1280640 kB`
（Go runtime 常规虚拟预留）。路由器常驻可跑。

## LuCI

- 文件落位齐：controller/cbi `herald.lua`、`view/herald/status.htm`
  （**view/herald/ 父目录被 opkg 正常创建**——data.tar.gz 带目录条目的
  meshlet 教训修复在真机生效）、iStore 图标与 meta json。
- 真机 Lua 解析：`loadfile` 两文件 `parse ok`（router lua 5.1）。
- 菜单注册：重建 indexcache 后
  `/tmp/luci-indexcache.*.json` 树内含 `admin/services/herald`（含 status 子页）。
- **诚实边界**：浏览器登录渲染未走通（无 web 凭据）——与 meshlet
  OWRT-IPK-001 先例同口径；CBI/status 页的浏览器内行为待有凭据后人工复核。

## 真机抓出的包级真 bug（已修）

herald 对配置里**存在但 disabled** 的 provider 仍执行工厂创建：UCI 简单模式
首版渲染无条件输出 `telegram:` 段（enabled: false、无 config），工厂因
`telegram: token is required` 报错 → assembleServe 失败 → 进程退出 →
procd respawn 循环（logread 实录 `failed to create provider ... telegram: token is required`）。
修复（init.d/herald）：未启用不渲染 telegram 块；启用但 token/chat_id 缺一
则 init 直接 refuse start 并 logger 说明（fail-closed 与 heraldd 同口径）。
修复后 rel-2 包真机升级 → running。

## 架构覆盖

- x86_64：上表全部真机实证。
- aarch64：本地交叉编译产物 ELF 核查 `statically linked ... ARM aarch64`
  + build-ipk 结构检查同口径（**无真机，未实机安装——如实标注**）。

## .ipk 结构

三包均为 `debian-binary + control.tar.gz + data.tar.gz` 单 tar 布局；
data.tar.gz 全部带目录条目（`etc/ etc/herald/ usr/ usr/lib/ ...`），
conffiles 段含 `/etc/config/herald` 与 `/etc/herald/config.yaml`。
