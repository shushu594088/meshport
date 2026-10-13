#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
npm --prefix web ci
npm --prefix web run build
bash scripts/bootstrap-test.sh
go test ./...
for arch in amd64 arm64; do
 dir="build/meshport-linux-$arch"
 mkdir -p "$dir"
 CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "$dir/meshport" ./cmd/meshport
 CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "$dir/meshport-helper" ./cmd/meshport-helper
 cp scripts/install.sh scripts/uninstall.sh deploy/*.service "$dir/"
 cp README.md THIRD_PARTY_NOTICES.txt "$dir/"
 cp -r docs "$dir/"
 printf '%s\n' "$arch" > "$dir/ARCH"
 tar -czf "build/meshport-0.1.0-linux-$arch.tar.gz" -C "$dir" .
done
cp deploy/install-targets.tsv build/INSTALL-TARGETS.tsv
(cd build && sha256sum meshport-0.1.0-linux-*.tar.gz > SHA256SUMS)
