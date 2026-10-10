#!/usr/bin/env bash
# GithubImagePush 一键安装/更新脚本
#
# 推荐用法（Gitee 镜像直连，国内速度 MB 级；脚本本体走 jsdelivr）：
#   curl -fsSL https://cdn.jsdelivr.net/gh/JeckChen666/GithubImagePush@main/install.sh | GITEE_TOKEN=<Gitee私人令牌> bash
# 备选一（脚本本体也从 Gitee 取，全程不碰 GitHub）：
#   curl -fsSL -H "Authorization: token <Gitee私人令牌>" https://gitee.com/jeckchen666/GithubImagePush/raw/main/install.sh | GITEE_TOKEN=<Gitee私人令牌> bash
# 备选二（无令牌：GitHub 慢速直连或走镜像，仅公开仓库可用）：
#   curl -fsSL https://cdn.jsdelivr.net/gh/JeckChen666/GithubImagePush@main/install.sh | bash
#   curl -fsSL https://cdn.jsdelivr.net/gh/JeckChen666/GithubImagePush@main/install.sh | MIRROR=https://ghproxy.net/ bash
#
# 行为说明：
#   - 下载源优先级：Gitee Release（GITEE_TOKEN）→ GitHub 直链（可选 MIRROR）→ GitHub API（GH_TOKEN，私有仓库）
#   - 首次安装：下载 config.example.yaml 作为起始配置，注册 systemd 服务并设开机自启
#   - 更新：只替换二进制，config.yaml 原样保留
set -euo pipefail

REPO_GH="JeckChen666/GithubImagePush"
REPO_GITEE="jeckchen666/GithubImagePush"
BINARY="github-image-push"
APP_DIR="/app/GithubImagePush"
SERVICE="github-image-push"
CONFIG_EXAMPLE="config.example.yaml"
MEM_HIGH="200M"
MEM_MAX="400M"

log() { echo "==> $*"; }
die() { echo "错误：$*" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || die "本脚本仅支持 Linux 服务器"
[ "$(id -u)" = "0" ] || die "请用 root 运行"

case "$(uname -m)" in
    x86_64)          ARCH="amd64" ;;
    aarch64 | arm64) ARCH="arm64" ;;
    *) die "不支持的架构：$(uname -m)" ;;
esac

ASSET="$BINARY-linux-$ARCH"

fetch() { # fetch <url> <输出文件>（带超时、断点续传、自动重试）
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 15 --max-time 600 \
            --retry 5 --retry-delay 2 --retry-all-errors -C - "$1" -o "$2"
    else
        wget -q --timeout=15 --tries=5 -c -O "$2" "$1"
    fi
}

CURL_COMMON=(-fL --progress-bar --connect-timeout 15 --max-time 600 --retry 5 --retry-delay 2 --retry-all-errors)

# ———————— 解析最新版本 ————————
if [ -n "${GITEE_TOKEN:-}" ]; then
    log "通过 Gitee 查询最新版本..."
    TAG=$(curl -fsSL --connect-timeout 15 -H "Authorization: token $GITEE_TOKEN" \
        "https://gitee.com/api/v5/repos/$REPO_GITEE/releases/latest" 2>/dev/null |
        grep -m1 '"tag_name"' | cut -d'"' -f4 || true)
elif [ -n "${GH_TOKEN:-}" ]; then
    log "通过 GitHub API 查询最新版本..."
    TAG=$(curl -fsSL --connect-timeout 15 -H "Authorization: Bearer $GH_TOKEN" \
        "https://api.github.com/repos/$REPO_GH/releases/latest" 2>/dev/null |
        grep -m1 '"tag_name"' | cut -d'"' -f4 || true)
else
    log "查询最新版本（匿名）..."
    TAG=$(fetch "https://github.com/$REPO_GH/releases/latest" - 2>/dev/null |
        grep -o 'releases/tag/v[0-9.]*' | head -1 | cut -d/ -f3 || true)
    if [ -z "${TAG:-}" ] && command -v curl >/dev/null 2>&1; then
        TAG=$(curl -fsSI -o /dev/null -w '%{redirect_url}' "https://github.com/$REPO_GH/releases/latest" |
            grep -o 'releases/tag/v[0-9.]*' | head -1 | cut -d/ -f3 || true)
    fi
fi
[ -n "${TAG:-}" ] || die "无法获取最新版本号（检查网络或令牌）"
log "最新版本：$TAG"

# ———————— 下载二进制（Gitee 优先）———————
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

if [ -n "${GITEE_TOKEN:-}" ]; then
    URL="https://gitee.com/$REPO_GITEE/releases/download/$TAG/$ASSET"
    log "从 Gitee 镜像下载（国内直连）..."
    curl "${CURL_COMMON[@]}" -H "Authorization: token $GITEE_TOKEN" "$URL" -o "$TMP_DIR/$BINARY" ||
        die "Gitee 下载失败：$URL（确认令牌有效、仓库与 Release 存在）"
elif [ -n "${GH_TOKEN:-}" ]; then
    log "通过 GitHub API 下载（私有仓库模式）..."
    ASSET_ID=$(curl -fsSL --connect-timeout 15 -H "Authorization: Bearer $GH_TOKEN" \
        "https://api.github.com/repos/$REPO_GH/releases/tags/$TAG" |
        grep -A8 "\"name\": \"$ASSET\"" | grep -m1 '"id":' | grep -o '[0-9]\+' || true)
    [ -n "${ASSET_ID:-}" ] || die "未在 $TAG 中找到资产 $ASSET"
    curl "${CURL_COMMON[@]}" -H "Authorization: Bearer $GH_TOKEN" -H "Accept: application/octet-stream" \
        "https://api.github.com/repos/$REPO_GH/releases/assets/$ASSET_ID" -o "$TMP_DIR/$BINARY" ||
        die "GitHub API 下载失败：确认 GH_TOKEN 对 $REPO_GH 有读权限"
else
    URL="${MIRROR:-}https://github.com/$REPO_GH/releases/download/$TAG/$ASSET"
    log "从 GitHub 下载..."
    if command -v curl >/dev/null 2>&1; then
        curl "${CURL_COMMON[@]}" -C - "$URL" -o "$TMP_DIR/$BINARY" ||
            die "下载失败：$URL（国内高峰期建议配置 GITEE_TOKEN 走镜像，见脚本头注释）"
    else
        fetch "$URL" "$TMP_DIR/$BINARY" || die "下载失败：$URL"
    fi
fi
chmod +x "$TMP_DIR/$BINARY"

# ———————— 部署 ————————
FIRST_INSTALL=0
mkdir -p "$APP_DIR"
if [ ! -f "$APP_DIR/config.yaml" ]; then
    FIRST_INSTALL=1
    log "首次安装：下载默认配置到 $APP_DIR/config.yaml（请填写 GitHub token/repo 后重启服务）"
    fetch "https://raw.githubusercontent.com/$REPO_GH/main/$CONFIG_EXAMPLE" "$APP_DIR/config.yaml" ||
        fetch "https://gitee.com/$REPO_GITEE/raw/main/$CONFIG_EXAMPLE" "$APP_DIR/config.yaml" ||
        die "下载默认配置失败，请手动创建 $APP_DIR/config.yaml（参考仓库 config.example.yaml）"
    chmod 600 "$APP_DIR/config.yaml"
else
    log "检测到现有配置，更新模式：config.yaml 保持不动"
fi

log "写入 systemd 服务 $SERVICE.service"
cat > "/etc/systemd/system/$SERVICE.service" <<UNIT
[Unit]
Description=GithubImagePush GitHub 图床服务
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/$BINARY -config $APP_DIR/config.yaml
Restart=on-failure
RestartSec=3
MemoryHigh=$MEM_HIGH
MemoryMax=$MEM_MAX
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT

log "替换二进制并重启服务..."
systemctl stop "$SERVICE" 2>/dev/null || true
install -m 0755 "$TMP_DIR/$BINARY" "$APP_DIR/$BINARY"
systemctl daemon-reload
systemctl enable --now "$SERVICE" >/dev/null 2>&1 || true
sleep 1

if systemctl is-active --quiet "$SERVICE"; then
    VER=$("$APP_DIR/$BINARY" -version 2>/dev/null | awk '{print $2}' || echo "$TAG")
    log "✔ $SERVICE 已运行，版本 $VER"
    if [ "$FIRST_INSTALL" = "1" ]; then
        log "后续步骤：编辑 $APP_DIR/config.yaml 填写 GitHub token/repo，然后 systemctl restart $SERVICE"
    fi
else
    die "服务未能启动，查看日志：journalctl -u $SERVICE -n 50 --no-pager"
fi
