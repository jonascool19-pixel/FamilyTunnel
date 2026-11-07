#!/usr/bin/env bash
# Real WireGuard + TCP/UDP + two-stage NAT; all changes stay in disposable namespaces.
set -Eeuo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Run this integration test with sudo';exit 1; }
ROOT=$(cd "$(dirname "$0")/.." && pwd)
state=$(mktemp -d /var/tmp/ft-integration.XXXXXX)
vps=ft-vps-$$;ct=ft-ct-$$;wan=ft-wan-$$;lan=ft-lan-$$
cleanup(){
 status=$?
 if ((status!=0));then
  for ns in "$vps" "$ct" "$wan" "$lan";do
   echo "Diagnostic namespace: $ns"
   ip netns exec "$ns" iptables-save -c 2>/dev/null || true
   ip netns exec "$ns" conntrack -L -p udp 2>/dev/null || true
   ip -n "$ns" link show ft0 2>/dev/null || true
  done
 fi
 for ns in "$vps" "$ct" "$wan" "$lan";do
  ip netns pids "$ns" 2>/dev/null | xargs -r kill 2>/dev/null || true
  ip netns del "$ns" 2>/dev/null || true
 done
 rm -rf -- "$state"
}
trap cleanup EXIT
for ns in "$vps" "$ct" "$wan" "$lan";do ip netns add "$ns";ip -n "$ns" link set lo up;done
link(){
 ip link add "a$$" type veth peer name "b$$"
 ip link set "a$$" netns "$1";ip link set "b$$" netns "$3"
 ip -n "$1" link set "a$$" name "$2";ip -n "$3" link set "b$$" name "$4"
 ip -n "$1" addr add "$5" dev "$2";ip -n "$3" addr add "$6" dev "$4"
 ip -n "$1" link set "$2" up;ip -n "$3" link set "$4" up
}
link "$wan" public "$vps" public 198.18.0.2/24 198.18.0.1/24
link "$vps" underlay "$ct" underlay 192.0.2.1/24 192.0.2.2/24
link "$ct" local "$lan" local 192.168.99.1/24 192.168.99.2/24
ip -n "$wan" route add default via 198.18.0.1
ip -n "$ct" route add 198.18.0.1/32 via 192.0.2.1
ip -n "$lan" route add default via 192.168.99.1
wg genkey > "$state/server.key";wg genkey > "$state/client.key"
server_pub=$(wg pubkey < "$state/server.key");client_pub=$(wg pubkey < "$state/client.key")
for ns in "$vps" "$ct";do ip -n "$ns" link add ft0 type wireguard;ip netns exec "$ns" iptables -P FORWARD DROP;done
ip netns exec "$vps" wg set ft0 listen-port 51820 private-key "$state/server.key" peer "$client_pub" allowed-ips 10.77.0.2/32
ip netns exec "$ct" wg set ft0 listen-port 51821 private-key "$state/client.key" peer "$server_pub" allowed-ips 10.77.0.1/32 endpoint 192.0.2.1:51820 persistent-keepalive 1
ip -n "$vps" addr add 10.77.0.1/30 dev ft0;ip -n "$ct" addr add 10.77.0.2/30 dev ft0
ip -n "$vps" link set ft0 up;ip -n "$ct" link set ft0 up
for role in server client;do
 mkdir -p "$state/$role"
 printf '{"role":"%s"}\n' "$role" > "$state/$role/config.json"
 sqlite3 "$state/$role/state.db" 'CREATE TABLE routes(public_port INTEGER,enabled INTEGER);CREATE TABLE port_forwards(id INTEGER PRIMARY KEY,name TEXT,protocol TEXT,target_ip TEXT,target_port INTEGER,public_port INTEGER,enabled INTEGER);INSERT INTO port_forwards VALUES(1,"TCP","tcp","192.168.99.2",18080,28080,1),(2,"UDP","udp","192.168.99.2",18081,28081,1);'
done
ip netns exec "$vps" env FT_STATE="$state/server" bash "$ROOT/scripts/firewall.sh" apply
ip netns exec "$ct" env FT_STATE="$state/client" bash "$ROOT/scripts/firewall.sh" apply
ip netns exec "$lan" python3 "$ROOT/tests/port-echo.py" &
sleep 2
if [[ -n ${FT_TEST_BINARY:-} ]];then
 ip netns exec "$ct" env FT_FULL_SCAN_TARGET=192.168.99.2 "$FT_TEST_BINARY" -test.run '^TestFullPortSweepInNamespace$' -test.v -test.timeout=75s
fi
ip netns exec "$wan" python3 - <<'PY'
import socket
import struct
payload = bytes(range(256)) * 1024
with socket.create_connection(("198.18.0.1", 28080), timeout=5) as connection:
    connection.sendall(payload)
    received = b""
    while len(received) < len(payload):
        received += connection.recv(16384)
    assert received == payload, "TCP stream changed"
# UDP packets with DF set must receive the real tunnel MTU, not silently pass oversized packets.
with socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_ICMP) as errors:
    errors.settimeout(5)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as oversized:
        oversized.setsockopt(socket.IPPROTO_IP, 10, 2)  # Linux IP_MTU_DISCOVER / IP_PMTUDISC_DO
        oversized.sendto(b"m" * 1400, ("198.18.0.1", 28081))
    packet, _ = errors.recvfrom(65535)
    header = (packet[0] & 15) * 4
    assert packet[header:header+2] == bytes([3, 4]), "PMTU ICMP error missing"
    assert struct.unpack("!H", packet[header+6:header+8])[0] == 1420, "Wrong WireGuard path MTU"
print("UDP path MTU discovery passed", flush=True)
for payload in (b"", b"game-query", b"x" * 1400, b"z" * 64000):
    print("Testing UDP payload bytes:", len(payload), flush=True)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as udp:
        udp.settimeout(5)
        udp.setsockopt(socket.IPPROTO_IP, 10, 0)  # Allow fragmentation for large UDP datagrams.
        if payload == b"game-query":
            udp.bind(("198.18.0.2", 33000))
        udp.sendto(payload, ("198.18.0.1", 28081))
        received, address = udp.recvfrom(65535)
        assert received == payload, "UDP datagram changed"
        assert address == ("198.18.0.1", 28081), "UDP return mapping wrong"
print("Real TCP streams and UDP datagrams passed through WireGuard and both NAT stages")
PY
# Reuse the same external client tuple after editing the target. Old NAT state must be removed.
for role in server client;do sqlite3 "$state/$role/state.db" 'UPDATE port_forwards SET target_port=18082 WHERE protocol="udp";';done
ip netns exec "$vps" env FT_STATE="$state/server" bash "$ROOT/scripts/firewall.sh" apply
ip netns exec "$ct" env FT_STATE="$state/client" bash "$ROOT/scripts/firewall.sh" apply
ip netns exec "$wan" python3 - <<'PY'
import socket
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
    client.settimeout(5)
    client.bind(("198.18.0.2", 33000))
    client.sendto(b"same-client", ("198.18.0.1", 28081))
    response, address = client.recvfrom(65535)
    assert response == b"new:same-client", "Stale UDP NAT target survived an edit"
print("Editing UDP target with the same external client tuple passed")
PY
# Disable one mapping. New connections must fail, while the other protocol remains configured.
for role in server client;do sqlite3 "$state/$role/state.db" 'UPDATE port_forwards SET enabled=0 WHERE protocol="tcp";';done
ip netns exec "$vps" env FT_STATE="$state/server" bash "$ROOT/scripts/firewall.sh" apply
ip netns exec "$ct" env FT_STATE="$state/client" bash "$ROOT/scripts/firewall.sh" apply
ip netns exec "$wan" python3 - <<'PY'
import socket
try:
    socket.create_connection(("198.18.0.1", 28080), timeout=1)
except OSError:
    pass
else:
    raise AssertionError("Disabled TCP mapping is still reachable")
PY
ip netns exec "$vps" env FT_STATE="$state/server" bash "$ROOT/scripts/firewall.sh" remove
ip netns exec "$ct" env FT_STATE="$state/client" bash "$ROOT/scripts/firewall.sh" remove
if ip netns exec "$vps" iptables -t nat -S FTPRE 2>/dev/null;then echo 'NAT cleanup failed' >&2;exit 1;fi
echo 'Disable and scoped cleanup passed'
