# Abnahmetest auf zwei Ubuntu-24.04-Systemen

Für eine Freigabe werden ein frischer Oracle-VPS, ein frischer unprivilegierter Proxmox-CT, eine Testdomain und ein HTTP-Backend im Heimnetz verwendet.

1. VPS mit dem Server-Befehl installieren, ausgegebenen Pairing-Code kopieren und in den Client-Installer des CT einfügen. Prüfen: ungültiger oder abgeschnittener Code wird abgelehnt; eingefügter Code ist zur Kontrolle sichtbar; die erkannte private CT-IP wird als Enter-Standard angeboten; Dienste aktiv, Datenbank `ok`, Initialpasswort vorhanden, GUI nur auf interner Adresse erreichbar.
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
12. VPS mit `ss -lntup` prüfen: öffentlich nur SSH gemäß eigener Regel, 80/TCP, 443/TCP, 51820/UDP sowie die ausdrücklich veröffentlichten zusätzlichen HTTPS-TCP-Ports. 2019, 8443, 9080 und 9081 dürfen nicht öffentlich erreichbar sein.
13. Server zuerst, danach CT aktualisieren. Vorhandene Route bleibt auf 443; dieselbe Domain zusätzlich auf 8096 und 8123 anlegen. Öffentliche Links müssen unterschiedliche Backends erreichen. Nach Neustart, Backup/Restore und erneutem Installieren Ports und Ziele prüfen. Mit geschlossener OCI-Freigabe muss der öffentliche Test fehlschlagen; nach Freigabe des passenden TCP-Ports erfolgreich sein. Deaktivieren/Löschen entfernt Caddy-Listener und FamilyTunnel-Hostfirewall-Freigabe (bei anderen aktiven Domains auf demselben Port bleibt dessen Freigabe bestehen). Ein Client mit Zusatzports darf nicht stillschweigend auf einen alten Server synchronisieren.

## Oberfläche, Rücksetzen und koordinierte Wartung

- Statistikreset synchronisiert SQLite mit dem noch ungeflushten Speicherpuffer; neue Zugriffe zählen anschließend erneut.
- Suchreset wartet auf den beendeten Suchworker, bevor Treffer, Protokollerkennung und inaktive passende Vorschläge gelöscht werden. Aktivierte Dienste und deren Zertifikatbindung bleiben erhalten.
- TCP+UDP-Änderungen und Webdienst mit optionalem UDP werden atomar gespeichert; Konflikte ändern keinen Teil der Konfiguration.
- HTTPS-Prüfungen testen auch langsame private TLS-Endpunkte und Server, die einen bestimmten SNI-Domainnamen verlangen. Dieser Name wird zusammen mit dem Zertifikat-Fingerprint gespeichert und im Backend-Proxy verwendet.
- `FT_TEST_LAN_IP`, `FT_TEST_LAN_PORT` und `FT_TEST_LAN_DOMAIN` erlauben einen ausdrücklich aktivierten lokalen Diagnosetest (`go test -run TestOptionalLiveNamedHTTPS -v ./internal/app`). Keine privaten Produktionsadressen sind als Testvorgaben hinterlegt.
- Die neuen Wartungstests verwenden simulierte Uhren, Boot-Identitäten und Reboot-Funktionen. Sie führen keine realen Neustarts aus. GitHub Actions prüft zusätzlich auf Datenrennen.
- MB/GB in der Oberfläche sind dezimale Datenmengen, keine Mbit/s-Durchsatzmessung. Die Statistik erfasst weiterhin HTTP-Nutzdaten; direkte TCP-/UDP-Verbindungen erhalten dadurch keine neuen Verkehrszähler.
