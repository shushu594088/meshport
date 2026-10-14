#!/usr/bin/env bash
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo '请使用 sudo 运行安装脚本'; exit 1; }
source /etc/os-release
case "$ID" in ubuntu|debian) ;; *) echo '当前安装包仅支持 Ubuntu / Debian'; exit 1;; esac
[[ -d /run/systemd/system ]] || { echo '需要运行中的 systemd'; exit 1; }
case "$(uname -m)" in x86_64) arch=amd64; core_arch=amd64-compatible; core_sha=ba3ce607747a07f948fc35780e108a4a7c7f552a38b9bd4d115f313ebcb89c20;; aarch64|arm64) arch=arm64; core_arch=arm64; core_sha=9dd862e28b46ff7d775f169cceebc28deccaa0a9e804237d421cd2571e0caba0;; *) echo '仅支持 amd64 / arm64';exit 1;; esac
base=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
[[ -f "$base/meshport" && -f "$base/meshport-helper" && -f "$base/ARCH" && $(cat "$base/ARCH") == "$arch" ]] || { echo "请下载匹配 $arch 的完整发布包";exit 1; }
[[ ! -e /usr/bin/meshport ]] || { echo '已安装 Meshport。请先卸载（默认保留数据），再安装新版本。';exit 1; }
port=${1:-8888}
[[ $port =~ ^[0-9]+$ && $port -ge 1024 && $port -le 65535 && $port -ne 7890 && $port -ne 7891 && $port -ne 9090 && $port -ne 1053 ]] || { echo '无效的 Web 端口'; exit 1; }
for cmd in curl gzip sha256sum ip sudo; do command -v "$cmd" >/dev/null || { echo "缺少 $cmd，请先 apt install curl gzip coreutils iproute2 sudo ca-certificates"; exit 1; }; done
tmp=$(mktemp -d)
installed=false
complete=false
cleanup_install() {
 if [[ $installed == true && $complete != true ]]; then
  systemctl disable --now meshport.service meshport-mihomo.service 2>/dev/null || true
  rm -f -- /usr/bin/meshport /usr/bin/meshport-mihomo /usr/libexec/meshport-helper /etc/systemd/system/meshport.service /etc/systemd/system/meshport-mihomo.service /etc/sudoers.d/meshport
  systemctl daemon-reload
  echo '安装未完成，已移除不完整的程序和服务；用户数据已保留。' >&2
 fi
 rm -rf -- "$tmp"
}
trap cleanup_install EXIT
core_version=v1.19.32
asset="mihomo-linux-${core_arch}-${core_version}.gz"
if [[ -f "$base/$asset" ]]; then cp -- "$base/$asset" "$tmp/core.gz";else curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --silent --show-error "https://github.com/MetaCubeX/mihomo/releases/download/${core_version}/${asset}" -o "$tmp/core.gz";fi
echo "$core_sha  $tmp/core.gz" | sha256sum --check --status || { echo 'Mihomo 校验失败';exit 1; }
gzip -dc "$tmp/core.gz" > "$tmp/mihomo"
chmod 755 "$tmp/mihomo" "$base/meshport" "$base/meshport-helper"
"$tmp/mihomo" -v >/dev/null
"$base/meshport" version >/dev/null
"$base/meshport" check-port --port "$port"
id meshport &>/dev/null || useradd --system --home-dir /var/lib/meshport --shell /usr/sbin/nologin meshport
install -d -m 750 -o root -g meshport /etc/meshport
install -d -m 700 -o meshport -g meshport /var/lib/meshport
install -d -m 755 /usr/libexec
installed=true
install -m 755 "$base/meshport" /usr/bin/meshport
install -m 755 "$base/meshport-helper" /usr/libexec/meshport-helper
install -m 755 "$tmp/mihomo" /usr/bin/meshport-mihomo
install -m 644 "$base/meshport.service" /etc/systemd/system/meshport.service
install -m 644 "$base/meshport-mihomo.service" /etc/systemd/system/meshport-mihomo.service
cat > "$tmp/sudoers" <<'EOF'
meshport ALL=(root) NOPASSWD: /usr/libexec/meshport-helper start, /usr/libexec/meshport-helper stop, /usr/libexec/meshport-helper restart, /usr/libexec/meshport-helper enable, /usr/libexec/meshport-helper disable, /usr/libexec/meshport-helper status, /usr/libexec/meshport-helper preflight, /usr/libexec/meshport-helper arm, /usr/libexec/meshport-helper disarm, /usr/libexec/meshport-helper update
EOF
visudo -cf "$tmp/sudoers" >/dev/null
install -m 440 "$tmp/sudoers" /etc/sudoers.d/meshport
if [[ ! -e /etc/meshport/environment ]]; then printf 'MESHPORT_HOST=0.0.0.0\nMESHPORT_SECURE_COOKIE=false\n' > /etc/meshport/environment;chmod 640 /etc/meshport/environment;fi
sudo -u meshport /usr/bin/meshport init >/dev/null
# Initial port is consumed only on a fresh state; reinstall preserves existing settings.
port=$(sudo -u meshport /usr/bin/meshport configure --port "$port")
systemctl daemon-reload
if [[ $(sudo -u meshport /usr/bin/meshport autostart) == true ]]; then systemctl enable meshport-mihomo.service >/dev/null;else systemctl disable meshport-mihomo.service >/dev/null 2>&1 || true;fi
systemctl enable --now meshport.service >/dev/null
systemctl is-active --quiet meshport.service
complete=true
printf '\nMeshport 已安装。访问：http://服务器地址:%s\n代理与 TUN 默认关闭。\n' "$port"
if [[ -f /var/lib/meshport/init-token ]]; then printf '一次性初始化凭证：';cat /var/lib/meshport/init-token;else echo '保留了已有管理员密码，请使用原密码登录。';fi
