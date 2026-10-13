#!/usr/bin/env bash
# Stable bootstrap entry. Payload installers and target selection live in each Release.
set -euo pipefail

meshport_error() { printf 'Meshport：%s\n' "$*" >&2; }
meshport_usage() {
 cat <<'EOF'
Meshport 一键安装
用法：sudo bash install.sh [--port 8888] [--version v0.1.0]
默认从 GitHub 下载最新稳定 Release，按系统、版本及架构选择安装包。
重复安装不会覆盖已有程序或数据；升级请先使用保留数据的卸载方式。
EOF
}
meshport_arch() {
 case "$1" in x86_64|amd64) printf 'amd64\n';; aarch64|arm64) printf 'arm64\n';; *) meshport_error "暂不支持处理器架构：$1"; return 1;; esac
}
meshport_asset() {
 local manifest=$1 distro=$2 os_version=$3 arch=$4 release_version=$5
 local major=${os_version%%.*} target_distro target_version target_arch template extra score best_score=0 best_asset=''
 IFS= read -r header < "$manifest"
 [[ $header == '# meshport-install-targets-v1' ]] || { meshport_error '安装清单格式不受支持'; return 1; }
 while IFS=$'\t' read -r target_distro target_version target_arch template extra || [[ -n ${target_distro:-} ]]; do
  [[ -n $target_distro && $target_distro != \#* ]] || continue
  [[ $target_distro == "$distro" && $target_arch == "$arch" ]] || continue
  [[ -z $extra ]] || { meshport_error '安装清单字段数量无效'; return 1; }
  if [[ $target_version == "$os_version" ]]; then score=3
  elif [[ $target_version == "$major" ]]; then score=2
  elif [[ $target_version == '*' ]]; then score=1
  else continue; fi
  local candidate=${template//\{version\}/$release_version}
  [[ $candidate =~ ^[a-zA-Z0-9][a-zA-Z0-9._+-]*\.tar\.gz$ ]] || { meshport_error '安装包文件名无效'; return 1; }
  if (( score > best_score )); then best_score=$score; best_asset=$candidate
  elif (( score == best_score )) && [[ $candidate != "$best_asset" ]]; then meshport_error '安装清单存在冲突'; return 1; fi
 done < "$manifest"
 [[ -n $best_asset ]] || { meshport_error "当前 Release 暂无适用于 $distro $os_version / $arch 的安装包"; return 1; }
 printf '%s\n' "$best_asset"
}
meshport_verify() {
 local directory=$1 asset=$2 hash name extra selected_hash=''
 while read -r hash name extra || [[ -n ${hash:-} ]]; do
  name=${name%$'\r'}
  name=${name#\*}
  [[ $name == "$asset" ]] || continue
  [[ $hash =~ ^[a-fA-F0-9]{64}$ && -z $extra && -z $selected_hash ]] || { meshport_error '安装包校验清单无效'; return 1; }
  selected_hash=$hash
 done < "$directory/SHA256SUMS"
 [[ -n $selected_hash ]] || { meshport_error '校验清单中没有对应的安装包'; return 1; }
 (cd -- "$directory" && printf '%s  %s\n' "$selected_hash" "$asset" | sha256sum --check --status -) || { meshport_error '安装包 SHA-256 校验失败，已取消安装'; return 1; }
}
meshport_dependencies() {
 local missing=false command
 for command in curl gzip sha256sum ip sudo tar visudo useradd; do
  command -v "$command" >/dev/null || missing=true
 done
 if [[ $missing == true ]]; then
  printf '正在安装必要的系统依赖…\n'
  if command -v apt-get >/dev/null; then
   apt-get update
   DEBIAN_FRONTEND=noninteractive apt-get install -y curl gzip coreutils iproute2 sudo ca-certificates tar passwd
  elif command -v dnf >/dev/null; then
   dnf install -y curl gzip coreutils iproute sudo ca-certificates tar shadow-utils
  elif command -v yum >/dev/null; then
   yum install -y curl gzip coreutils iproute sudo ca-certificates tar shadow-utils
  else meshport_error '没有可用的 apt-get、dnf 或 yum，请先安装系统依赖'; return 1; fi
 fi
}
meshport_download() {
 curl --fail --location --silent --show-error --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 --connect-timeout 15 --max-time 300 "$1" --output "$2"
}
meshport_main() (
 local port=8888 requested_version=latest
 while (( $# )); do
  case "$1" in
   --port) (( $# >= 2 )) || { meshport_error '--port 缺少值'; return 1; }; port=$2; shift 2;;
   --version) (( $# >= 2 )) || { meshport_error '--version 缺少值'; return 1; }; requested_version=$2; shift 2;;
   -h|--help) meshport_usage; return 0;;
   *) meshport_error "未知参数：$1"; meshport_usage; return 1;;
  esac
 done
 [[ $EUID -eq 0 ]] || { meshport_error '请通过 sudo bash 运行'; return 1; }
 [[ $port =~ ^[0-9]{4,5}$ && $port -ge 1024 && $port -le 65535 && $port -ne 7890 && $port -ne 7891 && $port -ne 9090 && $port -ne 1053 ]] || { meshport_error '无效的 Web 端口'; return 1; }
 [[ -r /etc/os-release ]] || { meshport_error '无法识别操作系统'; return 1; }
 . /etc/os-release
 local distro=${ID:-unknown} os_version=${VERSION_ID:-unknown}
 [[ -d /run/systemd/system ]] || { meshport_error '需要运行中的 systemd'; return 1; }
 [[ ! -e /usr/bin/meshport ]] || { meshport_error '已安装 Meshport；如需升级，请先保留数据卸载。现有服务和数据未改动。'; return 1; }
 local arch release_root='https://github.com/shushu594088/meshport/releases' tag effective_url
 arch=$(meshport_arch "$(uname -m)")
 # curl is needed to fetch this entry in the recommended command; handle local-file invocation too.
 meshport_dependencies
 if [[ $requested_version == latest ]]; then
  effective_url=$(curl --fail --head --location --silent --show-error --proto '=https' --proto-redir '=https' --tlsv1.2 --retry 3 --connect-timeout 15 --max-time 120 --output /dev/null --write-out '%{url_effective}' "$release_root/latest")
  [[ $effective_url == "$release_root/tag/"* ]] || { meshport_error '无法确定最新稳定 Release'; return 1; }
  tag=${effective_url##*/}
 else tag=v${requested_version#v}; fi
 [[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9][a-zA-Z0-9.-]*)?$ ]] || { meshport_error 'Release 版本格式无效'; return 1; }
 local version=${tag#v} download_root="$release_root/download/$tag" workspace asset
 workspace=$(mktemp -d -t meshport-install.XXXXXXXX)
 trap 'rm -rf -- "$workspace"' EXIT
 printf '系统：%s %s；架构：%s；安装版本：%s\n' "$distro" "$os_version" "$arch" "$tag"
 meshport_download "$download_root/INSTALL-TARGETS.tsv" "$workspace/INSTALL-TARGETS.tsv"
 asset=$(meshport_asset "$workspace/INSTALL-TARGETS.tsv" "$distro" "$os_version" "$arch" "$version")
 printf '从 GitHub 下载：%s\n' "$asset"
 meshport_download "$download_root/$asset" "$workspace/$asset"
 meshport_download "$download_root/SHA256SUMS" "$workspace/SHA256SUMS"
 meshport_verify "$workspace" "$asset"
 mkdir "$workspace/package"
 tar --extract --gzip --file "$workspace/$asset" --directory "$workspace/package" --no-same-owner
 [[ -f "$workspace/package/install.sh" && -f "$workspace/package/uninstall.sh" ]] || { meshport_error '安装包缺少安装或卸载脚本'; return 1; }
 bash "$workspace/package/install.sh" "$port"
 # Keep the matching uninstaller available after the download workspace is removed.
 cat "$workspace/package/uninstall.sh" > "$workspace/uninstall-entry"
 printf '\nrm -f -- /usr/libexec/meshport-uninstall\n' >> "$workspace/uninstall-entry"
 install -m 755 -o root -g root "$workspace/uninstall-entry" /usr/libexec/meshport-uninstall
 printf '安装完成。卸载命令：sudo bash /usr/libexec/meshport-uninstall\n'
)

if [[ -z ${BASH_SOURCE[0]:-} || ${BASH_SOURCE[0]} == "$0" ]]; then meshport_main "$@"; fi
