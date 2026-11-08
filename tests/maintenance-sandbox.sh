#!/usr/bin/env bash
# CI-only regression for the real systemd address-family sandbox. No reboot.
set -Eeuo pipefail
cd "$(dirname "$0")/.."
[[ $EUID == 0 ]] || { echo 'Run inside the disposable Linux CI VM as root.' >&2; exit 1; }
families=$(awk -F= '$1=="RestrictAddressFamilies" {print $2}' systemd/familytunnel-maintenance.service)
[[ " $families " == *' AF_NETLINK '* ]] || { echo 'WireGuard requires AF_NETLINK.' >&2; exit 1; }
check='import socket; s=socket.socket(socket.AF_NETLINK,socket.SOCK_RAW,16); s.close()'
# First prove the previous restriction actually blocks the required socket.
if systemd-run --quiet --wait --pipe --collect --unit="ft-netlink-denied-$$" \
    -p 'RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6' \
    -p NoNewPrivileges=yes -p ProtectSystem=strict /usr/bin/python3 -c "$check"; then
 echo 'Expected the old sandbox to block netlink.' >&2; exit 1
fi
systemd-run --quiet --wait --pipe --collect --unit="ft-netlink-allowed-$$" \
    -p "RestrictAddressFamilies=$families" \
    -p NoNewPrivileges=yes -p ProtectSystem=strict /usr/bin/python3 -c "$check"
echo 'Real systemd sandbox permits the maintenance WireGuard query; no reboot executed.'
