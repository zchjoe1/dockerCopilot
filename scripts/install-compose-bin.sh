#!/bin/bash
# 安装 dc 的 Compose 功能所需的 docker-compose 静态二进制（幂等）
#
# 为什么需要：镜像里不含 docker CLI/compose（有意如此，避免为它重建镜像），
# 而 internal/utiles/compose.go 直接 exec 宿主上的这个文件：
#     /vol1/1000/docker/.dockercopilot/docker-compose
# 文件不存在 ⇒ ComposeAvailable()=false ⇒ 接口返回 available:false ⇒ **前端把 Compose 标签整个隐藏**。
#
# 用法：sudo bash scripts/install-compose-bin.sh
# 环境变量：
#   COMPOSE_BIN  安装路径（默认 /vol1/1000/docker/.dockercopilot/docker-compose）
#   HTTP_PROXY   下载代理（默认尝试 http://127.0.0.1:7890，失败则直连）
set -uo pipefail

BIN="${COMPOSE_BIN:-/vol1/1000/docker/.dockercopilot/docker-compose}"
URL="https://github.com/docker/compose/releases/latest/download/docker-compose-linux-$(uname -m)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [ -x "$BIN" ]; then
  echo "[compose-bin] 已存在：$BIN"
  "$BIN" version 2>&1 | head -1
  exit 0
fi

mkdir -p "$(dirname "$BIN")"

fetch() {
  local proxy="$1" out="$2"
  if [ -n "$proxy" ]; then
    curl -fsSL --max-time 180 -x "$proxy" "$URL" -o "$out"
  else
    curl -fsSL --max-time 180 "$URL" -o "$out"
  fi
}

echo "[compose-bin] 下载 $URL"
if fetch "${HTTP_PROXY:-http://127.0.0.1:7890}" "$TMP/dc"; then
  echo "[compose-bin] 经代理下载成功"
elif fetch "" "$TMP/dc"; then
  echo "[compose-bin] 直连下载成功"
else
  echo "[compose-bin] 下载失败：请检查网络/代理，或手动放置到 $BIN" >&2
  exit 1
fi

# 基本校验：必须是可执行文件
if ! head -c 4 "$TMP/dc" | grep -q $'\x7fELF'; then
  echo "[compose-bin] 下载内容不是 ELF 二进制（可能被代理拦截），放弃安装" >&2
  exit 1
fi

install -m 755 "$TMP/dc" "$BIN"
echo "[compose-bin] 已安装：$BIN"
"$BIN" version 2>&1 | head -1
