#!/bin/sh
# Builds the three OpenWrt .ipk packages for herald (meshlet/custos precedent):
#   herald             heraldd static binary + procd init + UCI config +
#                      default full config.yaml, arch-specific
#   luci-app-herald    LuCI controller, settings page and status dashboard
#                      (arch all)
#   app-meta-herald    iStore app metadata (icon + app json), arch all
#
# Usage: build-ipk.sh <bin-dir> [out-dir] [--arch x86_64|aarch64] [--rel N]
#                      [--version V]
#
# <bin-dir> must contain the heraldd binary (e.g. bin/, a cross-compile
# output dir). The package architecture is derived from the ELF (`file`);
# --arch overrides it. herald has no in-repo version file, so the package
# version comes from --version (the nightly workflow passes `git describe
# --tags --always`); default 0.0.0. The .ipk container is the tar.gz layout
# (debian-binary + control.tar.gz + data.tar.gz as a single tar), the same
# form the custos/meshlet packages ship and which opkg on iStoreOS 24.10
# installs.
set -eu

die() { printf 'build-ipk: %s\n' "$*" >&2; exit 1; }

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
SRC="$SCRIPT_DIR"

BINDIR=""
OUT="."
ARCH_OVERRIDE=""
REL_OVERRIDE=""
VERSION_OVERRIDE=""
while [ $# -gt 0 ]; do
	case "$1" in
		--rel)
			[ $# -ge 2 ] || die "--rel requires a value"
			REL_OVERRIDE="$2"
			shift 2
			;;
		--version)
			[ $# -ge 2 ] || die "--version requires a value"
			VERSION_OVERRIDE="$2"
			shift 2
			;;
		--arch)
			[ $# -ge 2 ] || die "--arch requires a value (x86_64|aarch64)"
			ARCH_OVERRIDE="$2"
			shift 2
			;;
		--*)
			die "unknown option: $1"
			;;
		*)
			if [ -z "$BINDIR" ]; then
				BINDIR="$1"
			else
				OUT="$1"
			fi
			shift
			;;
	esac
done

[ -n "$BINDIR" ] || die "usage: build-ipk.sh <bin-dir> [out-dir] [--arch x86_64|aarch64] [--rel N] [--version V]"
[ -d "$BINDIR" ] || die "bin dir not found: $BINDIR"
[ -f "$BINDIR/heraldd" ] || die "missing binary in $BINDIR: heraldd"
[ -f "$SRC/etc/herald/config.yaml" ] || die "etc/herald/config.yaml missing"
[ -f "$SRC/app-meta/herald.json" ] || die "app-meta/herald.json missing"
[ -f "$SRC/app-meta/herald.png" ] || die "app-meta/herald.png missing"

VERSION="${VERSION_OVERRIDE:-0.0.0}"
case "$VERSION" in
	[0-9]*) ;;
	*) VERSION=$(printf '%s' "$VERSION" | sed 's/^v//' | sed 's/^[^0-9]*//') ;;
esac
[ -n "$VERSION" ] || die "cannot derive a package version"
REL="${VERSION}-1"
[ -n "$REL_OVERRIDE" ] && REL="${VERSION}-${REL_OVERRIDE}"

if [ -n "$ARCH_OVERRIDE" ]; then
	case "$ARCH_OVERRIDE" in
		x86_64 | aarch64) ARCH="$ARCH_OVERRIDE" ;;
		*) die "unsupported --arch: $ARCH_OVERRIDE (expected x86_64|aarch64)" ;;
	esac
else
	# `file` reports the ELF machine, mapped to opkg's architecture names.
	ARCH=$(file -b "$BINDIR/heraldd" | sed -n 's/.*[, ]\(x86-64\|aarch64\).*/\1/p' | head -1)
	case "$ARCH" in
		x86-64) ARCH=x86_64 ;;
		aarch64) ARCH=aarch64 ;;
		*) die "unsupported binary architecture: $ARCH (pass --arch)" ;;
	esac
fi

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

# ---- herald (arch-specific) -----------------------------------------------
R="$STAGE/herald"
mkdir -p "$R/data/etc/init.d" "$R/data/etc/config" "$R/data/etc/herald" \
	"$R/data/usr/bin" "$R/CONTROL"
cp "$SRC/etc/init.d/herald" "$R/data/etc/init.d/"
cp "$SRC/etc/config/herald" "$R/data/etc/config/"
cp "$SRC/etc/herald/config.yaml" "$R/data/etc/herald/"
cp "$BINDIR/heraldd" "$R/data/usr/bin/"
chmod 755 "$R/data/etc/init.d/herald" "$R/data/usr/bin/heraldd"

cat > "$R/CONTROL/conffiles" <<'EOF'
/etc/config/herald
/etc/herald/config.yaml
EOF
cp "$SRC/CONTROL/postinst" "$R/CONTROL/postinst"
cp "$SRC/CONTROL/prerm" "$R/CONTROL/prerm"
SIZE=$(du -sk "$R/data" | cut -f1)
cat > "$R/CONTROL/control" <<EOF
Package: herald
Version: $REL
Depends: libc
Source: https://github.com/cuihairu/herald
SourceName: herald
License: Apache-2.0
Section: net
SourceDateEpoch: 1794931200
Maintainer: cuihairu <chuihairu@gmail.com>
Architecture: $ARCH
Installed-Size: $SIZE
Description: Notification orchestration engine: routes alerts to Telegram, email, Feishu, WeCom and more, with a dashboard, dedup/digest aggregation and RSS pulls; static heraldd binary with procd service. All credentials are user-supplied, nothing is embedded.
 通知编排引擎：统一接收告警并按路由投递到 Telegram/邮件/飞书/企业微信等渠道，带 Dashboard、去重聚合与 RSS 拉取；静态 heraldd 二进制与 procd 服务。凭据全部由用户提供，包内不内嵌。
EOF

chmod 755 "$R"/CONTROL/postinst "$R"/CONTROL/prerm
mkdir -p "$OUT"
tar -czf "$R/control.tar.gz" -C "$R/CONTROL" ./control ./postinst ./prerm ./conffiles
# Whole-tree archive so the data member set includes directory entries:
# opkg's unpacker does not create missing parent directories for file-only
# members (/etc/herald/ was absent on a fresh router, so config.yaml failed
# to unpack with "wfopen: ... No such file or directory").
tar -czf "$R/data.tar.gz" -C "$R/data" etc usr
echo "2.0" > "$R/debian-binary"
tar -czf "$OUT/herald_${REL}_${ARCH}.ipk" -C "$R" \
	./debian-binary ./control.tar.gz ./data.tar.gz
echo "built: $OUT/herald_${REL}_${ARCH}.ipk"

# ---- luci-app-herald (all) -------------------------------------------------
R="$STAGE/luci"
mkdir -p "$R/data/usr/lib/lua/luci/controller" "$R/data/usr/lib/lua/luci/model/cbi" \
	"$R/data/usr/lib/lua/luci/view/herald" "$R/CONTROL"
cp "$SRC/usr/lib/lua/luci/controller/herald.lua" "$R/data/usr/lib/lua/luci/controller/"
cp "$SRC/usr/lib/lua/luci/model/cbi/herald.lua" "$R/data/usr/lib/lua/luci/model/cbi/"
cp "$SRC/usr/lib/lua/luci/view/herald/status.htm" "$R/data/usr/lib/lua/luci/view/herald/"

cat > "$R/CONTROL/postinst" <<'EOF'
#!/bin/sh
# Drop the LuCI menu/index caches so the new controller routes are picked
# up without a reboot (idempotent; nothing to do when the cache is absent).
rm -f /tmp/luci-indexcache* 2>/dev/null || true
exit 0
EOF
cat > "$R/CONTROL/prerm" <<'EOF'
#!/bin/sh
exit 0
EOF
SIZE=$(du -sk "$R/data" | cut -f1)
cat > "$R/CONTROL/control" <<EOF
Package: luci-app-herald
Version: $REL
Depends: herald, curl
Source: https://github.com/cuihairu/herald
SourceName: luci-app-herald
License: Apache-2.0
Section: net
SourceDateEpoch: 1794931200
Maintainer: cuihairu <chuihairu@gmail.com>
Architecture: all
Installed-Size: $SIZE
Description: LuCI interface for herald: settings page (service parameters, Telegram channel, full config.yaml editor) and a live status dashboard (service state, provider status, recent logs).
 herald 的 LuCI 界面：设置页（服务参数、Telegram 通道、config.yaml 直编）与实时状态面板（服务状态、provider 状态、最近日志）。
EOF

chmod 755 "$R"/CONTROL/postinst "$R"/CONTROL/prerm
tar -czf "$R/control.tar.gz" -C "$R/CONTROL" ./control ./postinst ./prerm
# Whole-tree archive with directory entries (see the herald note above).
tar -czf "$R/data.tar.gz" -C "$R/data" usr
echo "2.0" > "$R/debian-binary"
tar -czf "$OUT/luci-app-herald_${REL}_all.ipk" -C "$R" \
	./debian-binary ./control.tar.gz ./data.tar.gz
echo "built: $OUT/luci-app-herald_${REL}_all.ipk"

# ---- app-meta-herald (all) — the iStore-facing metadata package -----------
R="$STAGE/meta"
mkdir -p "$R/data/usr/lib/opkg/meta" "$R/data/www/luci-static/resources/app-icons" \
	"$R/CONTROL"
# The manifest version tracks the package version at build time.
sed "s/\"version\": \"[^\"]*\"/\"version\": \"$VERSION\"/" \
	"$SRC/app-meta/herald.json" > "$R/data/usr/lib/opkg/meta/herald.json"
cp "$SRC/app-meta/herald.png" "$R/data/www/luci-static/resources/app-icons/herald.png"

cat > "$R/CONTROL/postinst" <<'EOF'
#!/bin/sh
exit 0
EOF
cat > "$R/CONTROL/prerm" <<'EOF'
#!/bin/sh
exit 0
EOF
SIZE=$(du -sk "$R/data" | cut -f1)
cat > "$R/CONTROL/control" <<EOF
Package: app-meta-herald
Version: $REL
Depends: luci-app-herald
Source: https://github.com/cuihairu/herald
SourceName: app-meta-herald
License: Apache-2.0
Section: net
SourceDateEpoch: 1794931200
Maintainer: cuihairu <chuihairu@gmail.com>
Architecture: all
Installed-Size: $SIZE
Description: iStore app metadata for herald (icon and app manifest).
 herald 的 iStore 应用元数据（图标与应用清单）。
EOF

chmod 755 "$R"/CONTROL/postinst "$R"/CONTROL/prerm
tar -czf "$R/control.tar.gz" -C "$R/CONTROL" ./control ./postinst ./prerm
# Whole-tree archive with directory entries (see the herald note above).
tar -czf "$R/data.tar.gz" -C "$R/data" usr www
echo "2.0" > "$R/debian-binary"
tar -czf "$OUT/app-meta-herald_${REL}_all.ipk" -C "$R" \
	./debian-binary ./control.tar.gz ./data.tar.gz
echo "built: $OUT/app-meta-herald_${REL}_all.ipk"
