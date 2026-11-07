#!/usr/bin/env bash
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Bitte über den dokumentierten sudo-Befehl starten.' >&2; exit 1; }
export FT_ROLE=client
export FT_BUNDLE_URL=https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/dist/familytunnel-0.1.0-install.tar.gz
curl --proto '=https' --tlsv1.2 -4 --connect-timeout 10 --max-time 60 -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/bootstrap.sh | bash
