#!/usr/bin/env python3
"""Generate off/on + GIF for every Herald configuration switch.

Renders a per-switch HTML demo (replicating the dashboard's antd Switch
and a behavioral effect preview), captures off / on / transition frames
with headless Chromium, and stitches them into a looping GIF with ffmpeg.
"""
import glob
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
OUT = REPO / "docs" / "public" / "screenshots" / "config"
TMP = Path(tempfile.mkdtemp(prefix="herald-config-effect-"))

CHROME_CANDIDATES = sorted(
    glob.glob("/home/cui/.cache/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-linux64/chrome-headless-shell")
)
CHROME = next((c for c in reversed(CHROME_CANDIDATES)), None)

SWITCHES = [
    {"id": "auth_enabled", "group": "security", "path": "auth.enabled",
     "title": "API 鉴权", "title_en": "API Authentication",
     "desc": "开启后管理面 API 需要 Bearer / X-API-Key 凭证；关闭时仅允许回环地址绑定。",
     "effect": "管理面 API 返回 401，要求 Bearer / X-API-Key。",
     "effect_off": "管理面 API 匿名可访问（仅限回环绑定）。",
     "demo": "auth", "preview_off": "GET /api/v1/status → 200 匿名", "preview_on": "GET /api/v1/status → 401 未授权"},
    {"id": "dedup_enabled", "group": "delivery", "path": "dedup.enabled",
     "title": "去重", "title_en": "Deduplication",
     "desc": "同一窗口内重复事件只投递一次。",
     "effect": "窗口内重复事件被折叠，只投递第一条。",
     "effect_off": "每条事件都投递，重复不折叠。",
     "demo": "dedup", "preview_off": "事件 A ×3 → 投递 3 条", "preview_on": "事件 A ×3 → 投递 1 条"},
    {"id": "digest_enabled", "group": "delivery", "path": "digest.enabled",
     "title": "Digest 聚合", "title_en": "Digest Aggregation",
     "desc": "开启后事件按窗口折叠成摘要，定时翻转投递。",
     "effect": "窗口内事件聚合为一条摘要，定时翻转投递。",
     "effect_off": "每条事件直接投递（不聚合）。",
     "demo": "digest", "preview_off": "5 条事件 → 5 条投递", "preview_on": "5 条事件 → 1 条摘要"},
    {"id": "feeds_enabled", "group": "delivery", "path": "feeds.enabled",
     "title": "RSS 拉式渠道", "title_en": "RSS Pull Channel",
     "desc": "开启后渲染 /feeds/** 公共与私密 feed。",
     "effect": "/feeds/** 返回渲染后的 RSS XML。",
     "effect_off": "/feeds/** 不开放，rss 渠道视为未知 Provider。",
     "demo": "feeds", "preview_off": "GET /feeds/xxx → 404", "preview_on": "GET /feeds/xxx → RSS XML"},
    {"id": "sources_enabled", "group": "integration", "path": "sources.enabled",
     "title": "来源适配器", "title_en": "Source Adapter",
     "desc": "开启后 Telegram 机器人 / 公众号回调与定期对账生效。",
     "effect": "外部关注/取关事件写入注册表，定期对账外部状态。",
     "effect_off": "外部事件被忽略，注册表不受外部影响。",
     "demo": "sources", "preview_off": "外部关注事件 → 忽略", "preview_on": "外部关注事件 → 写入注册表"},
    {"id": "sources_reconcile_enabled", "group": "integration", "path": "sources.reconcile.enabled",
     "title": "来源定期对账", "title_en": "Source Reconciliation",
     "desc": "开启后定期探测外部平台是否仍认识目标，失效则注销。",
     "effect": "定期探测，失效联系面被注销并终止渠道订阅。",
     "effect_off": "不做对账，外部失效不自动清理。",
     "demo": "reconcile", "preview_off": "失效联系面 → 不清理", "preview_on": "失效联系面 → 注销"},
    {"id": "delivery_allow_phone", "group": "delivery", "path": "delivery.allow_phone",
     "title": "电话渠道放行", "title_en": "Allow Phone Channel",
     "desc": "解除 §6.2 电话默认禁用门：L5 强度渠道需此开关才投递。",
     "effect": "电话渠道（L5）允许投递（仍需受众绑定电话联系面）。",
     "effect_off": "电话渠道保持默认禁用，被强度门拒绝。",
     "demo": "phone", "preview_off": "L5 电话渠道 → 拒绝", "preview_on": "L5 电话渠道 → 放行"},
]

PROVIDER_TYPES = [
    "log", "telegram", "feishu", "wecom", "dingtalk", "slack", "discord",
    "email", "webhook", "aliyunsms", "tencentsms", "neteasesms",
    "wechatmp", "wechat", "fcm", "apns", "jpush", "getui",
]
PROVIDER_LABELS = {
    "log": "日志", "telegram": "Telegram", "feishu": "飞书", "wecom": "企业微信",
    "dingtalk": "钉钉", "slack": "Slack", "discord": "Discord", "email": "邮件",
    "webhook": "Webhook", "aliyunsms": "阿里云短信", "tencentsms": "腾讯云短信",
    "neteasesms": "网易云信短信", "wechatmp": "微信公众号", "wechat": "微信个人推送",
    "fcm": "FCM", "apns": "APNs", "jpush": "JPush", "getui": "个推",
}

EXTRA_SWITCHES = [
    {"id": "feishu_interactive_cards", "group": "provider", "path": "providers.feishu.config.interactive_cards",
     "title": "飞书交互卡片", "title_en": "Feishu Interactive Cards",
     "desc": "告警以带「确认告警」按钮的交互卡片发送（需 card_callback）。",
     "effect": "告警渲染为交互卡片，带 Acknowledge 按钮。",
     "effect_off": "告警以普通文本消息发送。",
     "demo": "feishu_card", "preview_off": "文本消息", "preview_on": "交互卡片 + 确认按钮"},
    {"id": "jpush_apns_production", "group": "provider", "path": "providers.jpush.config.apns_production",
     "title": "JPush APNs 生产环境", "title_en": "JPush APNs Production",
     "desc": "true 走 APNs 生产环境，false 走沙盒。",
     "effect": "推送发往 APNs 生产环境。",
     "effect_off": "推送发往 APNs 沙盒环境。",
     "demo": "jpush_env", "preview_off": "APNs 沙盒", "preview_on": "APNs 生产"},
]

FONTS = "font-family: -apple-system, 'Segoe UI', Roboto, 'Noto Sans SC', 'Noto Sans CJK SC', sans-serif;"

BASE_CSS = """
* { box-sizing: border-box; }
body { margin: 0; background: #0f172a; color: #e2e8f0; %(font)s }
.wrap { width: 480px; padding: 24px; }
.card { background: #1e293b; border: 1px solid #334155; border-radius: 12px; padding: 18px 20px; }
.row { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.name { font-size: 15px; font-weight: 600; color: #f1f5f9; }
.path { font-family: 'Noto Sans Mono', monospace; font-size: 11px; color: #94a3b8; margin-top: 4px; }
.state { font-size: 12px; color: #94a3b8; margin-top: 10px; }
.switch { width: 52px; height: 28px; border-radius: 16px; background: #475569;
  position: relative; transition: background .35s ease; flex-shrink: 0; }
.switch .knob { width: 22px; height: 22px; border-radius: 50%; background: #fff;
  position: absolute; top: 3px; left: 3px; transition: left .35s ease; box-shadow: 0 1px 3px rgba(0,0,0,.4); }
.effect { margin-top: 14px; border-top: 1px dashed #334155; padding-top: 12px; font-size: 12.5px; color: #cbd5e1; }
.effect .chip { display: inline-block; padding: 2px 8px; border-radius: 6px; font-size: 11px; margin-bottom: 6px; }
.effect.off .chip { background: #334155; color: #94a3b8; }
.effect.on .chip { background: #14532d; color: #86efac; }
.preview { margin-top: 12px; }
.preview .box { display: inline-block; padding: 8px 14px; border-radius: 8px; font-size: 13px;
  border: 1px solid #334155; background: #0f172a; }
.preview .box.ok { border-color: #16a34a; background: #052e16; color: #86efac; }
.preview .box.warn { border-color: #f59e0b; background: #451a03; color: #fcd34d; }
"""


def css(off, on):
    return BASE_CSS.replace("%(font)s", FONTS) + f"""
#stage.on .switch {{ background:#16a34a; }}
#stage.on .switch .knob {{ left:27px; }}
"""


def demo_html(s, state):
    """state: 'off' | 'on' | 'animate'."""
    on = state == "on"
    if state == "animate":
        # keyframes-driven transition: off->on then hold on.
        extra = """
#stage .switch { animation: swbg 1.5s ease forwards; }
#stage .switch .knob { animation: swknob 1.5s ease forwards; }
@keyframes swbg { 0% { background:#475569; } 100% { background:#16a34a; } }
@keyframes swknob { 0% { left:3px; } 100% { left:27px; } }
#stage .fx-off { animation: fades 1.5s ease forwards; }
#stage .fx-on { animation: fades2 1.5s ease forwards; }
@keyframes fades { 0% { opacity:1; } 100% { opacity:0; } }
@keyframes fades2 { 0% { opacity:0; } 100% { opacity:1; } }
#stage .pv { transition: none; }
#stage .pv.on { animation: pvcol 1.5s ease forwards; }
@keyframes pvcol { 0% { border-color:#f59e0b; background:#451a03; color:#fcd34d; }
  100% { border-color:#16a34a; background:#052e16; color:#86efac; } }
"""
        off_html = f"""<div class="effect off fx-off"><span class="chip">效果</span><div>{s['effect_off']}</div></div>
  <div class="preview"><span class="box warn pv">{s.get('preview_off','—')}</span></div>"""
        on_html = f"""<div class="effect on fx-on"><span class="chip">效果</span><div>{s['effect']}</div></div>
  <div class="preview"><span class="box ok pv on">{s.get('preview_on','—')}</span></div>"""
        body = f"""<div class="card" id="stage">
  <div class="row">
    <div><div class="name">{s['title']}</div><div class="path">{s['path']}</div></div>
    <div class="switch"><div class="knob"></div></div>
  </div>
  <div class="state">OFF → ON</div>
  {off_html}
  {on_html}
</div>"""
    else:
        extra = ""
        state_cls = " on" if on else ""
        fx_cls = "effect on" if on else "effect off"
        fx_txt = s['effect'] if on else s['effect_off']
        pv_cls = "box ok" if on else "box warn"
        pv_txt = s.get('preview_on', '—') if on else s.get('preview_off', '—')
        body = f"""<div class="card" id="stage{state_cls}">
  <div class="row">
    <div><div class="name">{s['title']}</div><div class="path">{s['path']}</div></div>
    <div class="switch"><div class="knob"></div></div>
  </div>
  <div class="state">{"ON" if on else "OFF"}</div>
  <div class="{fx_cls}"><span class="chip">效果</span><div>{fx_txt}</div></div>
  <div class="preview"><span class="{pv_cls}">{pv_txt}</span></div>
</div>"""
    return f"""<!doctype html><html><head><meta charset="utf-8"><style>{css(off=True, on=True)}{extra}</style></head><body>
<div class="wrap">{body}</div>
</body></html>"""


def capture(png, html, budget):
    cmd = [CHROME, "--headless", "--no-sandbox", "--disable-gpu",
           "--hide-scrollbars", "--window-size=520,260",
           f"--screenshot={png}", f"--virtual-time-budget={budget}",
           f"file://{html}"]
    subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def make_gif(prefix, out):
    lst = sorted(glob.glob(str(TMP / f"{prefix}-*.png")))
    if len(lst) < 3:
        raise RuntimeError(f"only {len(lst)} frames for {prefix}")
    palette = str(TMP / f"{prefix}-palette.png")
    subprocess.run(["ffmpeg", "-y", "-loglevel", "error",
                    "-framerate", "10", "-i", str(TMP / f"{prefix}-%d.png"),
                    "-vf", "fps=10,scale=480:-1:flags=lanczos,split[s0][s1];"
                    "[s0]palettegen[p];[s1][p]paletteuse",
                    "-loop", "0", str(out)], check=True)


def gen_switch(s):
    sid = s["id"]
    # off
    off_html = TMP / f"{sid}-off.html"
    off_html.write_text(demo_html(s, "off"))
    capture(str(OUT / f"{sid}-off.png"), str(off_html), 300)
    # on
    on_html = TMP / f"{sid}-on.html"
    on_html.write_text(demo_html(s, "on"))
    capture(str(OUT / f"{sid}-on.png"), str(on_html), 300)
    # transition frames via keyframes (virtual time drives the animation)
    anim_html = TMP / f"{sid}-anim.html"
    anim_html.write_text(demo_html(s, "animate"))
    n = 8
    for i in range(n):
        t = 60 + i * 160
        capture(str(TMP / f"{sid}-frame-{i}.png"), str(anim_html), t)
    make_gif(f"{sid}-frame", str(OUT / f"{sid}.gif"))
    return OUT / f"{sid}-off.png", OUT / f"{sid}-on.png", OUT / f"{sid}.gif"


def main():
    if not CHROME:
        sys.exit("chromium_headless_shell not found")
    OUT.mkdir(parents=True, exist_ok=True)
    manifest = []
    all_sw = list(SWITCHES) + list(EXTRA_SWITCHES)
    for ptype in PROVIDER_TYPES:
        all_sw.append({
            "id": f"provider_{ptype}_enabled", "group": "provider",
            "path": f"providers.{ptype}.enabled",
            "title": PROVIDER_LABELS.get(ptype, ptype),
            "title_en": PROVIDER_LABELS.get(ptype, ptype),
            "desc": f"开启后 {ptype} 渠道参与投递；关闭后被路由跳过。",
            "effect": f"{ptype} 渠道可投递，路由会选中它。",
            "effect_off": f"{ptype} 渠道被跳过，路由不会选中它。",
            "demo": "provider",
            "preview_on": f"{ptype}: 已启用", "preview_off": f"{ptype}: 已禁用",
        })
    for s in all_sw:
        off, on, gif = gen_switch(s)
        manifest.append({**s, "off": str(off), "on": str(on), "gif": str(gif)})
    (OUT / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2))
    print(f"generated {len(manifest)} switches -> {OUT}")


if __name__ == "__main__":
    main()
