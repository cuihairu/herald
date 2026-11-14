-- Copyright 2026 cuihairu <chuihairu@gmail.com>
-- SPDX-License-Identifier: Apache-2.0

module("luci.controller.herald", package.seeall)

function index()
	if not nixio.fs.access("/etc/config/herald") then
		return
	end

	entry({"admin", "services", "herald"},
		cbi("herald"),
		_("Herald 通知"), 60).dependent = true

	entry({"admin", "services", "herald", "status"},
		template("herald/status"),
		_("运行状态"), 10).leaf = true

	-- Internal JSON endpoint backing the status page. Not a menu entry.
	entry({"admin", "services", "herald", "api", "status"},
		call("api_status")).leaf = true
end

-- Strip control characters and cap the length (same policy as the custos
-- precedent: log-derived text must not flood rows or the response).
local function sanitize(value, max)
	if value == nil then
		return ""
	end
	local s = tostring(value):gsub("%c", "")
	if #s > max then
		s = s:sub(1, max) .. "…"
	end
	return s
end

local function shell_quote(s)
	return "'" .. tostring(s):gsub("'", "'\\''") .. "'"
end

-- procd state via the rc.common wrapper (exit 0 = running).
local function service_state()
	local code = luci.sys.call("/etc/init.d/herald status >/dev/null 2>&1")
	return (code == 0) and "running" or "stopped"
end

function api_status()
	luci.http.prepare_content("application/json")
	local uci = require("luci.model.uci").cursor()
	local utl = require("luci.util")

	local out = {
		service = { status = service_state() },
	}

	out.service.mode = uci:get("herald", "general", "mode") or "simple"
	out.service.addr = (uci:get("herald", "general", "http_addr") or "127.0.0.1")
		.. ":" .. (uci:get("herald", "general", "http_port") or "8080")

	-- Recent herald syslog lines (procd instance + init logger both carry
	-- the herald tag). Tail 25, sanitized.
	out.logs = {}
	local logs_raw = utl.exec("logread -e herald 2>/dev/null | tail -n 25")
	if logs_raw and logs_raw ~= "" then
		for line in logs_raw:gmatch("[^\r\n]+") do
			out.logs[#out.logs + 1] = sanitize(line, 200)
		end
	end

	-- Live probe of heraldd itself: /api/v1/status is the anonymous
	-- liveness/status face (loopback hop), independent of the auth
	-- middleware. Returned provider table feeds the status page.
	local addr = out.service.addr
	local raw = utl.exec("curl -s --max-time 5 " ..
		shell_quote("http://" .. addr .. "/api/v1/status") .. " 2>/dev/null")
	if raw == nil or raw == "" then
		out.error = "探活失败（heraldd 未运行或端口不通）"
		luci.http.write_json(out)
		return
	end
	local parsed = require("luci.jsonc").parse(raw)
	if parsed == nil then
		out.error = "探活响应不是合法 JSON"
		luci.http.write_json(out)
		return
	end
	out.live = {
		status = sanitize(parsed.data and parsed.data.status or "", 32),
		providers = parsed.data and parsed.data.providers or {},
	}

	luci.http.write_json(out)
end
