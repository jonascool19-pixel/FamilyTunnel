# Abnahmetest auf zwei Ubuntu-24.04-Systemen

Für eine Freigabe werden ein frischer Oracle-VPS, ein frischer unprivilegierter Proxmox-CT, eine Testdomain und ein HTTP-Backend im Heimnetz verwendet.

1. VPS mit dem Server-Befehl installieren, ausgegebenen Pairing-Code kopieren und in den Client-Installer des CT einfügen. Prüfen: ungültiger oder abgeschnittener Code wird abgelehnt; gültiger Code wird verdeckt eingelesen; Dienste aktiv, Datenbank `ok`, Initialpasswort vorhanden, GUI nur auf interner Adresse erreichbar.
2. Installer auf beiden Seiten erneut ausführen. Vorher und nachher SHA-256 der `config.json`, `ft0.conf`, Datenbank und Pairing-Datei vergleichen. Schlüssel und Daten müssen gleich bleiben.
3. Beide Systeme neu starten. Prüfen: Tunnel-Handshake innerhalb von 90 Sekunden, Controller/Caddy aktiv, GUI und Testdomain erreichbar.
4. `systemctl stop wg-quick@ft0` im CT, mindestens zwei Reparaturintervalle warten. Prüfen: Timer startet Tunnel neu; Caddy liefert während der Unterbrechung 502 statt einen falschen Dienst.
5. A-Record absichtlich auf eine andere IPv4 ändern. Prüfen: `dns_matches_vps: false` in der GUI. Zurückändern und TTL abwarten; Status wird wieder wahr.
6. Eine gültige Route anlegen, bearbeiten, deaktivieren, aktivieren und löschen. Vor und nach jeder Änderung `curl -fsS http://127.0.0.1:2019/config/` auf dem VPS prüfen. Caddy bleibt aktiv; ungültige Domains/Backends werden abgewiesen.
7. Backend stoppen. Prüfen: GUI-Backendtest meldet `unreachable`, externe Anfrage erhält 502, Fehlerzähler steigt. Backend starten; Erreichbarkeit kehrt zurück.
8. Route und Passwort ändern, beide Systeme neu starten. Prüfen: Datenbankzustand und Anmeldung bleiben erhalten.
9. `familytunnel backup` ausführen, Route ändern, Backup wiederherstellen. Prüfen: vorheriger Zustand ist zurück und automatisch ein Vor-Restore-Backup entstanden.
10. Testrelease mit absichtlich fehlerhaftem Binary/Dienststart bereitstellen und `familytunnel update URL` ausführen. Prüfen: Rückgabecode ungleich null, Symlink zeigt wieder auf die alte Version, Dienst läuft. Danach ein gültiges höheres Release testen.
11. `journalctl -u familytunnel` und SQLite-Ereignisse nach Token, WireGuard-Private-Key und Passwort durchsuchen. Es dürfen keine Treffer vorhanden sein.
12. VPS mit `ss -lntup` prüfen: öffentlich nur SSH gemäß eigener Regel sowie 80/TCP, 443/TCP und 51820/UDP. 2019, 8443, 9080 und 9081 dürfen nicht öffentlich erreichbar sein.
