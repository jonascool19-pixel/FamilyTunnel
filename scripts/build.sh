#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/.."
version=${VERSION:-0.1.0}
mkdir -p dist
for arch in amd64 arm64; do
 CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "dist/familytunnel-linux-$arch" ./cmd/familytunnel
done
(cd dist && sha256sum familytunnel-linux-* > SHA256SUMS)
printf '{"version":"%s"}\n' "$version" > dist/release.json
tar -C dist -czf "dist/familytunnel-$version-linux.tar.gz" familytunnel-linux-amd64 familytunnel-linux-arm64 release.json
(cd dist && sha256sum "familytunnel-$version-linux.tar.gz" > "familytunnel-$version-linux.tar.gz.sha256")
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT
mkdir -p "$stage/familytunnel/dist" "$stage/familytunnel/scripts"
cp -a install.sh install-familytunnel.sh install-server.sh install-client.sh bootstrap.sh purge.sh systemd docs README.md TESTING.md SECURITY.md CONTRIBUTING.md "$stage/familytunnel/"
# Only runtime helpers belong in an installer; never copy previews, tests,
# generated Python caches, local state or development tools implicitly.
cp scripts/manage.sh scripts/firewall.sh scripts/raw-firewall.sh scripts/oci-nsg.py scripts/oracle-setup.sh scripts/oracle-permissions.py "$stage/familytunnel/scripts/"
cp dist/familytunnel-linux-amd64 dist/familytunnel-linux-arm64 dist/SHA256SUMS "$stage/familytunnel/dist/"
tar -C "$stage" -czf "dist/familytunnel-$version-install.tar.gz" familytunnel
(cd dist && sha256sum "familytunnel-$version-install.tar.gz" > "familytunnel-$version-install.tar.gz.sha256")
