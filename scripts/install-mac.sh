#!/usr/bin/env bash
# 在 macOS 上安装或升级 Switchboard，不用去「隐私与安全性」点「仍要打开」。
# 安装包还没有苹果公证；浏览器下载的 dmg 带「来自网络」标记，系统才会拦。这个脚本用 curl 下载（不带标记），
# 装好后再把标记清一遍，所以双击就能打开。只在你信任这个安装包时用。
#
# 用法：
#   bash scripts/install-mac.sh                      # 下载最新版：先 GitHub，连不上换 Gitee
#   bash scripts/install-mac.sh ~/Downloads/xxx.dmg  # 用手上的安装包
set -euo pipefail

REPO="imoling/iml-switchboard"
APP="/Applications/Switchboard.app"
BACKEND="http://127.0.0.1:18081"

say() { printf '%s\n' "$*"; }
die() { printf '安装失败：%s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || die "这个脚本只用于 macOS"
case "$(uname -m)" in
  arm64) ARCH="aarch64" ;;
  x86_64) ARCH="x64" ;;
  *) die "不认识的芯片：$(uname -m)" ;;
esac

WORK="$(mktemp -d)"
MNT="$WORK/mnt"
cleanup() { hdiutil detach "$MNT" -quiet >/dev/null 2>&1 || true; rm -rf "$WORK"; }
trap cleanup EXIT

# 找本机芯片对应的 dmg 地址。预发布版也算，所以用版本列表，不用「最新版」接口。
dmg_url() {
  local api="$1"
  curl -fsSL --max-time 20 "$api" 2>/dev/null | /usr/bin/python3 -c '
import json, sys
arch = sys.argv[1]
try:
    rels = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for r in rels if isinstance(rels, list) else []:
    for a in r.get("assets") or []:
        u = a.get("browser_download_url") or ""
        if u.endswith("_" + arch + ".dmg"):
            print(u)
            sys.exit(0)
' "$ARCH"
}

if [ $# -ge 1 ]; then
  DMG="$1"
  [ -f "$DMG" ] || die "找不到安装包：$DMG"
else
  URL="$(dmg_url "https://api.github.com/repos/$REPO/releases?per_page=10")"
  [ -n "$URL" ] || URL="$(dmg_url "https://gitee.com/api/v5/repos/$REPO/releases?per_page=10")"
  [ -n "$URL" ] || die "GitHub 和 Gitee 上都没找到 ${ARCH} 的安装包；也可以先手动下载，再把 dmg 路径传给这个脚本"
  say "正在下载 $(basename "$URL")…"
  DMG="$WORK/Switchboard.dmg"
  curl -fL --progress-bar --max-time 600 -o "$DMG" "$URL" || die "下载没完成：$URL"
fi

say "正在安装到「应用程序」…"
mkdir -p "$MNT"
hdiutil attach -nobrowse -readonly -mountpoint "$MNT" "$DMG" -quiet || die "打不开安装包：$DMG"
[ -d "$MNT/Switchboard.app" ] || die "安装包里没有 Switchboard.app"

# 先退出正在运行的界面；后台服务另算，见下面
if pgrep -f "$APP/Contents/MacOS/iml-switchboard" >/dev/null 2>&1; then
  osascript -e 'tell application "Switchboard" to quit' >/dev/null 2>&1 || pkill -TERM -f "$APP/Contents/MacOS/iml-switchboard" || true
  sleep 2
fi
rm -rf "$APP"
ditto "$MNT/Switchboard.app" "$APP" || die "复制失败；「应用程序」文件夹可能没有写权限"
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true
hdiutil detach "$MNT" -quiet || true

# 后台服务还是旧版本时换成新的；正在出图、出视频或回答问题时不动它，免得打断，界面里的「后台服务」可以稍后重启
busy="$(curl -s --noproxy 127.0.0.1 --max-time 3 -H 'X-IMLS-Client: install' "$BACKEND/api/status" 2>/dev/null | /usr/bin/python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("busy") or 0)
except Exception: print("down")' || echo down)"
if [ "$busy" = "0" ]; then
  pkill -TERM -f 'imls serve' || true
  sleep 2
  nohup "$APP/Contents/MacOS/imls" serve --background >/dev/null 2>&1 &
  disown || true
  say "后台服务已换成新版本"
elif [ "$busy" != "down" ]; then
  say "后台服务有 $busy 个任务在跑，没有重启；稍后在设置的「后台服务」里重启"
fi

open "$APP"
say "装好了：$(defaults read "$APP/Contents/Info" CFBundleShortVersionString 2>/dev/null || echo Switchboard)"
