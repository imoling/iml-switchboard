#!/usr/bin/env bash
# 把 GitHub 上的发布同步到 Gitee：发布标题、说明、预发布标记和全部安装包。
# 用法：GITEE_TOKEN=... GH_TOKEN=... scripts/sync-gitee.sh [标签]
# 不给标签就同步全部发布。可重复跑：Gitee 上已有的发布只更新说明，已有的同名附件不再上传。
# 标签要先推到 Gitee（.github/workflows/gitee.yml 里先推代码和标签再调本脚本）。
set -euo pipefail

: "${GITEE_TOKEN:?缺少 GITEE_TOKEN}"
GITHUB_REPO=${GITHUB_REPO:-imoling/iml-switchboard}
GITEE_REPO=${GITEE_REPO:-imoling/iml-switchboard}
API="https://gitee.com/api/v5/repos/$GITEE_REPO"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

if [ $# -gt 0 ]; then
  tags=("$@")
else
  mapfile -t tags < <(gh release list -R "$GITHUB_REPO" --limit 100 --json tagName,isDraft -q '.[] | select(.isDraft | not) | .tagName')
fi

failed=0
for tag in "${tags[@]}"; do
  echo "== $tag"
  gh release view "$tag" -R "$GITHUB_REPO" --json name,body,isPrerelease,tagName > "$WORK/gh.json"
  name=$(jq -r .name "$WORK/gh.json")
  body=$(jq -r .body "$WORK/gh.json")
  pre=$(jq -r .isPrerelease "$WORK/gh.json")

  # Gitee 要求发布说明非空
  [ -n "$body" ] || body="Switchboard $tag"

  existing=$(curl -sS "$API/releases/tags/$tag?access_token=$GITEE_TOKEN")
  id=$(jq -r 'if type == "object" then .id // empty else empty end' <<<"$existing")
  if [ -z "$id" ]; then
    resp=$(curl -sS -X POST "$API/releases" \
      --data-urlencode "access_token=$GITEE_TOKEN" \
      --data-urlencode "tag_name=$tag" \
      --data-urlencode "name=$name" \
      --data-urlencode "body=$body" \
      --data-urlencode "prerelease=$pre" \
      --data-urlencode "target_commitish=$tag")
    id=$(jq -r '.id // empty' <<<"$resp")
    [ -n "$id" ] || { echo "创建发布失败：$resp"; failed=1; continue; }
    echo "已创建发布 $id"
  else
    curl -sS -o /dev/null -X PATCH "$API/releases/$id" \
      --data-urlencode "access_token=$GITEE_TOKEN" \
      --data-urlencode "tag_name=$tag" \
      --data-urlencode "name=$name" \
      --data-urlencode "body=$body" \
      --data-urlencode "prerelease=$pre"
    echo "已更新发布 $id"
  fi

  have=$(curl -sS "$API/releases/$id/attach_files?access_token=$GITEE_TOKEN&per_page=100" | jq -r 'if type == "array" then .[].name else empty end')
  rm -rf "$WORK/assets" && mkdir "$WORK/assets"
  gh release download "$tag" -R "$GITHUB_REPO" -D "$WORK/assets"
  for f in "$WORK/assets"/*; do
    base=$(basename "$f")
    if grep -qxF "$base" <<<"$have"; then
      echo "  跳过 $base（已有）"
      continue
    fi
    resp=$(curl -sS -X POST "$API/releases/$id/attach_files" \
      -F "access_token=$GITEE_TOKEN" -F "file=@$f")
    if jq -e '.id' <<<"$resp" >/dev/null 2>&1; then
      echo "  已上传 $base"
    else
      echo "  上传失败 $base：$resp"
      failed=1
    fi
  done
done
exit $failed
