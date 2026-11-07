#!/usr/bin/env python3
"""Synchronize only FamilyTunnel-owned OCI rules; unrelated rules remain intact."""
import argparse
import json
import os
import shutil
import sqlite3
import subprocess
import urllib.request

MARKER = "FamilyTunnel managed "

def camel(value):
    if isinstance(value, list):
        return [camel(item) for item in value]
    if isinstance(value, dict):
        return {key.split("-")[0] + "".join(p.title() for p in key.split("-")[1:]): camel(item) for key, item in value.items()}
    return value

def desired_rules(state_dir):
    ports = {("6", 80), ("6", 443), ("17", 51820)}
    with sqlite3.connect(os.path.join(state_dir, "state.db")) as db:
        ports.update(("6", row[0]) for row in db.execute("SELECT DISTINCT public_port FROM routes WHERE enabled=1"))
        ports.update(("6" if proto == "tcp" else "17", port) for proto, port in db.execute("SELECT protocol,public_port FROM port_forwards WHERE enabled=1"))
    rules = []
    for proto, port in sorted(ports):
        if not 1 <= port <= 65535:
            raise ValueError("Invalid stored port")
        options = "tcpOptions" if proto == "6" else "udpOptions"
        rules.append({"protocol": proto, "source": "0.0.0.0/0", "sourceType": "CIDR_BLOCK", "isStateless": False,
                      options: {"destinationPortRange": {"min": port, "max": port}},
                      "description": MARKER + proto + "/" + str(port)})
    return rules

def rule_key(rule):
    rule = camel(rule)
    options = rule.get("tcpOptions") or rule.get("udpOptions") or {}
    dst = options.get("destinationPortRange") or {}
    return (str(rule.get("protocol")), rule.get("source"), dst.get("min"), dst.get("max"),
            rule.get("isStateless", False), json.dumps(options.get("sourcePortRange"), sort_keys=True))

def run(*args):
    result = subprocess.run(["oci", *args], capture_output=True, text=True, timeout=8)
    if result.returncode:
        raise RuntimeError("OCI CLI request failed; verify authentication, IAM permissions and target OCID")
    return json.loads(result.stdout or "{}")

def sync_target(target, rules):
    kind, identifier = target["kind"], target["id"]
    if kind == "security-list":
        if not identifier.startswith("ocid1.securitylist."):
            raise ValueError("Invalid security list OCID")
        response = run("network", "security-list", "get", "--security-list-id", identifier)
        current = camel(response["data"].get("ingress-security-rules", []))
        unmanaged = [rule for rule in current if not (rule.get("description") or "").startswith(MARKER)]
        desired = unmanaged + [rule for rule in rules if rule_key(rule) not in {rule_key(old) for old in unmanaged}]
        if {json.dumps(rule, sort_keys=True) for rule in current} != {json.dumps(rule, sort_keys=True) for rule in desired}:
            etag = response.get("etag")
            if not etag:
                raise RuntimeError("OCI ETag missing; refusing to overwrite concurrent security-list changes")
            run("network", "security-list", "update", "--security-list-id", identifier,
                "--ingress-security-rules", json.dumps(desired), "--if-match", etag, "--force")
    elif kind == "nsg":
        if not identifier.startswith("ocid1.networksecuritygroup."):
            raise ValueError("Invalid NSG OCID")
        current = run("network", "nsg", "rules", "list", "--nsg-id", identifier, "--all").get("data", [])
        wanted = {rule_key(rule) for rule in rules}
        obsolete = [rule["id"] for rule in current if (rule.get("description") or "").startswith(MARKER)
                    and rule_key(rule) not in wanted]
        if obsolete:
            run("network", "nsg", "rules", "remove", "--nsg-id", identifier,
                "--security-rule-ids", json.dumps(obsolete))
        existing = {rule_key(rule) for rule in current if rule.get("direction") == "INGRESS" and rule.get("id") not in obsolete}
        missing = [dict(rule, direction="INGRESS") for rule in rules if rule_key(rule) not in existing]
        for offset in range(0, len(missing), 25):
            run("network", "nsg", "rules", "add", "--nsg-id", identifier, "--security-rules", json.dumps(missing[offset:offset + 25]))
    else:
        raise ValueError("Choose nsg or security-list")

def discover_target():
    request = urllib.request.Request("http://169.254.169.254/opc/v2/vnics/",
                                     headers={"Authorization": "Bearer Oracle"})
    # Metadata must never leave the VM through an environment-configured proxy.
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=2) as response:
        vnics = json.loads(response.read(65536))
    if not isinstance(vnics, list) or len(vnics) != 1:
        raise ValueError("Multiple network interfaces: choose the Oracle target once in advanced settings")
    vnic = run("network", "vnic", "get", "--vnic-id", vnics[0]["vnicId"])["data"]
    groups = vnic.get("nsg-ids", [])
    if len(groups) == 1:
        return {"enabled": True, "kind": "nsg", "id": groups[0]}
    if groups:
        raise ValueError("Multiple NSGs: choose the Oracle target once in advanced settings")
    subnet = run("network", "subnet", "get", "--subnet-id", vnic["subnet-id"])["data"]
    lists = subnet.get("security-list-ids", [])
    if len(lists) != 1:
        raise ValueError("Multiple security lists: choose the Oracle target once in advanced settings")
    return {"enabled": True, "kind": "security-list", "id": lists[0]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("nsg", nargs="?")
    parser.add_argument("vnic", nargs="?")
    parser.add_argument("--target-file")
    parser.add_argument("--discover-target", action="store_true")
    parser.add_argument("--state-dir", default="/var/lib/familytunnel")
    args = parser.parse_args()
    if args.discover_target:
        if not shutil.which("oci"):
            raise RuntimeError("OCI CLI is not installed")
        config = os.path.join(args.state_dir, "oci", "config")
        if os.path.isfile(config):
            os.environ["OCI_CLI_CONFIG_FILE"] = config
        else:
            os.environ.setdefault("OCI_CLI_AUTH", "instance_principal")
        print(json.dumps(discover_target()))
        return
    if args.target_file:
        with open(args.target_file, encoding="utf-8") as file:
            target = json.load(file)
        if not target.get("enabled"):
            return
    elif args.nsg:
        target = {"kind": "nsg", "id": args.nsg}
    else:
        parser.error("Specify an NSG OCID or --target-file")
    if not shutil.which("oci"):
        raise RuntimeError("OCI CLI is not installed")
    config = os.path.join(args.state_dir, "oci", "config")
    if os.path.isfile(config):
        os.environ["OCI_CLI_CONFIG_FILE"] = config
    else:
        os.environ.setdefault("OCI_CLI_AUTH", "instance_principal")
    sync_target(target, desired_rules(args.state_dir))
    if args.vnic:
        # This optional legacy action is only invoked explicitly from the CLI.
        vnic = run("network", "vnic", "get", "--vnic-id", args.vnic)["data"]
        ids = sorted(set(vnic.get("nsg-ids", []) + [args.nsg]))
        run("network", "vnic", "update", "--vnic-id", args.vnic, "--nsg-ids", json.dumps(ids), "--force")
    print("FamilyTunnel Oracle rules synchronized")

if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as error:
        raise SystemExit(str(error))
