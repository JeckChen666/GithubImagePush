#!/usr/bin/env bash
# GithubImagePush 图床上传脚本 — 供 AI Agent / 命令行调用
# 用法见 --help 或 SKILL.md
set -euo pipefail

# 配置加载顺序：命令行参数 > 环境变量 > ~/.config/github-image-push/env
CONFIG_FILE="${GIP_CONFIG_FILE:-$HOME/.config/github-image-push/env}"
# 先暂存外部环境变量，source 配置文件后以其为兜底（保证环境变量优先）
_env_url="${GIP_URL:-}"; _env_key="${GIP_KEY:-}"
_env_bin="${GIP_BIN:-}"; _env_config="${GIP_CONFIG:-}"
# shellcheck disable=SC1090
[ -f "$CONFIG_FILE" ] && . "$CONFIG_FILE"
GIP_URL="${_env_url:-${GIP_URL:-http://127.0.0.1:8080}}"
GIP_KEY="${_env_key:-${GIP_KEY:-}}"
GIP_BIN="${_env_bin:-${GIP_BIN:-}}"
GIP_CONFIG="${_env_config:-${GIP_CONFIG:-}}"

FORMAT="url"
AUTO_START=0
FILES=()

usage() {
  sed -n '2,4p' "$0" | sed 's/^# //'
  cat <<'EOF'

用法: upload.sh [选项] <图片文件>...

选项:
  -f, --format FMT   输出格式: url(默认) | markdown | html | bbcode | json
  -u, --url URL      服务地址 (默认 $GIP_URL 或 http://127.0.0.1:8080)
  -k, --key KEY      访问 Key (默认 $GIP_KEY)
      --start        服务未运行时自动后台启动 (需 GIP_BIN，可选 GIP_CONFIG)
  -h, --help         显示帮助
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -f|--format) FORMAT="${2:-}"; shift 2 ;;
    -u|--url)    GIP_URL="${2:-}"; shift 2 ;;
    -k|--key)    GIP_KEY="${2:-}"; shift 2 ;;
    --start)     AUTO_START=1; shift ;;
    -h|--help)   usage; exit 0 ;;
    -*)          echo "未知选项: $1" >&2; usage >&2; exit 2 ;;
    *)           FILES+=("$1"); shift ;;
  esac
done

[ "${#FILES[@]}" -gt 0 ] || { echo "错误: 未指定图片文件" >&2; usage >&2; exit 2; }
for f in "${FILES[@]}"; do
  [ -f "$f" ] || { echo "错误: 文件不存在: $f" >&2; exit 2; }
done
[ -n "$GIP_KEY" ] || { echo "错误: 未配置访问 Key（设置 GIP_KEY 或写入 ${CONFIG_FILE}）" >&2; exit 2; }

healthz() { curl -sf -o /dev/null --connect-timeout 2 "${GIP_URL}/healthz"; }

# 服务未运行时按需拉起
if ! healthz; then
  if [ "$AUTO_START" = 1 ]; then
    [ -n "$GIP_BIN" ] || { echo "错误: --start 需要 GIP_BIN（服务二进制路径）" >&2; exit 3; }
    [ -x "$GIP_BIN" ] || { echo "错误: GIP_BIN 不可执行: ${GIP_BIN}" >&2; exit 3; }
    # shellcheck disable=SC2086
    nohup "$GIP_BIN" ${GIP_CONFIG:+-config "$GIP_CONFIG"} >/dev/null 2>&1 &
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      healthz && break
      sleep 0.5
    done
    healthz || { echo "错误: 服务自动启动失败，请检查 GIP_BIN/GIP_CONFIG" >&2; exit 3; }
  else
    echo "错误: 无法连接服务 ${GIP_URL} （未运行？加 --start 或先手动启动）" >&2
    exit 3
  fi
fi

# 组装 multipart 请求（多个 -F 一次上传）
ARGS=(-s -w '\n%{http_code}' -H "X-API-Key: $GIP_KEY")
for f in "${FILES[@]}"; do
  ARGS+=(-F "file=@$f")
done
RESP="$(curl "${ARGS[@]}" "${GIP_URL}/api/upload")"
HTTP="${RESP##*$'\n'}"
BODY="${RESP%$'\n'*}"

if [ "$HTTP" = "401" ]; then
  echo "错误: Key 无效或未提供（HTTP 401），检查 GIP_KEY" >&2; exit 4
elif [ "$HTTP" = "429" ]; then
  echo "错误: 鉴权失败次数过多被限速，请稍后再试" >&2; exit 4
elif [ "$HTTP" != "200" ]; then
  echo "错误: 上传请求失败 (HTTP ${HTTP})" >&2
  echo "$BODY" | head -c 500 >&2; echo >&2
  exit 5
fi

if [ "$FORMAT" = "json" ]; then
  echo "$BODY"
  exit 0
fi

# 解析每文件结果；python3 缺失时退化为正则提取首个 url
if command -v python3 >/dev/null 2>&1; then
  printf '%s' "$BODY" | FORMAT="$FORMAT" python3 -c '
import json, os, sys
fmt = os.environ.get("FORMAT", "url")
try:
    d = json.load(sys.stdin)
except Exception as e:
    print("错误: 响应解析失败: %s" % e, file=sys.stderr); sys.exit(5)
items = d.get("data") or []
pick = {
    "url": lambda i: i.get("url", ""),
    "markdown": lambda i: i.get("markdown", ""),
    "html": lambda i: i.get("html", ""),
    "bbcode": lambda i: i.get("bbcode", ""),
}.get(fmt, lambda i: i.get("url", ""))
ok = [i for i in items if i.get("success")]
fail = [i for i in items if not i.get("success")]
for i in ok:
    print(pick(i))
for i in fail:
    print("失败 %s: %s" % (i.get("name"), i.get("message")), file=sys.stderr)
if not ok:
    print(d.get("message") or "上传失败", file=sys.stderr); sys.exit(1)
'
  exit $?
else
  URL=$(printf '%s' "$BODY" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p' | head -1)
  if [ -n "$URL" ]; then
    echo "$URL"
  else
    echo "错误: 无法解析响应（建议安装 python3）" >&2; exit 5
  fi
fi
