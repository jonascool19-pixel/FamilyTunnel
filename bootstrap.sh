#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Root-Rechte erforderlich.' >&2; exit 1; }
: "${FT_BUNDLE_URL:?FT_BUNDLE_URL muss auf familytunnel-0.1.0-install.tar.gz zeigen}"
tmp=$(mktemp -d /var/tmp/familytunnel-bootstrap.XXXXXX)
trap 'rm -rf -- "$tmp"' EXIT
curl --proto '=https' --tlsv1.2 -4 --connect-timeout 10 --max-time 120 -fL --retry 3 "$FT_BUNDLE_URL" -o "$tmp/bundle.tar.gz"
curl --proto '=https' --tlsv1.2 -4 --connect-timeout 10 --max-time 30 -fL --retry 3 "$FT_BUNDLE_URL.sha256" -o "$tmp/bundle.tar.gz.sha256"
expected=$(awk 'NR==1 {print $1}' "$tmp/bundle.tar.gz.sha256")
[[ $expected =~ ^[a-fA-F0-9]{64}$ ]] || { echo 'Ungültige Paket-Prüfsumme.' >&2; exit 1; }
printf '%s  %s\n' "$expected" "$tmp/bundle.tar.gz" | sha256sum -c -
# Check every entry before root extracts anything. Links and device files are forbidden.
command -v python3 >/dev/null || { apt-get update; apt-get install -y python3; }
python3 - "$tmp/bundle.tar.gz" <<'PYSAFE'
import pathlib,sys,tarfile
seen=set();total=0
with tarfile.open(sys.argv[1],'r:gz') as archive:
    for item in archive:
        name=pathlib.PurePosixPath(item.name)
        if (name.is_absolute() or '..' in name.parts or not name.parts or name.parts[0]!='familytunnel'
            or '\\' in item.name or any(ord(c)<32 for c in item.name)
            or not (item.isfile() or item.isdir()) or item.mode & 0o7000
            or name.as_posix() in seen or len(seen)>=4096 or item.size<0):
            raise SystemExit('Unsicheres Installationsarchiv.')
        seen.add(name.as_posix());total+=item.size
        if total>1024*1024*1024: raise SystemExit('Installationsarchiv ist zu groß.')
PYSAFE
tar -C "$tmp" -xzf "$tmp/bundle.tar.gz" --no-same-owner --no-same-permissions
bash "$tmp/familytunnel/install.sh"
