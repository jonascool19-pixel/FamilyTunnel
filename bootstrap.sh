#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Root-Rechte erforderlich.' >&2; exit 1; }
: "${FT_BUNDLE_URL:?FT_BUNDLE_URL muss auf familytunnel-0.1.0-install.tar.gz zeigen}"
tmp=$(mktemp -d /var/tmp/familytunnel-bootstrap.XXXXXX)
trap 'rm -rf -- "$tmp"' EXIT
curl --proto '=https' --tlsv1.2 -fL --retry 3 "$FT_BUNDLE_URL" -o "$tmp/bundle.tar.gz"
curl --proto '=https' --tlsv1.2 -fL --retry 3 "$FT_BUNDLE_URL.sha256" -o "$tmp/bundle.tar.gz.sha256"
expected=$(awk 'NR==1 {print $1}' "$tmp/bundle.tar.gz.sha256")
printf '%s  %s\n' "$expected" "$tmp/bundle.tar.gz" | sha256sum -c -
tar -tzf "$tmp/bundle.tar.gz" | grep -Eq '(^/|(^|/)\.\.(/|$))' && { echo 'Unsicheres Archiv.' >&2; exit 1; } || true
tar -C "$tmp" -xzf "$tmp/bundle.tar.gz" --no-same-owner --no-same-permissions
exec bash "$tmp/familytunnel/install.sh"
