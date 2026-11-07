#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
STATE=/var/lib/familytunnel
BACKUPS=/var/backups/familytunnel
ACTION=${1:-status}
shift || true
[[ $EUID == 0 ]] || { echo 'Root-Rechte erforderlich.' >&2; exit 1; }
mkdir -p "$BACKUPS"

status() {
 systemctl --no-pager --full status familytunnel wg-quick@ft0 || true
 if [[ $(jq -r .role "$STATE/config.json") == server ]]; then systemctl --no-pager --full status caddy || true; fi
 wg show ft0 || true
 sqlite3 "$STATE/state.db" 'PRAGMA quick_check;'
}

backup() {
 local out=${1:-$BACKUPS/familytunnel-$(date -u +%Y%m%dT%H%M%SZ).tar.gz}
 local tmp
 tmp=$(mktemp -d /var/tmp/familytunnel-backup.XXXXXX)
 trap 'rm -rf -- "$tmp"' EXIT
 sqlite3 "$STATE/state.db" ".backup '$tmp/state.db'"
 install -m 600 "$STATE/config.json" "$tmp/config.json"
 for f in admin.crt admin.key pairing.json initial-password oci-target.json site-verification.json; do [[ -f $STATE/$f ]] && install -m 600 "$STATE/$f" "$tmp/$f"; done
 if [[ -d $STATE/oci ]];then cp -a "$STATE/oci" "$tmp/oci";fi
 install -m 600 /etc/wireguard/ft0.conf "$tmp/ft0.conf"
 tar -C "$tmp" -czf "$out" .
 chmod 600 "$out"
 sha256sum "$out" > "$out.sha256"
 echo "$out"
}

restore() {
 local archive=${1:?Nutzung: familytunnel restore DATEI.tar.gz}
 [[ -f $archive ]] || { echo 'Backup nicht gefunden.' >&2; exit 1; }
 [[ -f $archive.sha256 ]] && (cd "$(dirname "$archive")" && sha256sum -c "$(basename "$archive").sha256")
 local tmp pre
 tmp=$(mktemp -d /var/tmp/familytunnel-restore.XXXXXX)
 trap 'rm -rf -- "$tmp"' EXIT
 tar -tzf "$archive" | grep -Eq '(^/|(^|/)\.\.(/|$))' && { echo 'Unsicherer Backup-Pfad.' >&2; exit 1; } || true
 tar -C "$tmp" -xzf "$archive" --no-same-owner --no-same-permissions
 [[ -f $tmp/state.db && -f $tmp/config.json && -f $tmp/ft0.conf ]] || { echo 'Unvollständiges Backup.' >&2; exit 1; }
 sqlite3 "$tmp/state.db" 'PRAGMA integrity_check;' | grep -qx ok
 jq -e '.role == "server" or .role == "client"' "$tmp/config.json" >/dev/null
 pre=$(backup)
 systemctl stop familytunnel-maintenance.timer familytunnel-maintenance.service 2>/dev/null || true
 systemctl stop familytunnel wg-quick@ft0
 rm -f "$STATE/state.db-wal" "$STATE/state.db-shm"
 install -m 600 "$tmp/state.db" "$STATE/state.db"
 install -m 600 "$tmp/config.json" "$STATE/config.json"
 install -m 600 "$tmp/ft0.conf" /etc/wireguard/ft0.conf
 rm -f "$STATE/oci-target.json"
 for f in admin.crt admin.key pairing.json initial-password oci-target.json site-verification.json; do [[ -f $tmp/$f ]] && install -m 600 "$tmp/$f" "$STATE/$f"; done
 # Restoring an old snapshot must never replay an old reboot order or plan.
 sqlite3 "$STATE/state.db" "DELETE FROM settings WHERE k LIKE 'maintenance_%';"
 if [[ -d $tmp/oci ]];then install -d -m 700 "$STATE/oci";cp -a "$tmp/oci/." "$STATE/oci/";fi
 /usr/local/lib/familytunnel/firewall.sh apply
 systemctl start wg-quick@ft0 familytunnel
 systemctl start familytunnel-maintenance.timer 2>/dev/null || true
 sleep 2
 if ! systemctl is-active --quiet familytunnel; then echo "Restore fehlgeschlagen; vorheriger Stand: $pre" >&2; exit 1; fi
 echo "Wiederhergestellt. Sicherheitskopie des vorherigen Stands: $pre"
}

repair() {
 local failed=0 role
 role=$(jq -r .role "$STATE/config.json")
 ip link show ft0 >/dev/null 2>&1 || systemctl restart wg-quick@ft0 || failed=1
 /usr/local/lib/familytunnel/firewall.sh apply || failed=1
 systemctl is-active --quiet familytunnel || systemctl restart familytunnel || failed=1
 if [[ $role == server ]]; then systemctl is-active --quiet caddy || systemctl restart caddy || failed=1; fi
 sqlite3 "$STATE/state.db" 'PRAGMA quick_check;' | grep -qx ok || failed=1
 exit "$failed"
}

update() {
 local release=${1:-} pre
 [[ -n $release ]] || { echo 'Nutzung: familytunnel update URL-ZUM-RELEASE-TAR.GZ' >&2; exit 1; }
 local tmp old version arch
 tmp=$(mktemp -d /var/tmp/familytunnel-update.XXXXXX)
 trap 'rm -rf -- "$tmp"' EXIT
 curl --proto '=https' --tlsv1.2 -fL --retry 3 "$release" -o "$tmp/release.tar.gz"
 curl --proto '=https' --tlsv1.2 -fL --retry 3 "$release.sha256" -o "$tmp/release.tar.gz.sha256"
 local expected actual
 expected=$(awk 'NR==1 {print $1}' "$tmp/release.tar.gz.sha256")
 [[ $expected =~ ^[a-fA-F0-9]{64}$ ]] || { echo 'Ungültige Release-Prüfsumme.' >&2; exit 1; }
 actual=$(sha256sum "$tmp/release.tar.gz" | awk '{print $1}')
 [[ ${actual,,} == ${expected,,} ]] || { echo 'Release-Prüfsumme stimmt nicht.' >&2; exit 1; }
 tar -tzf "$tmp/release.tar.gz" | grep -Eq '(^/|(^|/)\.\.(/|$))' && { echo 'Unsicheres Release-Archiv.' >&2; exit 1; } || true
 tar -C "$tmp" -xzf "$tmp/release.tar.gz" --no-same-owner --no-same-permissions
 version=$(jq -er .version "$tmp/release.json")
 [[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
 arch=$(dpkg --print-architecture)
 [[ -f $tmp/familytunnel-linux-$arch ]]
 "$tmp/familytunnel-linux-$arch" selftest
 pre=$(backup)
 old=$(readlink -f /opt/familytunnel/current)
 local release_hash release_dir
 release_hash=$(sha256sum "$tmp/familytunnel-linux-$arch" | awk '{print $1}')
 release_dir="/opt/familytunnel/releases/$release_hash"
 install -d -m 755 "$release_dir"
 if [[ ! -f $release_dir/familytunnel ]]; then install -m 755 "$tmp/familytunnel-linux-$arch" "$release_dir/familytunnel.new";mv -Tf "$release_dir/familytunnel.new" "$release_dir/familytunnel";fi
 ln -sfn "$release_dir" /opt/familytunnel/current.new
 mv -Tf /opt/familytunnel/current.new /opt/familytunnel/current
 systemctl restart familytunnel
 sleep 3
 if ! systemctl is-active --quiet familytunnel; then
  ln -sfn "$old" /opt/familytunnel/current.new
  mv -Tf /opt/familytunnel/current.new /opt/familytunnel/current
  restore "$pre" >/dev/null
  echo 'Update fehlgeschlagen; atomar zurückgerollt.' >&2
  exit 1
 fi
 echo "Aktualisiert auf $version"
}

uninstall() {
 [[ ${1:-} == --yes ]] || { echo 'Nutzung: familytunnel uninstall --yes (Backups bleiben erhalten)' >&2; exit 1; }
 backup >/dev/null
 systemctl disable --now familytunnel-maintenance.timer familytunnel-repair.timer familytunnel familytunnel-firewall wg-quick@ft0 2>/dev/null || true
 [[ $(jq -r .role "$STATE/config.json" 2>/dev/null) == server ]] && systemctl disable --now caddy 2>/dev/null || true
 rm -f /etc/systemd/system/familytunnel*.service /etc/systemd/system/familytunnel*.timer /etc/systemd/system/caddy.service.d/familytunnel.conf
 rm -f /usr/local/bin/familytunnel /etc/wireguard/ft0.conf
 rm -rf /usr/local/lib/familytunnel /opt/familytunnel "$STATE"
 systemctl daemon-reload
 echo "FamilyTunnel entfernt. Backups liegen in $BACKUPS. Das Caddy-Paket bleibt installiert."
}

case "$ACTION" in
 status) status;; repair) repair;; backup) backup "${1:-}";; restore) restore "$@";; update) update "$@";; uninstall) uninstall "$@";; *) echo 'status|repair|update|backup|restore|uninstall' >&2;exit 2;;
esac
