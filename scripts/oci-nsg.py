#!/usr/bin/env python3
"""Idempotently add FamilyTunnel ingress rules to an existing OCI NSG.

Requires a configured OCI CLI. Optionally attaches the NSG to a VNIC.
"""
import json, subprocess, sys

if len(sys.argv) not in (2, 3):
    raise SystemExit("usage: oci-nsg.py NSG_OCID [VNIC_OCID]")
nsg = sys.argv[1]
rules = [
    {"direction":"INGRESS","protocol":"6","source":"0.0.0.0/0","sourceType":"CIDR_BLOCK","isStateless":False,"tcpOptions":{"destinationPortRange":{"min":80,"max":80}},"description":"FamilyTunnel HTTP"},
    {"direction":"INGRESS","protocol":"6","source":"0.0.0.0/0","sourceType":"CIDR_BLOCK","isStateless":False,"tcpOptions":{"destinationPortRange":{"min":443,"max":443}},"description":"FamilyTunnel HTTPS"},
    {"direction":"INGRESS","protocol":"17","source":"0.0.0.0/0","sourceType":"CIDR_BLOCK","isStateless":False,"udpOptions":{"destinationPortRange":{"min":51820,"max":51820}},"description":"FamilyTunnel WireGuard"},
]

def run(*args):
    return subprocess.run(["oci", *args], check=True, text=True, capture_output=True)

current = json.loads(run("network", "nsg", "rules", "list", "--nsg-id", nsg, "--all").stdout).get("data", [])
def signature(r):
    opt = r.get("tcp-options") or r.get("udp-options") or r.get("tcpOptions") or r.get("udpOptions") or {}
    dst = opt.get("destination-port-range") or opt.get("destinationPortRange") or {}
    return (str(r.get("protocol")), r.get("source"), dst.get("min"), dst.get("max"))
missing = [r for r in rules if signature(r) not in {signature(x) for x in current}]
if missing:
    run("network", "nsg", "rules", "add", "--nsg-id", nsg, "--security-rules", json.dumps(missing), "--force")
if len(sys.argv) == 3:
    vnic = json.loads(run("network", "vnic", "get", "--vnic-id", sys.argv[2]).stdout)["data"]
    ids = sorted(set(vnic.get("nsg-ids", []) + [nsg]))
    run("network", "vnic", "update", "--vnic-id", sys.argv[2], "--nsg-ids", json.dumps(ids), "--force")
print("FamilyTunnel NSG rules present; VNIC attachment updated when requested.")
