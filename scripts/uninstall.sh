#!/usr/bin/env bash
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo '请使用 sudo 运行卸载脚本'; exit 1; }
[[ $# -eq 0 || ($# -eq 1 && $1 == --purge) ]] || { echo '用法：sudo bash uninstall.sh [--purge]';exit 1; }
systemctl stop meshport-update.service 2>/dev/null || true
systemctl stop meshport-tun-rollback.timer meshport-tun-rollback.service 2>/dev/null || true
systemctl disable --now meshport.service meshport-mihomo.service 2>/dev/null || true
if [[ -x /usr/libexec/meshport-helper ]]; then /usr/libexec/meshport-helper cleanup;fi
rm -f -- /usr/bin/meshport /usr/bin/meshport-mihomo /usr/libexec/meshport-helper /etc/systemd/system/meshport.service /etc/systemd/system/meshport-mihomo.service /etc/sudoers.d/meshport
systemctl daemon-reload
systemctl reset-failed 2>/dev/null || true
if [[ ${1:-} == --purge ]]; then rm -rf -- /var/lib/meshport /etc/meshport;userdel meshport 2>/dev/null || true;echo '已卸载并清除 Meshport 数据。';else echo '已卸载。用户数据和配置保留在 /var/lib/meshport 与 /etc/meshport。';fi
