#!/usr/bin/env bash
# 冒烟测试：用临时数据目录起后台，逐个敲接口。不会动你的真实配置。
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH=/opt/homebrew/bin:$PATH
export IMLS_DATA_DIR=$(mktemp -d /tmp/imls-smoke.XXXX)
export IMLS_SECRETS_FILE=1
export IMLS_ADDR=${IMLS_ADDR:-127.0.0.1:18091}
BIN=${BIN:-bin/imls}
[ -x "$BIN" ] || go build -o bin/imls ./cmd/imls
"$BIN" serve >"$IMLS_DATA_DIR/serve.log" 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null; echo; echo "日志：$IMLS_DATA_DIR/serve.log"' EXIT
for i in $(seq 1 40); do curl -sf -H 'X-IMLS-Client: smoke' http://$IMLS_ADDR/api/status >/dev/null && break; sleep 0.25; done
H='-H X-IMLS-Client:smoke -H Content-Type:application/json'
check() { printf '%-32s' "$1"; shift; out=$(curl -sf $H "$@") && echo "ok  ${out:0:90}" || { echo "FAIL"; exit 1; }; }
check "GET /api/status"      http://$IMLS_ADDR/api/status
check "GET /api/themes"      http://$IMLS_ADDR/api/themes
check "GET /api/rules"       http://$IMLS_ADDR/api/rules
check "POST /api/rules/try"  -d '{"prompt":"/fable 这个系统的架构怎么设计"}' http://$IMLS_ADDR/api/rules/try
check "POST /api/quick/preview" -d '{"prompt":"给公众号做一张封面"}' http://$IMLS_ADDR/api/quick/preview
check "GET /api/accounts"    http://$IMLS_ADDR/api/accounts
check "GET /api/overview"    "http://$IMLS_ADDR/api/overview?range=7d"
check "GET /api/integrations" http://$IMLS_ADDR/api/integrations
check "GET /api/settings"    http://$IMLS_ADDR/api/settings
check "GET /api/media"       http://$IMLS_ADDR/api/media
check "GET /api/onboarding"  http://$IMLS_ADDR/api/onboarding
KEY=$(python3 -c "import yaml,sys;print(yaml.safe_load(open('$IMLS_DATA_DIR/config.yaml'))['settings']['facade_key'])" 2>/dev/null || grep facade_key "$IMLS_DATA_DIR/config.yaml" | awk '{print $2}')
printf '%-32s' "GET /v1/models (bearer)"; curl -sf -H "Authorization: Bearer $KEY" http://$IMLS_ADDR/v1/models | head -c 80; echo
printf '%-32s' "GET /v1/models (no key → 401)"; code=$(curl -s -o /dev/null -w '%{http_code}' http://$IMLS_ADDR/v1/models); [ "$code" = 401 ] && echo ok || { echo "FAIL $code"; exit 1; }
echo "冒烟通过"
