#!/usr/bin/env bash
# GithubImagePush 一键安装/更新脚本
#
# 首次安装或更新到最新版（保留服务器上的 config.yaml）：
#   curl -fsSL https://raw.githubusercontent.com/JeckChen666/GithubImagePush/main/install.sh | sudo bash
#
# 行为说明：
#   - 从 GitHub Releases 下载与本机架构匹配的最新二进制（linux amd64/arm64）
#   - 首次安装：下载 config.example.yaml 作为起始配置，注册 systemd 服务并设开机自启
#   - 更新：只替换二进制，config.yaml 原样保留
set -euo pipefail

REPO="JeckChen666/GithubImagePush"
BINARY="github-image-push"
APP_DIR="/app/GithubImagePush"
SERVICE="github-image-push"
CONFIG_EXAMPLE="config.example.yaml"
MEM_HIGH="200M"
MEM_MAX="400M"

log() { echo "==> $*"; }
die() { echo "错误：$*" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || die "本脚本仅支持 Linux 服务器"
[ "$(id -u)" = "0" ] || die "请用 root 运行（sudo bash）"

case "$(uname -m)" in
    x86_64)          ARCH="amd64" ;;
    aarch64 | arm64) ARCH="arm64" ;;
    *) die "不支持的架构：$(uname -m)" ;;
esac

fetch() { # fetch <url> <输出文件>
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1"
    else
        die "系统缺少 curl 或 wget，请先安装"
    fi
}

# 通过 releases/latest 页面解析最新 tag（不占 GitHub API 匿名配额）
log "查询最新版本..."
TAG=$(fetch "https://github.com/$REPO/releases/latest" - 2>/dev/null |
    grep -o 'releases/tag/v[0-9.]*' | head -1 | cut -d/ -f3 || true)
if [ -z "${TAG:-}" ] && command -v curl >/dev/null 2>&1; then
    TAG=$(curl -fsSI -o /dev/null -w '%{redirect_url}' "https://github.com/$REPO/releases/latest" |
        grep -o 'releases/tag/v[0-9.]*' | head -1 | cut -d/ -f3 || true)
fi
[ -n "${TAG:-}" ] || die "无法获取最新版本号（检查网络或仓库 Releases）"
log "最新版本：$TAG"

URL="https://github.com/$REPO/releases/download/$TAG/$BINARY-linux-$ARCH"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

log "下载 $URL"
fetch "$URL" "$TMP_DIR/$BINARY"
chmod +x "$TMP_DIR/$BINARY"

FIRST_INSTALL=0
mkdir -p "$APP_DIR"
if [ ! -f "$APP_DIR/config.yaml" ]; then
    FIRST_INSTALL=1
    log "首次安装：下载默认配置到 $APP_DIR/config.yaml（请填写 GitHub token/repo 后重启服务）"
    fetch "https://raw.githubusercontent.com/$REPO/main/$CONFIG_EXAMPLE" "$APP_DIR/config.yaml" ||
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
