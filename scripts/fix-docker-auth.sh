#!/bin/bash
# fnOS 会周期性重写 /root/.docker/config.json（只留它自己的 X-Meta-* 头，抹掉 auths）
# ⇒ docker pull / docker compose pull 报 pull access denied，dc 的私仓检测拿不到 scope。
#
# 本脚本把「当前文件」与「凭据备份」合并：保留 fnOS 的 HttpHeaders，补回 auths。
#
# 用法：
#   sudo bash scripts/fix-docker-auth.sh              # 立即修一次
#   sudo bash scripts/fix-docker-auth.sh --install    # 安装 systemd timer（每 5 分钟自愈）
set -uo pipefail

# 2026-11-04：从顶层 .docker/ 挪进 dc 自己的项目目录（与 .bin/ 并列），顶层不再多两个隐藏目录。
# ⚠️ 升级时若旧备份还在 /vol1/1000/docker/.docker/config.json，本脚本会自动搬过来（见 migrate_old）。
CANON_DIR="/vol1/1000/docker/dockercopilot/.creds"
CANON="$CANON_DIR/config.json"
TARGET="/root/.docker/config.json"
SELF="/usr/local/sbin/dc-fix-docker-auth.sh"

OLD_CANON_DIR="/vol1/1000/docker/.docker"
OLD_CANON="$OLD_CANON_DIR/config.json"

# 迁移：旧版本把备份放在顶层 .docker/，挪到项目目录后第一次运行要把它搬过来。
# ⛔ 只在"新位置还没有凭据、旧位置有 auths"时搬，别覆盖已有的新备份。
migrate_old() {
  grep -q '"auths"' "$CANON" 2>/dev/null && return 0
  grep -q '"auths"' "$OLD_CANON" 2>/dev/null || return 0
  mkdir -p "$CANON_DIR" && chmod 700 "$CANON_DIR"
  mv -f "$OLD_CANON" "$CANON" && chmod 600 "$CANON"
  rmdir "$OLD_CANON_DIR" 2>/dev/null || true
  echo "[docker-auth] 已把旧备份 $OLD_CANON 迁到 $CANON"
}

fix_once() {
  migrate_old

  # ① 备份不存在或没有 auths ⇒ 从当前文件抢救
  if ! grep -q '"auths"' "$CANON" 2>/dev/null; then
    mkdir -p "$CANON_DIR" && chmod 700 "$CANON_DIR"
    if grep -q '"auths"' "$TARGET" 2>/dev/null; then
      cp -f "$TARGET" "$CANON" && chmod 600 "$CANON"
      echo "[docker-auth] 已备份凭据到 $CANON"
    else
      echo "[docker-auth] 两份都没有 auths，需要重新 docker login" >&2
      return 1
    fi
  fi

  # ② 已是好的
  if grep -q '"auths"' "$TARGET" 2>/dev/null; then
    return 0
  fi

  # ③ 合并
  python3 - "$TARGET" "$CANON" <<'PY'
import json, sys
target, canon = sys.argv[1], sys.argv[2]
def load(p):
    try:
        with open(p, encoding='utf-8') as f:
            return json.load(f)
    except Exception:
        return {}
cur, saved = load(target), load(canon)
if not saved.get('auths'):
    sys.exit(1)
cur['auths'] = saved['auths']
for k in ('HttpHeaders', 'credsStore', 'credHelpers'):
    if saved.get(k) and not cur.get(k):
        cur[k] = saved[k]
with open(target, 'w', encoding='utf-8') as f:
    json.dump(cur, f)
PY

  chmod 600 "$TARGET" 2>/dev/null
  if grep -q '"auths"' "$TARGET"; then
    echo "[docker-auth] $(date '+%F %T') 已恢复 auths（来源 $CANON）"
  else
    echo "[docker-auth] $(date '+%F %T') 恢复失败" >&2
    return 1
  fi
}

install_timer() {
  install -m 755 "$0" "$SELF" 2>/dev/null || cp -f "$0" "$SELF"
  chmod 755 "$SELF"
  cat > /etc/systemd/system/dc-fix-docker-auth.service <<UNIT
[Unit]
Description=恢复被 fnOS 清空的 /root/.docker/config.json 凭据
After=docker.service

[Service]
Type=oneshot
ExecStart=$SELF
UNIT
  cat > /etc/systemd/system/dc-fix-docker-auth.timer <<'UNIT'
[Unit]
Description=每 5 分钟检查 docker 凭据是否被清空

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min
AccuracySec=30s

[Install]
WantedBy=timers.target
UNIT
  systemctl daemon-reload
  systemctl enable --now dc-fix-docker-auth.timer >/dev/null 2>&1
  echo "[docker-auth] 已安装并启用 dc-fix-docker-auth.timer"
  systemctl list-timers dc-fix-docker-auth.timer --no-pager 2>/dev/null | head -3
}

if [ "${1:-}" = "--install" ]; then
  fix_once
  install_timer
else
  fix_once
fi
