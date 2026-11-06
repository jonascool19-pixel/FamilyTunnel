#!/usr/bin/env bash
set -Eeuo pipefail
STATE=/var/lib/familytunnel
exec 8>/run/lock/familytunnel-firewall.lock
flock -w 10 8
if [[ ${1:-apply} == remove ]];then
 while iptables -w -C INPUT -j FAMILYTUNNEL 2>/dev/null;do iptables -w -D INPUT -j FAMILYTUNNEL;done
 iptables -w -F FAMILYTUNNEL 2>/dev/null || true
 iptables -w -X FAMILYTUNNEL 2>/dev/null || true
 exit 0
fi
role=$(jq -er .role "$STATE/config.json")
iptables -w -N FAMILYTUNNEL 2>/dev/null || true
iptables -w -F FAMILYTUNNEL
iptables -w -A FAMILYTUNNEL -i lo -j RETURN
iptables -w -A FAMILYTUNNEL -i ft0 -s 10.77.0.0/30 -p tcp -m multiport --dports 8443,9080,9081 -j ACCEPT
if [[ $role == server ]];then
 iptables -w -A FAMILYTUNNEL -p tcp -m multiport --dports 80,443 -j ACCEPT
 iptables -w -A FAMILYTUNNEL -p udp --dport 51820 -j ACCEPT
 # Only active, explicitly published HTTPS ports are opened. Old databases have none.
 if [[ -f $STATE/state.db ]] && [[ $(sqlite3 "$STATE/state.db" "SELECT count(*) FROM pragma_table_info('routes') WHERE name='public_port';") == 1 ]];then
  while IFS= read -r port;do
   [[ $port =~ ^[0-9]+$ ]] && ((port>=1024 && port<=65535)) || continue
   case $port in 2019|8443|9080|9081) continue;; esac
   iptables -w -A FAMILYTUNNEL -p tcp --dport "$port" -j ACCEPT
  done < <(sqlite3 -cmd '.timeout 5000' "$STATE/state.db" 'SELECT DISTINCT public_port FROM routes WHERE enabled=1 ORDER BY public_port;')
 fi
else
 for net in 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16;do iptables -w -A FAMILYTUNNEL -s "$net" -p tcp --dport 8443 -j ACCEPT;done
fi
iptables -w -A FAMILYTUNNEL -p tcp -m multiport --dports 8443,9080,9081,2019 -j DROP
iptables -w -A FAMILYTUNNEL -j RETURN
iptables -w -C INPUT -j FAMILYTUNNEL 2>/dev/null || iptables -w -I INPUT 1 -j FAMILYTUNNEL
# Existing SSH policy and Oracle metadata/storage rules remain untouched.
