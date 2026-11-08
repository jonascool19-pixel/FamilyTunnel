"""Check the installer without extracting it or executing bundled programs."""
import hashlib
import re
import sys
import tarfile
from pathlib import PurePosixPath

ROOT_FILES = {
    "README.md", "TESTING.md", "SECURITY.md", "CONTRIBUTING.md", "install.sh", "install-familytunnel.sh",
    "install-server.sh", "install-client.sh", "bootstrap.sh", "purge.sh",
}
HELPERS = {"archive.py", "manage.sh", "firewall.sh", "raw-firewall.sh", "oci-nsg.py", "oracle-setup.sh", "oracle-permissions.py"}
BINARIES = {"familytunnel-linux-amd64", "familytunnel-linux-arm64"}
REQUIRED = ROOT_FILES | {"scripts/" + x for x in HELPERS} | {
    "dist/SHA256SUMS", *("dist/" + x for x in BINARIES),
    "systemd/familytunnel.service", "systemd/familytunnel-maintenance.timer",
    "systemd/familytunnel-maintenance.service",
}


def audit(path):
    files = {}
    with tarfile.open(path, "r:gz") as archive:
        for member in archive:
            name = PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or not name.parts or name.parts[0] != "familytunnel":
                raise ValueError("Unexpected archive path")
            if member.isdir():
                continue
            if not member.isfile() or member.size > 32 * 1024 * 1024:
                raise ValueError("Unexpected archive entry type or size")
            relative = "/".join(name.parts[1:])
            allowed = (
                relative in ROOT_FILES
                or relative in {"scripts/" + x for x in HELPERS}
                or relative in {"dist/SHA256SUMS", *("dist/" + x for x in BINARIES)}
                or (len(name.parts) == 3 and name.parts[1] == "docs" and name.suffix == ".md")
                or (len(name.parts) == 3 and name.parts[1] == "systemd" and name.suffix in {".service", ".timer"})
            )
            if not allowed or relative in files:
                raise ValueError("Unexpected or duplicate installer file: " + relative)
            data = archive.extractfile(member).read()
            if re.search(rb"-----BEGIN (?:OPENSSH |RSA |EC )?PRIVATE KEY-----\s+[A-Za-z0-9+/=]{32}", data):
                raise ValueError("Embedded private key in installer")
            files[relative] = data
    missing = REQUIRED - files.keys()
    if missing:
        raise ValueError("Missing installer files: " + ", ".join(sorted(missing)))
    checks = {}
    for line in files["dist/SHA256SUMS"].decode("ascii").splitlines():
        digest, filename = line.split()
        if filename not in BINARIES or not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise ValueError("Invalid binary checksum list")
        checks[filename] = digest
    if checks.keys() != BINARIES:
        raise ValueError("Incomplete binary checksum list")
    for filename, digest in checks.items():
        data = files["dist/" + filename]
        if not data.startswith(b"\x7fELF") or hashlib.sha256(data).hexdigest() != digest:
            raise ValueError("Invalid Linux binary or checksum")
    return len(files)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("Usage: python3 tests/package-audit.py INSTALL.tar.gz")
    print("Installer allowlist and checksums passed:", audit(sys.argv[1]), "files; no demos or saved state")
