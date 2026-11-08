#!/usr/bin/env bash
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Bitte über den dokumentierten sudo-Befehl starten.' >&2; exit 1; }
export FT_ROLE=client
export FT_BUNDLE_URL=https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/dist/familytunnel-0.1.0-install.tar.gz
umask 077
ft_bootstrap_dir=$(mktemp -d /var/tmp/familytunnel-entry.XXXXXX)
trap 'rm -rf -- "$ft_bootstrap_dir"' EXIT
curl --proto '=https' --tlsv1.2 -4 --connect-timeout 10 --max-time 60 -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/bootstrap.sh -o "$ft_bootstrap_dir/bootstrap.sh"
bash "$ft_bootstrap_dir/bootstrap.sh"
