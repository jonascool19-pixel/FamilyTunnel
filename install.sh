#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export DEBIAN_FRONTEND=noninteractive
[[ $EUID == 0 ]] || { echo 'Bitte mit sudo bash install.sh starten.' >&2; exit 1; }
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
STATE=/var/lib/familytunnel
exec 9>/run/lock/familytunnel.lock
flock -n 9 || { echo 'FamilyTunnel-Wartung läuft bereits.' >&2; exit 1; }
source /etc/os-release
[[ $ID == ubuntu && $VERSION_ID == 24.04 ]] || { echo 'Ubuntu 24.04 erforderlich.' >&2; exit 1; }
[[ -d /run/systemd/system ]] || { echo 'systemd erforderlich.' >&2; exit 1; }
arch=$(dpkg --print-architecture)
[[ $arch == amd64 || $arch == arm64 ]] || { echo 'Nur amd64 und arm64 unterstützt.' >&2; exit 1; }
[[ -f $ROOT/dist/familytunnel-linux-$arch ]] || { echo 'Release-Binary fehlt; zuerst scripts/build.sh ausführen.' >&2; exit 1; }
(cd "$ROOT/dist" && sha256sum --check SHA256SUMS)
echo 'Ubuntu und benötigte Pakete werden aktualisiert.'
apt-get update
apt-get -y upgrade
apt-get install -y ca-certificates curl jq wireguard-tools iproute2 iptables sqlite3 openssl python3 util-linux
# Do not enable nesting or weaken an unprivileged CT. Probe kernel support instead.
if ! ip link show ft0 >/dev/null 2>&1; then
 ip link add ft-probe type wireguard || { echo 'WireGuard im CT nicht verfügbar. Proxmox-Host: WireGuard-Kernelmodul und CT-Netzwerkrechte prüfen. Kein automatisches Privilegieren.' >&2; exit 1; }
 ip link delete ft-probe
fi
install -d -m 700 "$STATE" /etc/wireguard
if [[ -f $STATE/config.json ]]; then
 role=$(jq -er .role "$STATE/config.json")
 echo "Bestehende $role-Konfiguration und Schlüssel werden beibehalten."
else
 role=${FT_ROLE:-}
 if [[ -z $role ]]; then read -rp 'Rolle (server auf Oracle / client zuhause): ' role </dev/tty; fi
 [[ $role == server || $role == client ]] || { echo 'Ungültige Rolle.' >&2; exit 1; }
 if [[ $role == server ]]; then
  public_ip=${FT_PUBLIC_IP:-};if [[ -z $public_ip ]]; then read -rp 'Öffentliche IPv4 der Oracle-VM: ' public_ip </dev/tty; fi
  python3 - "$public_ip" <<'PY'
import ipaddress,sys
ip=ipaddress.ip_address(sys.argv[1]); assert ip.version==4 and ip.is_global, 'Öffentliche IPv4 erforderlich'
PY
  if [[ ! -f $STATE/pairing.json ]]; then
   server_key=$(wg genkey);client_key=$(wg genkey);server_pub=$(printf '%s' "$server_key" | wg pubkey);client_pub=$(printf '%s' "$client_key" | wg pubkey);token=$(openssl rand -hex 32)
   jq -n --arg endpoint "$public_ip" --arg server_public_key "$server_pub" --arg client_private_key "$client_key" --arg token "$token" '{endpoint:$endpoint,server_public_key:$server_public_key,client_private_key:$client_private_key,token:$token}' > "$STATE/pairing.json"
   cat > /etc/wireguard/ft0.conf <<EOF
[Interface]
Address = 10.77.0.1/30
ListenPort = 51820
PrivateKey = $server_key

[Peer]
PublicKey = $client_pub
AllowedIPs = 10.77.0.2/32
EOF
  else
   public_ip=$(jq -er .endpoint "$STATE/pairing.json");token=$(jq -er .token "$STATE/pairing.json")
  fi
  listen=10.77.0.1:8443;data_listen=10.77.0.1:9081
 else
  pairing=${FT_PAIRING_FILE:-}
  if [[ -z $pairing ]]; then
   pairing="$STATE/pairing-input.json"
   read -rp 'Pairing-Code vom Oracle-Server hier einfügen: ' pairing_code </dev/tty
   if [[ -z $pairing_code ]] || ! printf '%s' "$pairing_code" | base64 -d > "$pairing" 2>/dev/null; then
    rm -f "$pairing"
    echo 'Pairing-Code ist ungültig oder unvollständig.' >&2
    exit 1
   fi
   chmod 600 "$pairing"
  fi
  [[ -f $pairing ]] || { echo 'Pairing-Datei fehlt.' >&2; exit 1; }
  python3 - "$pairing" <<'PY'
import json,ipaddress,base64,sys,re
d=json.load(open(sys.argv[1]));ip=ipaddress.ip_address(d['endpoint']);assert ip.version==4 and ip.is_global
for k in ('server_public_key','client_private_key'): assert len(base64.b64decode(d[k],validate=True))==32
assert re.fullmatch('[a-f0-9]{64}',d['token'])
PY
  public_ip=$(jq -er .endpoint "$pairing");server_pub=$(jq -er .server_public_key "$pairing");client_key=$(jq -er .client_private_key "$pairing");token=$(jq -er .token "$pairing")
  admin_ip=${FT_ADMIN_IP:-}
  if [[ -z $admin_ip ]];then
   detected_ip=$(python3 - <<'PY'
import ipaddress, subprocess
for value in subprocess.check_output(["hostname", "-I"], text=True).split():
    try:
        ip = ipaddress.ip_address(value)
    except ValueError:
        continue
    if ip.version == 4 and ip.is_private and not ip.is_loopback and not ip.is_link_local and ip not in ipaddress.ip_network("10.77.0.0/30"):
        print(ip)
        break
PY
)
   if [[ -n $detected_ip ]]; then
    read -rp "Interne IPv4 dieses CT für die Webverwaltung [$detected_ip]: " admin_ip </dev/tty
    admin_ip=${admin_ip:-$detected_ip}
   else
    read -rp 'Interne IPv4 dieses CT für die Webverwaltung (z. B. 192.168.1.50): ' admin_ip </dev/tty
   fi
  fi
  python3 - "$admin_ip" <<'PY'
import ipaddress,sys
ip=ipaddress.ip_address(sys.argv[1]);assert ip.version==4 and ip.is_private and not ip.is_loopback and not ip.is_unspecified
assert any(ip in ipaddress.ip_network(n) for n in ('10.0.0.0/8','172.16.0.0/12','192.168.0.0/16'))
PY
  ip -4 -j addr | jq -e --arg ip "$admin_ip" 'any(.[].addr_info[]; .local == $ip)' >/dev/null || { echo 'Admin-IP ist diesem CT nicht zugewiesen.' >&2; exit 1; }
  listen=$admin_ip:8443;data_listen=10.77.0.2:9080
  cat > /etc/wireguard/ft0.conf <<EOF
[Interface]
Address = 10.77.0.2/30
PrivateKey = $client_key

[Peer]
PublicKey = $server_pub
Endpoint = $public_ip:51820
AllowedIPs = 10.77.0.1/32
PersistentKeepalive = 25
EOF
 fi
 jq -n --arg role "$role" --arg listen "$listen" --arg data_listen "$data_listen" --arg token "$token" --arg public_ip "$public_ip" '{role:$role,listen:$listen,data_listen:$data_listen,token:$token,public_ip:$public_ip,server_url:"http://10.77.0.1:9081",caddy_url:"http://127.0.0.1:2019"}' > "$STATE/config.json"
fi
if [[ $role == server ]]; then
 apt-get install -y caddy
 # Dedicated Caddy instance only; never overwrite an existing non-FamilyTunnel setup.
 if [[ -s /etc/caddy/Caddyfile ]] && ! grep -q 'FamilyTunnel' /etc/caddy/Caddyfile && systemctl is-active --quiet caddy; then
  if ! grep -q 'root \* /usr/share/caddy' /etc/caddy/Caddyfile; then echo 'Bestehende Caddy-Konfiguration gefunden. Dedizierte VM erforderlich.' >&2;exit 1;fi
 fi
 cat > /etc/caddy/Caddyfile <<'EOF'
# FamilyTunnel bootstrap; live config managed through the local Caddy API.
{
 admin 127.0.0.1:2019
}
:80 {
 respond "FamilyTunnel: no services configured" 404
}
EOF
 install -d /etc/systemd/system/caddy.service.d
 printf '[Service]\nRestart=on-failure\nRestartSec=5s\n' > /etc/systemd/system/caddy.service.d/familytunnel.conf
fi
install -d -m 755 /opt/familytunnel/releases/0.1.0 /usr/local/lib/familytunnel
install -m 755 "$ROOT/dist/familytunnel-linux-$arch" /opt/familytunnel/releases/0.1.0/familytunnel
install -m 755 "$ROOT/scripts/manage.sh" "$ROOT/scripts/firewall.sh" "$ROOT/scripts/oci-nsg.py" /usr/local/lib/familytunnel/
ln -sfn /opt/familytunnel/releases/0.1.0 /opt/familytunnel/current.new
mv -Tf /opt/familytunnel/current.new /opt/familytunnel/current
ln -sfn /opt/familytunnel/current/familytunnel /usr/local/bin/familytunnel
install -m 644 "$ROOT/systemd/"* /etc/systemd/system/
chmod 600 /etc/wireguard/ft0.conf "$STATE/config.json"
systemctl daemon-reload
systemctl enable --now wg-quick@ft0
systemctl enable --now familytunnel-firewall
if [[ $role == server ]]; then systemctl enable --now caddy;systemctl restart caddy;fi
systemctl enable --now familytunnel familytunnel-repair.timer
systemctl restart familytunnel
familytunnel selftest
sleep 2
systemctl is-active --quiet familytunnel
[[ -s $STATE/admin.crt && -s $STATE/state.db ]]
sqlite3 "$STATE/state.db" 'PRAGMA integrity_check;' | grep -qx ok
echo "Installation abgeschlossen. Verwaltung: https://$(jq -r .listen "$STATE/config.json")"
echo "Benutzer: admin. Initialpasswort: sudo cat $STATE/initial-password"
openssl x509 -in "$STATE/admin.crt" -noout -fingerprint -sha256
if [[ $role == server ]];then
 echo
 echo '==================== PAIRING-CODE FÜR DEN HEIM-CT ===================='
 base64 -w0 "$STATE/pairing.json"
 echo
 echo '======================================================================'
 echo 'Diesen vollständigen Code kopieren und bei der Client-Installation'
 echo 'in das Feld "Pairing-Code vom Oracle-Server" einfügen.'
 echo 'Der Code enthält geheime Zugangsdaten: nicht veröffentlichen oder teilen.'
 echo "Die Quelldatei bleibt geschützt unter: $STATE/pairing.json"
 echo 'OCI: NSG muss der VM-VNIC zugewiesen sein. Automation: /usr/local/lib/familytunnel/oci-nsg.py NSG_OCID [VNIC_OCID]'
fi
if [[ -e /var/run/reboot-required ]];then echo 'Ubuntu empfiehlt einen Neustart. Er wird nicht ohne deine Kontrolle ausgelöst.';fi
echo 'Tunnel und DNS sind erst nach Einrichtung beider Seiten und der OCI-Regeln prüfbar.'
