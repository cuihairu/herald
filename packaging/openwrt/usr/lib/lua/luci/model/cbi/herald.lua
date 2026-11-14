-- Copyright 2026 cuihairu <chuihairu@gmail.com>
-- SPDX-License-Identifier: Apache-2.0

local fs = require("nixio.fs")

m = Map("herald", translate("Herald 通知编排"),
	translate("通知编排引擎：统一接收告警并按路由投递到 Telegram/邮件/企业微信等渠道。" ..
		"简单模式由 UCI 渲染配置（服务参数 + Telegram 通道）；完整配置面（provider/规则/模板/RSS）" ..
		"切到高级模式，直接编辑下方 config.yaml。"))

-- Restart the service after a successful apply; the init script re-renders
-- the simple-mode config from UCI, so every setting change takes effect
-- immediately. Restarting a disabled service is a no-op.
function m.on_commit()
	luci.sys.call("/etc/init.d/herald restart >/dev/null 2>&1")
end

-- 服务运行参数。
s = m:section(NamedSection, "general", "general", translate("服务"))
s.addremove = false

o = s:option(Flag, "enabled", translate("启用服务"))
o.rmempty = false

o = s:option(ListValue, "mode", translate("配置模式"))
o:value("simple", translate("简单（UCI 渲染）"))
o:value("advanced", translate("高级（config.yaml 直编）"))
o.default = "simple"
o.rmempty = false
o.description = translate("简单模式：UCI 渲染服务参数 + Telegram；高级模式：直接使用下方 config.yaml")

o = s:option(Value, "http_addr", translate("HTTP 绑定地址"))
o.default = "127.0.0.1"
o.rmempty = false
o.description = translate("回环地址无需鉴权；LAN/公网地址必须填下方 API key（否则拒绝启动）")

o = s:option(Value, "http_port", translate("HTTP 端口"))
o.default = "8080"
o.datatype = "port"
o.rmempty = false

o = s:option(Value, "db_path", translate("凭据库路径 (SQLite)"))
o.default = "/etc/herald/store.db"
o.rmempty = false
o.description = translate("Dashboard 签发的 API key 持久化于此，重启不丢")

o = s:option(Value, "api_key", translate("API key"))
o.password = true
o.description = translate("Dashboard/API 鉴权密钥；绑定非回环地址时必填")

o = s:option(Value, "log_name", translate("日志通道名"))
o.default = "herald"
o.rmempty = false
o.description = translate("log provider 在通知里署的名字")

o = s:option(Value, "config_path", translate("配置文件路径"))
o.default = "/etc/herald/config.yaml"
o.rmempty = false
o.description = translate("高级模式使用的完整配置文件（升级包不覆盖）")

-- Telegram 通道（核心通知面最小字段）。
t = m:section(NamedSection, "telegram", "telegram", translate("Telegram 通道"))
t.addremove = false

o = t:option(Flag, "enabled", translate("启用 Telegram"))
o.rmempty = false
o.description = translate("启用后所有级别路由 log + telegram")

o = t:option(Value, "token", translate("Bot token"))
o.password = true
o.description = translate("@BotFather 签发，形如 123456:ABC-DEF…")

o = t:option(Value, "chat_id", translate("Chat ID"))
o.description = translate("投递目标会话/群组 ID")

-- config.yaml 直编逃生口（round-trip：仅保存时写文件，绝不自动改写）。
a = m:section(NamedSection, "general", "general", translate("完整配置 (config.yaml)"))
a.addremove = false

raw = a:option(TextValue, "raw_config")
raw.rows = 28
raw.wrap = "off"
raw.description = translate("高级模式的完整配置；简单模式下只作参考编辑，切到高级模式即生效。" ..
	"凭据请勿留占位符——herald 拒绝以未解析的 $VAR 引用启动。")
function raw.cfgvalue()
	return fs.readfile("/etc/herald/config.yaml") or ""
end
function raw.write(self, section, value)
	if value ~= nil then
		if #value > 0 and value:sub(-1) ~= "\n" then
			value = value .. "\n"
		end
		fs.writefile("/etc/herald/config.yaml", value:gsub("\r\n", "\n"))
	end
end
function raw.remove()
end

return m
