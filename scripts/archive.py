#!/usr/bin/env python3
"""Extract a bounded FamilyTunnel archive containing only plain files/directories."""
import pathlib
import shutil
import sys
import tarfile


def extract(source, destination, kind):
    root = pathlib.Path(destination).resolve()
    seen = set()
    total = 0
    with tarfile.open(source, "r:gz") as archive:
        members = []
        for item in archive:
            name = pathlib.PurePosixPath(item.name)
            if (name.is_absolute() or ".." in name.parts or "\\" in item.name
                    or any(ord(c) < 32 for c in item.name)
                    or not (item.isfile() or item.isdir()) or item.mode & 0o7000):
                raise ValueError("Unsicherer Archiveintrag")
            relative = name.as_posix()
            if relative == "." and item.isdir():
                continue
            if relative in seen or len(seen) >= 4096 or item.size < 0:
                raise ValueError("Doppelter oder zu großer Archiveintrag")
            seen.add(relative)
            total += item.size
            if total > 1024 * 1024 * 1024:
                raise ValueError("Archiv überschreitet 1 GiB")
            if kind == "update" and relative not in {"release.json", "familytunnel-linux-amd64", "familytunnel-linux-arm64"}:
                raise ValueError("Unerwartete Datei im Update")
            if kind == "install" and (not name.parts or name.parts[0] != "familytunnel"):
                raise ValueError("Unerwarteter Installationspfad")
            if kind == "backup" and not (relative in {
                "state.db", "config.json", "ft0.conf", "admin.crt", "admin.key",
                "pairing.json", "initial-password", "oci-target.json", "site-verification.json",
            } or name.parts[0] == "oci"):
                raise ValueError("Unerwartete Datei im Backup")
            target = root.joinpath(*name.parts)
            if not target.resolve().is_relative_to(root):
                raise ValueError("Archivpfad verlässt das Zielverzeichnis")
            members.append((item, target))
        # Validate the entire archive before writing anything. Never restore links,
        # devices, ownership, setuid bits or archive-provided directory permissions.
        for item, target in members:
            if item.isdir():
                target.mkdir(parents=True, exist_ok=True, mode=0o700)
            else:
                target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                with archive.extractfile(item) as src, target.open("xb") as dst:
                    shutil.copyfileobj(src, dst, 1024 * 1024)
                target.chmod(0o700 if item.mode & 0o111 else 0o600)


if __name__ == "__main__":
    try:
        kind, source, destination = sys.argv[1:]
        if kind not in {"install", "update", "backup"}:
            raise ValueError("Ungültiger Archivtyp")
        extract(source, destination, kind)
    except (ValueError, OSError, tarfile.TarError) as error:
        raise SystemExit("Archiv abgelehnt: " + str(error))
