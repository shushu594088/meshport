#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source ./install.sh
workspace=$(mktemp -d)
trap 'rm -rf -- "$workspace"' EXIT
[[ $(meshport_arch x86_64) == amd64 ]]
[[ $(meshport_arch aarch64) == arm64 ]]
if meshport_arch mips >/dev/null 2>&1; then echo 'unsupported architecture accepted'; exit 1; fi
[[ $(meshport_asset deploy/install-targets.tsv ubuntu 24.04 amd64 0.1.0) == meshport-0.1.0-linux-amd64.tar.gz ]]
[[ $(meshport_asset deploy/install-targets.tsv debian 12 arm64 0.1.0) == meshport-0.1.0-linux-arm64.tar.gz ]]
if meshport_asset deploy/install-targets.tsv centos 9 amd64 0.1.0 >/dev/null 2>&1; then echo 'unsupported OS accepted'; exit 1; fi
cat > "$workspace/future.tsv" <<'EOF'
# meshport-install-targets-v1
centos	*	amd64	meshport-{version}-linux-centos-amd64.tar.gz
centos	9	amd64	meshport-{version}-linux-centos9-amd64.tar.gz
centos	9.4	amd64	meshport-{version}-linux-centos94-amd64.tar.gz
EOF
[[ $(meshport_asset "$workspace/future.tsv" centos 9.4 amd64 0.2.0) == meshport-0.2.0-linux-centos94-amd64.tar.gz ]]
[[ $(meshport_asset "$workspace/future.tsv" centos 9.5 amd64 0.2.0) == meshport-0.2.0-linux-centos9-amd64.tar.gz ]]
[[ $(meshport_asset "$workspace/future.tsv" centos 8 amd64 0.2.0) == meshport-0.2.0-linux-centos-amd64.tar.gz ]]
printf 'package payload\n' > "$workspace/meshport-test.tar.gz"
(cd "$workspace" && sha256sum meshport-test.tar.gz > SHA256SUMS)
meshport_verify "$workspace" meshport-test.tar.gz
# Windows-built releases can use CRLF in their checksum list.
sed 's/$/\r/' "$workspace/SHA256SUMS" > "$workspace/checksums-crlf"
mv "$workspace/checksums-crlf" "$workspace/SHA256SUMS"
meshport_verify "$workspace" meshport-test.tar.gz
printf 'tampered\n' >> "$workspace/meshport-test.tar.gz"
if meshport_verify "$workspace" meshport-test.tar.gz >/dev/null 2>&1; then echo 'tampered package accepted'; exit 1; fi
cat > "$workspace/unsafe.tsv" <<'EOF'
# meshport-install-targets-v1
ubuntu	*	amd64	../../escape.tar.gz
EOF
if meshport_asset "$workspace/unsafe.tsv" ubuntu 24.04 amd64 0.1.0 >/dev/null 2>&1; then echo 'unsafe archive path accepted'; exit 1; fi
echo 'PASS: bootstrap selection, future distro/version routing and checksum failure protection'
