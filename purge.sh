#!/usr/bin/env bash
set -Eeuo pipefail
[[ $EUID == 0 ]] || { echo 'Bitte über sudo bash starten.' >&2; exit 1; }

role=""
if [[ -f /var/lib/familytunnel/config.json ]] && command -v jq >/dev/null 2>&1; then
 role=$(jq -r '.role // empty' /var/lib/familytunnel/config.json 2>/dev/null || true)
fi

systemctl disable --now familytunnel-repair.timer familytunnel-repair.service familytunnel.service familytunnel-firewall.service wg-quick@ft0.service 2>/dev/null || true

if command -v iptables >/dev/null 2>&1; then
 while iptables -w -C INPUT -j FAMILYTUNNEL 2>/dev/null; do iptables -w -D INPUT -j FAMILYTUNNEL || break; done
 iptables -w -F FAMILYTUNNEL 2>/dev/null || true
 iptables -w -X FAMILYTUNNEL 2>/dev/null || true
fi

rm -f /etc/systemd/system/familytunnel.service \
 /etc/systemd/system/familytunnel-firewall.service \
 /etc/systemd/system/familytunnel-repair.service \
 /etc/systemd/system/familytunnel-repair.timer \
 /etc/wireguard/ft0.conf \
 /usr/local/bin/familytunnel
rm -rf /var/lib/familytunnel /var/backups/familytunnel /opt/familytunnel /usr/local/lib/familytunnel
find /var/tmp -maxdepth 1 -type d -name 'familytunnel-*' -exec rm -rf -- {} + 2>/dev/null || true

if [[ $role == server ]] || grep -q 'FamilyTunnel' /etc/caddy/Caddyfile 2>/dev/null; then
 systemctl disable --now caddy.service 2>/dev/null || true
 rm -rf /etc/systemd/system/caddy.service.d /etc/caddy /var/lib/caddy
 if dpkg-query -W -f='${Status}' caddy 2>/dev/null | grep -q 'install ok installed'; then
  apt-get purge -y caddy
 fi
fi

systemctl daemon-reload
systemctl reset-failed
echo 'FamilyTunnel wurde vollständig zurückgesetzt. Allgemeine Ubuntu-Pakete und Systemupdates bleiben erhalten.'
