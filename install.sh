#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
export DEBIAN_FRONTEND=noninteractive
[[ $EUID == 0 ]] || { echo 'Bitte mit sudo bash install.sh starten.' >&2; exit 1; }
if [[ -t 1 ]]; then
 BLUE=$'\033[1;34m'; CYAN=$'\033[1;36m'; GREEN=$'\033[1;32m'; YELLOW=$'\033[1;33m'; RESET=$'\033[0m'
else
 BLUE=''; CYAN=''; GREEN=''; YELLOW=''; RESET=''
fi
step() { printf '\n%b==>%b %b%s%b\n' "$BLUE" "$RESET" "$CYAN" "$1" "$RESET"; }
done_step() { printf '%b[OK]%b %s\n' "$GREEN" "$RESET" "$1"; }
printf '%b\n' "$CYAN"
echo '  +==================================================================+'
echo '  |                                                                  |'
echo '  |                    FAMILYTUNNEL INSTALLATION                     |'
echo '  |               Server und Client · Ubuntu 24.04                  |'
echo '  |                                                                  |'
echo '  +==================================================================+'
printf '%b' "$RESET"
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
done_step 'Release und Prüfsummen wurden verifiziert.'
step 'Ubuntu aktualisieren und benötigte Pakete installieren'
apt-get update
apt-get -y upgrade
apt-get install -y ca-certificates curl jq wireguard-tools iproute2 iptables conntrack sqlite3 openssl python3 util-linux
done_step 'Ubuntu und Pakete sind bereit.'
# Do not enable nesting or weaken an unprivileged CT. Probe kernel support instead.
step 'WireGuard-Unterstützung prüfen'
if ! ip link show ft0 >/dev/null 2>&1; then
 ip link add ft-probe type wireguard || { echo 'WireGuard im CT nicht verfügbar. Proxmox-Host: WireGuard-Kernelmodul und CT-Netzwerkrechte prüfen. Kein automatisches Privilegieren.' >&2; exit 1; }
 ip link delete ft-probe
fi
done_step 'WireGuard ist verfügbar.'
install -d -m 700 "$STATE" /etc/wireguard
if [[ -f $STATE/config.json ]]; then
 role=$(jq -er .role "$STATE/config.json")
 echo "Bestehende $role-Konfiguration und Schlüssel werden beibehalten."
else
 role=${FT_ROLE:-}
 if [[ -z $role ]]; then
  echo
  echo 'Wo soll FamilyTunnel installiert werden?'
  echo '  1) Server (Oracle Cloud-Instanz / öffentlicher VPS)'
  echo '  2) Client (Proxmox Ubuntu CT zu Hause)'
  read -rp 'Auswahl [1 oder 2]: ' role_choice </dev/tty
  case "$role_choice" in
   1) role=server ;;
   2) role=client ;;
   *) echo 'Ungültige Auswahl. Bitte 1 oder 2 eingeben.' >&2; exit 1 ;;
  esac
 fi
 [[ $role == server || $role == client ]] || { echo 'Ungültige Rolle.' >&2; exit 1; }
 step "Rolle $role konfigurieren"
 if [[ $role == server ]]; then
  public_ip=${FT_PUBLIC_IP:-}
  if [[ -z $public_ip ]];then
   public_ip=$(curl --proto '=https' --tlsv1.2 --noproxy '*' -4 -fsS --connect-timeout 3 --max-time 6 https://api.ipify.org || true)
   if [[ -n $public_ip ]];then done_step "Öffentliche IPv4 automatisch erkannt: $public_ip";else read -rp 'Öffentliche IPv4 der Oracle-VM: ' public_ip </dev/tty;fi
  fi
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
    admin_ip=$detected_ip
    done_step "Interne Verwaltungsadresse automatisch erkannt: $admin_ip"
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
 step 'Caddy als öffentlichen HTTPS-Endpunkt einrichten'
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
 done_step 'Caddy ist eingerichtet.'
fi
step 'FamilyTunnel-Controller und Systemdienste installieren'
if [[ -s $STATE/state.db ]]; then
 install -d -m 700 /var/backups/familytunnel
 db_backup="/var/backups/familytunnel/pre-install-$(date -u +%Y%m%dT%H%M%S)-$$.db"
 sqlite3 "$STATE/state.db" ".backup '$db_backup'"
 chmod 600 "$db_backup"
 done_step "Datenbank vor der Aktualisierung gesichert: $db_backup"
fi
binary_hash=$(sha256sum "$ROOT/dist/familytunnel-linux-$arch" | awk '{print $1}')
release_dir="/opt/familytunnel/releases/$binary_hash"
install -d -m 755 "$release_dir" /usr/local/lib/familytunnel
# Never overwrite a running executable (Linux rejects that with ETXTBSY).
if [[ ! -f $release_dir/familytunnel ]]; then
 install -m 755 "$ROOT/dist/familytunnel-linux-$arch" "$release_dir/familytunnel.new"
 mv -Tf "$release_dir/familytunnel.new" "$release_dir/familytunnel"
fi
printf '%s  %s\n' "$binary_hash" "$release_dir/familytunnel" | sha256sum -c -
install -m 755 "$ROOT/scripts/manage.sh" "$ROOT/scripts/firewall.sh" "$ROOT/scripts/raw-firewall.sh" "$ROOT/scripts/oci-nsg.py" /usr/local/lib/familytunnel/
ln -sfn "$release_dir" /opt/familytunnel/current.new
mv -Tf /opt/familytunnel/current.new /opt/familytunnel/current
ln -sfn /opt/familytunnel/current/familytunnel /usr/local/bin/familytunnel
install -m 644 "$ROOT/systemd/"* /etc/systemd/system/
chmod 600 /etc/wireguard/ft0.conf "$STATE/config.json"
systemctl daemon-reload
systemctl enable --now wg-quick@ft0
systemctl enable --now familytunnel-firewall
if [[ $role == server ]]; then systemctl enable --now caddy;systemctl restart caddy;fi
systemctl enable --now familytunnel familytunnel-repair.timer familytunnel-maintenance.timer
systemctl restart familytunnel
done_step 'Systemdienste wurden gestartet.'
step 'Abschließende Selbsttests ausführen'
familytunnel selftest
sleep 2
systemctl is-active --quiet familytunnel
active_pid=$(systemctl show --property MainPID --value familytunnel)
[[ $active_pid =~ ^[1-9][0-9]*$ ]]
printf '%s  /proc/%s/exe\n' "$binary_hash" "$active_pid" | sha256sum -c -
done_step "Aktiver Controller bestätigt: ${binary_hash:0:12}"
[[ -s $STATE/admin.crt && -s $STATE/state.db ]]
sqlite3 "$STATE/state.db" 'PRAGMA integrity_check;' | grep -qx ok
done_step 'Alle Selbsttests waren erfolgreich.'
if [[ $role == server ]];then
 step 'Oracle-Anbindung und Cloud-Freigaben automatisch einrichten'
 bash "$ROOT/scripts/oracle-setup.sh" "$STATE"
fi
admin_url="https://$(jq -r .listen "$STATE/config.json")"
initial_password=""
if [[ -s $STATE/initial-password ]]; then initial_password=$(tr -d '\r\n' < "$STATE/initial-password"); fi
cert_fingerprint=$(openssl x509 -in "$STATE/admin.crt" -noout -fingerprint -sha256)
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
 echo 'Oracle-Anbindung wurde im Installationsablauf geprüft; keine zusätzliche NSG-Auswahl in der Oberfläche erforderlich.'
fi
echo "$cert_fingerprint"

if [[ -t 1 ]]; then
 green=$'\033[1;32m'; cyan=$'\033[1;36m'; yellow=$'\033[1;33m'; reset=$'\033[0m'
else
 green=''; cyan=''; yellow=''; reset=''
fi
box_line() { printf '%b|%b %-68s %b|%b\n' "$cyan" "$reset" "$1" "$cyan" "$reset"; }
echo
printf '%b+----------------------------------------------------------------------+%b\n' "$cyan" "$reset"
printf '%b|%b %b%-68s%b %b|%b\n' "$cyan" "$reset" "$green" 'FAMILYTUNNEL – INSTALLATION ERFOLGREICH' "$reset" "$cyan" "$reset"
printf '%b+----------------------------------------------------------------------+%b\n' "$cyan" "$reset"
if [[ $role == client ]]; then
 box_line "Webseite erreichbar: $admin_url"
else
 box_line "Webseite intern nach CT-Verbindung: $admin_url"
fi
box_line 'Benutzername: admin'
if [[ -n $initial_password ]]; then
 box_line "Startpasswort: $initial_password"
else
 box_line 'Passwort: bereits geändert – vorhandenes Passwort verwenden'
fi
box_line "Rolle: $role"
if [[ -e /var/run/reboot-required ]]; then
 printf '%b|%b %b%-68s%b %b|%b\n' "$cyan" "$reset" "$yellow" 'Hinweis: Ubuntu empfiehlt einen kontrollierten Neustart.' "$reset" "$cyan" "$reset"
fi
printf '%b+----------------------------------------------------------------------+%b\n' "$cyan" "$reset"
