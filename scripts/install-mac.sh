#!/usr/bin/env bash
# 在 macOS 上安装或升级 Switchboard，不用去「隐私与安全性」点「仍要打开」。
# 安装包还没有苹果公证；浏览器下载的 dmg 带「来自网络」标记，系统才会拦。这个脚本用 curl 下载（不带标记），
# 装好后再把标记清一遍，所以双击就能打开。只在你信任这个安装包时用。
#
# 用法：
#   bash scripts/install-mac.sh                      # 下载最新版：先 Gitee 镜像，没有再去 GitHub
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

# 找本机芯片对应的 dmg 地址：Gitee、GitHub 各问一次最新正式版，取版本高、又有这个芯片 dmg 的那个；一样新时用 Gitee，国内快。
# 不用发布列表：Gitee 的列表旧的在前，取第一条会装回老版本。
dmg_url() {
  /usr/bin/python3 - "$ARCH" "$@" <<'PY'
import json, subprocess, sys
arch, apis = sys.argv[1], sys.argv[2:]
def ver(tag):
    out = []
    for p in tag.lstrip("v").split("-")[0].split(".")[:3]:
        try: out.append(int(p))
        except ValueError: out.append(0)
    return tuple(out + [0] * (3 - len(out)))
best = None
for api in apis:
    try:
        raw = subprocess.run(["curl", "-fsSL", "--max-time", "20", api], capture_output=True, timeout=30).stdout
        r = json.loads(raw)
    except Exception:
        continue
    if not isinstance(r, dict) or r.get("prerelease") or r.get("draft"):
        continue
    for a in r.get("assets") or []:
        u = a.get("browser_download_url") or ""
        if u.endswith("_" + arch + ".dmg"):
            v = ver(r.get("tag_name") or "")
            if best is None or v > best[0]:
                best = (v, u)
            break
if best:
    print(best[1])
PY
}

if [ $# -ge 1 ]; then
  DMG="$1"
  [ -f "$DMG" ] || die "找不到安装包：$DMG"
else
  URL="$(dmg_url "https://gitee.com/api/v5/repos/$REPO/releases/latest" "https://api.github.com/repos/$REPO/releases/latest")"
  [ -n "$URL" ] || die "Gitee 和 GitHub 上都没找到 ${ARCH} 的安装包；也可以先手动下载，再把 dmg 路径传给这个脚本"
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
