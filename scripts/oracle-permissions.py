#!/usr/bin/env python3
"""One-time account-authorized bootstrap, run by the owner in OCI Cloud Shell.

Does not create a VM, collect credentials, change SSH, or grant tenancy admin.
The VM cannot grant these rights to itself without an account authorization.
"""
import argparse
import hashlib
import json
import re
import shutil
import subprocess


def validate_id(value, kind):
    if not re.fullmatch(r"ocid1\." + kind + r"\.[A-Za-z0-9._-]{1,480}", value):
        raise ValueError("Ungültige Oracle-Kennung: " + kind)
    return value


def statements(name, compartment):
    scope = " in tenancy" if compartment.startswith("ocid1.tenancy.") else " in compartment id " + compartment
    prefix = "Allow dynamic-group " + name + " to "
    return [
        prefix + "read vnics" + scope,
        prefix + "read subnets" + scope,
        prefix + "read security-lists" + scope,
        prefix + "manage security-lists" + scope + " where request.permission = 'SECURITY_LIST_UPDATE'",
        prefix + "read network-security-groups" + scope,
        prefix + "use network-security-groups" + scope + " where request.permission = 'NETWORK_SECURITY_GROUP_LIST_SECURITY_RULES'",
        prefix + "manage network-security-groups" + scope + " where request.permission = 'NETWORK_SECURITY_GROUP_UPDATE_SECURITY_RULES'",
    ]


def bootstrap(instance, compartment, region, call):
    validate_id(instance, "instance")
    if compartment.startswith("ocid1.tenancy."):
        validate_id(compartment, "tenancy")
    else:
        validate_id(compartment, "compartment")
    if not re.fullmatch(r"[a-z0-9-]{3,80}", region):
        raise ValueError("Ungültige Oracle-Region")
    root = compartment
    for _ in range(20):
        if root.startswith("ocid1.tenancy."):
            break
        root = call(region, "iam", "compartment", "get", "--compartment-id", root)["data"]["compartment-id"]
    else:
        raise ValueError("Oracle-Root-Compartment nicht eindeutig erkannt")
    tenancy = call(region, "iam", "tenancy", "get", "--tenancy-id", root)["data"]
    home_key = tenancy["home-region-key"]
    regions = call(region, "iam", "region", "list")["data"]
    home = next((row["name"] for row in regions if row["key"] == home_key), None)
    if not home:
        raise ValueError("Oracle-Heimatregion nicht erkannt")
    name = "FamilyTunnel-" + hashlib.sha256(instance.encode()).hexdigest()[:12]
    marker = "FamilyTunnel single-instance network bootstrap " + name
    rule = "ALL {instance.id = '" + instance + "'}"
    groups = call(home, "iam", "dynamic-group", "list", "--compartment-id", root, "--all")["data"]
    existing = [row for row in groups if row.get("name") == name]
    if existing:
        if len(existing) != 1 or existing[0].get("matching-rule") != rule or existing[0].get("description") != marker:
            raise ValueError("Bestehende fremde Oracle-Gruppe wird nicht verändert")
    else:
        call(home, "iam", "dynamic-group", "create", "--compartment-id", root,
             "--name", name, "--description", marker, "--matching-rule", rule)
    policies = call(home, "iam", "policy", "list", "--compartment-id", root, "--all")["data"]
    wanted = statements(name, compartment)
    existing = [row for row in policies if row.get("name") == name]
    if existing:
        if len(existing) != 1 or existing[0].get("description") != marker or set(existing[0].get("statements", [])) != set(wanted):
            raise ValueError("Bestehende abweichende Oracle-Policy wird nicht überschrieben")
    else:
        call(home, "iam", "policy", "create", "--compartment-id", root,
             "--name", name, "--description", marker, "--statements", json.dumps(wanted))
    return name


def call(region, *args):
    result = subprocess.run(["oci", *args, "--region", region, "--connection-timeout", "5",
                             "--read-timeout", "10", "--max-retries", "0"], capture_output=True, text=True, timeout=30)
    if result.returncode:
        raise RuntimeError("Oracle-Kontofreigabe fehlgeschlagen. In Cloud Shell mit einem berechtigten Kontoadministrator anmelden.")
    return json.loads(result.stdout or "{}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--instance", required=True)
    parser.add_argument("--compartment", required=True)
    parser.add_argument("--region", required=True)
    args = parser.parse_args()
    if not shutil.which("oci"):
        raise SystemExit("Diesen einmaligen Befehl in Oracle Cloud Shell ausführen; dort ist die Kontoanmeldung vorhanden.")
    name = bootstrap(args.instance, args.compartment, args.region, call)
    print("Oracle-Kontofreigabe eingerichtet:", name)
    print("Genau diese VM darf ihre Netzwerkfreigaben im angegebenen Compartment verwalten.")
    print("Keine Administrator-, Compute- oder SSH-Rechte und keine Rechte zum Löschen von Cloud-Ressourcen wurden vergeben.")
    print("Nach Übernahme der Oracle-Rechte den ursprünglichen Installer erneut ausführen; er findet und verwaltet die Freigaben automatisch.")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, ValueError, KeyError, OSError, subprocess.TimeoutExpired) as error:
        raise SystemExit(str(error))
