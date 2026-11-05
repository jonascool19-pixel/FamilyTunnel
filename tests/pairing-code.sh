#!/usr/bin/env bash
set -Eeuo pipefail
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
cat > "$tmp/pairing.json" <<'EOF'
{"endpoint":"203.0.113.10","server_public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","client_private_key":"BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA=","token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
EOF
code=$(base64 -w0 "$tmp/pairing.json")
printf '%s' "$code" | base64 -d > "$tmp/decoded.json"
cmp "$tmp/pairing.json" "$tmp/decoded.json"
if printf '%s' "${code:0:40}" | base64 -d 2>/dev/null | python3 -m json.tool >/dev/null 2>&1; then
 echo 'Abgeschnittener Pairing-Code wurde fälschlich akzeptiert.' >&2
 exit 1
fi
