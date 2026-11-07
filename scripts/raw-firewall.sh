#!/usr/bin/env bash
# Scoped TCP/UDP NAT. All traffic remains inside the authenticated WireGuard path.
set -Eeuo pipefail
STATE=${FT_STATE:-/var/lib/familytunnel}
forget_mapping(){
 local proto=$1 public=$2
 if [[ $(jq -r .role "$STATE/config.json") == server ]];then
  conntrack -D -p "$proto" --reply-src 10.77.0.2 --orig-port-dst "$public" >/dev/null 2>&1 || true
 else
  conntrack -D -p "$proto" --orig-src 10.77.0.1 --orig-dst 10.77.0.2 --orig-port-dst "$public" >/dev/null 2>&1 || true
 fi
}
if [[ ${1:-apply} == remove ]];then
 if [[ -s $STATE/raw-active ]];then
  while IFS='|' read -r proto public ip private;do forget_mapping "$proto" "$public";done < "$STATE/raw-active"
  rm -f -- "$STATE/raw-active"
 fi
 for spec in 'nat PREROUTING FTPRE' 'nat POSTROUTING FTPOST' 'filter FORWARD FTFWD' 'mangle FORWARD FTMSS';do
  read -r table parent chain <<< "$spec"
  while iptables -w -t "$table" -C "$parent" -j "$chain" 2>/dev/null;do iptables -w -t "$table" -D "$parent" -j "$chain";done
  iptables -w -t "$table" -F "$chain" 2>/dev/null || true
  iptables -w -t "$table" -X "$chain" 2>/dev/null || true
 done
 if [[ -s $STATE/forwarding-original ]];then
  original=$(<"$STATE/forwarding-original")
  if [[ $original =~ ^[01]$ ]] && [[ $(sysctl -n net.ipv4.ip_forward) == 1 ]];then sysctl -q -w "net.ipv4.ip_forward=$original";fi
  rm -f -- "$STATE/forwarding-original"
 fi
 exit 0
fi
[[ -s $STATE/state.db ]] || exit 0
[[ $(sqlite3 -cmd '.timeout 5000' "$STATE/state.db" "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='port_forwards';") == 1 ]] || exit 0
role=$(jq -er .role "$STATE/config.json")
rows=$(sqlite3 -cmd '.timeout 5000' -separator '|' "$STATE/state.db" 'SELECT protocol,public_port,target_ip,target_port FROM port_forwards WHERE enabled=1 ORDER BY id;')
if [[ -n $rows ]];then
 current=$(sysctl -n net.ipv4.ip_forward)
 [[ -f $STATE/forwarding-original ]] || printf '%s\n' "$current" > "$STATE/forwarding-original"
 if [[ $current != 1 ]];then
  sysctl -q -w net.ipv4.ip_forward=1 || { echo 'IP-Weiterleitung im CT nicht erlaubt. Proxmox-Netzwerkrechte prüfen; es wird kein privilegierter CT erzwungen.' >&2;exit 1; }
 fi
fi
for spec in 'nat PREROUTING FTPRE' 'nat POSTROUTING FTPOST' 'filter FORWARD FTFWD' 'mangle FORWARD FTMSS';do
 read -r table parent chain <<< "$spec"
 iptables -w -t "$table" -N "$chain" 2>/dev/null || true
 iptables -w -t "$table" -C "$parent" -j "$chain" 2>/dev/null || iptables -w -t "$table" -I "$parent" 1 -j "$chain"
done
rules=$(mktemp /run/lock/familytunnel-nat.XXXXXX)
trap 'rm -f -- "$rules"' EXIT
printf '*nat\n-F FTPRE\n-F FTPOST\n' > "$rules"
while IFS='|' read -r proto public ip private;do
 [[ -n $proto ]] || continue
 [[ $proto == tcp || $proto == udp ]] && [[ $public =~ ^[0-9]+$ && $private =~ ^[0-9]+$ && $ip =~ ^[0-9.]+$ ]] || exit 1
 ((public>=1024 && public<=65535 && private>=1 && private<=65535)) || exit 1
 case $public in 2019|8443|9080|9081) exit 1;; esac
 [[ $proto != udp || $public != 51820 ]] || exit 1
 if [[ $role == server ]];then
  printf -- '-A FTPRE ! -i ft0 -p %s --dport %s -j DNAT --to-destination 10.77.0.2:%s\n' "$proto" "$public" "$public" >> "$rules"
  printf -- '-A FTPOST -o ft0 -d 10.77.0.2 -p %s --dport %s -j SNAT --to-source 10.77.0.1\n' "$proto" "$public" >> "$rules"
 else
  printf -- '-A FTPRE -i ft0 -s 10.77.0.1 -d 10.77.0.2 -p %s --dport %s -j DNAT --to-destination %s:%s\n' "$proto" "$public" "$ip" "$private" >> "$rules"
  printf -- '-A FTPOST -s 10.77.0.1 -d %s -p %s --dport %s ! -o ft0 -j MASQUERADE\n' "$ip" "$proto" "$private" >> "$rules"
 fi
done <<< "$rows"
printf 'COMMIT\n*filter\n-F FTFWD\n' >> "$rules"
while IFS='|' read -r proto public ip private;do
 [[ -n $proto ]] || continue
 if [[ $role == server ]];then
  printf -- '-A FTFWD -o ft0 -d 10.77.0.2 -p %s --dport %s -m conntrack --ctstate DNAT -j ACCEPT\n' "$proto" "$public" >> "$rules"
  printf -- '-A FTFWD -i ft0 -s 10.77.0.2 -p %s --sport %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT\n' "$proto" "$public" >> "$rules"
 else
  printf -- '-A FTFWD -i ft0 -s 10.77.0.1 -d %s -p %s --dport %s -m conntrack --ctstate DNAT -j ACCEPT\n' "$ip" "$proto" "$private" >> "$rules"
  printf -- '-A FTFWD -o ft0 -d 10.77.0.1 -s %s -p %s --sport %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT\n' "$ip" "$proto" "$private" >> "$rules"
 fi
done <<< "$rows"
printf -- '-A FTFWD -i ft0 -j DROP\n-A FTFWD -o ft0 -j DROP\n-A FTFWD -j RETURN\nCOMMIT\n' >> "$rules"
printf -- '*mangle\n-F FTMSS\n-A FTMSS -i ft0 -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu\n-A FTMSS -o ft0 -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu\n-A FTMSS -j RETURN\nCOMMIT\n' >> "$rules"
iptables-restore -w --noflush < "$rules"
if [[ -s $STATE/raw-active ]];then
 while IFS='|' read -r proto public ip private;do
  if ! grep -Fxq "$proto|$public|$ip|$private" <<< "$rows";then forget_mapping "$proto" "$public";fi
 done < "$STATE/raw-active"
fi
printf '%s\n' "$rows" > "$STATE/raw-active.new"
mv -f -- "$STATE/raw-active.new" "$STATE/raw-active"
