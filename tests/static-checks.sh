#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/.."
for f in install.sh scripts/*.sh; do bash -n "$f"; done
python3 -m py_compile scripts/oci-nsg.py
if command -v systemd-analyze >/dev/null; then systemd-analyze verify systemd/*.service systemd/*.timer; fi
if [[ -f dist/SHA256SUMS ]]; then (cd dist && sha256sum -c SHA256SUMS); fi
go test ./...
go vet ./...
