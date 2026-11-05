# FamilyTunnel 0.1.0

FamilyTunnel veröffentlicht HTTP-Dienste aus einem Heimnetz über eine Oracle-Cloud-VM. Ein Ubuntu-24.04-LXC baut den WireGuard-Tunnel ausgehend auf; am Heimrouter ist keine Portfreigabe nötig. Caddy auf dem VPS beendet TLS und sendet Anfragen durch den Tunnel an den Controller im CT, der sie an private Heimnetz-Adressen weiterleitet.

## Voraussetzungen

- Oracle Ubuntu 24.04 VM mit öffentlicher IPv4 und bestehendem NSG
- Proxmox Ubuntu 24.04 LXC, bevorzugt unprivilegiert, 1 Core, 1 GB RAM, 8 GB Disk
- WireGuard-Unterstützung des Proxmox-Host-Kernels muss im CT verfügbar sein
- DNS-A/AAAA-Konfiguration: Ein A-Record jedes Dienstes zeigt auf die VPS-IPv4; ein irreführender AAAA-Record sollte entfernt werden
- Dedizierte VM für FamilyTunnel; ein bereits anderweitig verwaltetes Caddy wird nicht überschrieben

## Build

Auf einem Rechner mit Go 1.23 oder neuer:

```bash
bash ./scripts/build.sh
```

Das erzeugt statische Linux-Binaries für amd64 und arm64 mit Prüfsummen in `dist/`. Der Installer akzeptiert ausschließlich diese geprüften Dateien.

## Installation

FamilyTunnel wird auf zwei Systemen installiert. Der SSH-Schlüssel der Oracle-Instanz dient nur zur Anmeldung bei Oracle und wird auf dem Proxmox-CT nicht verwendet.

### 1. Oracle-Cloud-Instanz

Diesen Befehl im SSH-Terminal der Oracle-VM ausführen. Er wählt automatisch die Rolle `server`:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-server.sh | sudo bash
```

Am Ende zeigt der Installer einen deutlich markierten **Pairing-Code für den Heim-CT** an. Der vollständige Code wird kopiert und später in das Eingabefeld des Client-Installers eingefügt. Er enthält geheime Zugangsdaten und gehört weder in einen Chat noch ins GitHub-Repository. Die zugrunde liegende Datei bleibt zusätzlich unter `/var/lib/familytunnel/pairing.json` gespeichert.

### 2. Proxmox-CT zu Hause

Im Terminal des Ubuntu-24.04-CT folgenden Befehl ausführen. Er wählt automatisch die Rolle `client`:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-client.sh | sudo bash
```

Der Installer fragt direkt nach dem Pairing-Code. Die eingefügte Zeichenfolge bleibt zur eindeutigen Kontrolle sichtbar; deshalb sollte währenddessen niemand den Bildschirm mitlesen. Der Installer legt daraus automatisch die geschützte Konfigurationsdatei an und prüft deren Inhalt. Anschließend erkennt er die private IPv4 des CT und zeigt sie als Vorgabe in eckigen Klammern. Wenn die angezeigte Adresse stimmt, genügt `Enter`. Der Oracle-SSH-Schlüssel und eine manuelle Dateiübertragung werden nicht benötigt.

## Vollständiger Reset

Der Reset entfernt FamilyTunnel-Dienste, Konfiguration, Datenbank, Schlüssel, WireGuard-Profil und FamilyTunnel-Backups. Auf der Oracle-Seite wird auch das ausschließlich für FamilyTunnel eingerichtete Caddy entfernt. Allgemeine Ubuntu-Pakete und Systemupdates bleiben erhalten.

Oracle-Server vollständig zurücksetzen und direkt frisch installieren:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/purge.sh | sudo bash && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-server.sh | sudo bash
```

Proxmox-CT vollständig zurücksetzen und direkt frisch installieren:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/purge.sh | sudo bash && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-client.sh | sudo bash
```

Der Installer:

- aktualisiert Ubuntu und installiert WireGuard, Caddy, SQLite und Hilfspakete;
- prüft Architektur, Ubuntu-Version, Prüfsummen und WireGuard-Kernelunterstützung;
- erzeugt WireGuard-Schlüssel, ein 256-Bit-Pairing-Token und ein zufälliges Admin-Passwort;
- richtet native systemd-Dienste, begrenzte Journal-Logs und Firewall-Regeln ein;
- führt Crypto-, Dienst-, Datenbank- und Dateiselbsttests aus;
- ist bei erneuter Ausführung idempotent und behält Konfiguration, Schlüssel und Datenbank.

Das Initialpasswort wird nur root-lesbar unter `/var/lib/familytunnel/initial-password` gespeichert. Nach der ersten Anmeldung sollte es geändert und die Datei gelöscht werden. Das selbstsignierte Zertifikat der internen Verwaltung wird mit SHA-256-Fingerabdruck angezeigt. Dieser Fingerabdruck muss vor einer Browser-Ausnahme geprüft werden.

## Oracle NSG

Wenn die OCI CLI eingerichtet und berechtigt ist:

```bash
sudo /usr/local/lib/familytunnel/oci-nsg.py ocid1.networksecuritygroup... ocid1.vnic...
```

Das Skript ergänzt idempotent 80/TCP, 443/TCP und 51820/UDP und kann das NSG an die VNIC hängen. Es entfernt oder öffnet SSH nicht. Ohne OCI-Zugang müssen dieselben drei Regeln in der Oracle-Konsole gesetzt werden. Oracle empfiehlt NSGs statt Security Lists.

## Bedienung

```text
familytunnel status
familytunnel repair
familytunnel backup [ZIEL.tar.gz]
familytunnel restore BACKUP.tar.gz
familytunnel update HTTPS-RELEASE.tar.gz
familytunnel uninstall --yes
```

`repair` prüft und startet nur FamilyTunnel, WireGuard, die eigenen Firewall-Regeln und auf dem VPS Caddy. `backup` verwendet die konsistente SQLite-Backup-API. `restore` prüft Pfade und Datenbankintegrität und legt vorher eine Sicherung an. `update` prüft die veröffentlichte SHA-256-Datei, installiert in ein neues Versionsverzeichnis und schaltet den Symlink atomar um; bei einem fehlgeschlagenen Dienststart wird zurückgerollt. `uninstall` legt zuerst ein Backup an und behält `/var/backups/familytunnel` sowie das Caddy-Paket.

## Sicherheitsmodell

- Argon2id: 64 MiB, drei Durchläufe, parallele Login-Hashes begrenzt
- Secure/HttpOnly/SameSite-Strict-Sitzungscookie, CSRF-Token, Login-Limit, maximal 64 Sitzungen
- Admin-GUI des CT nur auf einer tatsächlich vorhandenen privaten RFC1918-Adresse; Server-GUI nur im Tunnel
- Caddy-Admin-API nur auf `127.0.0.1`; internes Proxy/API nur auf WireGuard-Adressen
- Backend-Ziele müssen private LAN-IP-Adressen sein; Loopback und Tunneladressen werden abgewiesen
- Pairing-Endpunkte authentifiziert; Firewall beschränkt sie zusätzlich auf `ft0`
- Keine Schlüssel, Tokens oder Passwörter in Anwendungslogs; Journal-Rate-Limit und maximal 1.000 Datenbankereignisse
- SSH-Regeln werden absichtlich nicht verändert, damit der Installer keinen Fernzugriff sperrt oder unnötig öffnet

## Statistiken und Healthchecks

Der Heim-Controller zählt HTTP-Requests, Fehler sowie eingehende und ausgehende HTTP-Nutzdaten pro Domain und UTC-Stunde. Die GUI aggregiert Stunde, Tag, Monat oder Gesamt. TLS-, TCP- und WireGuard-Overhead ist nicht enthalten. Healthchecks prüfen WireGuard-Handshakes, SQLite, Server-Synchronisierung, Caddy, DNS-A-Auflösung und Backend-TCP-Erreichbarkeit. Der Reparatur-Timer läuft alle zwei Minuten.

## Getestet

Automatisiert geprüft werden Build für Linux amd64/arm64, Passwort-Hashing, Eingabevalidierung, SSRF-Schutz für Backends, Caddy-Konfiguration, atomare Routenänderung und SQLite-Persistenz. `tests/static-checks.sh` prüft Shell-Syntax, systemd-Dateien und Release-Prüfsummen, sofern die jeweiligen Programme vorhanden sind.

Die echte Neuinstallation, Wiederholungsinstallation, Neustart-, Tunnelabbruch-, DNS-, Caddy-, Backend-, Backup/Restore- und Update-Rollback-Szenarien sind als Runbook in [TESTING.md](TESTING.md) beschrieben. Sie benötigen zwei echte Ubuntu-Systeme, OCI-Zugang, DNS und einen Backend-Dienst und können deshalb nicht in einer Windows-Arbeitsumgebung simuliert werden.

## Bekannte Grenzen

- Version 0.1.0 veröffentlicht HTTP/HTTPS-Backends; rohe TCP/UDP-Dienste und WebSocket-Upgrades sind nicht implementiert.
- DDNS wird geprüft und bei falscher IP gewarnt. Automatisches Ändern ist ohne Anbieter und Zugangsdaten nicht möglich.
- OCI-Regeln werden nur automatisiert, wenn eine konfigurierte OCI CLI mit passenden IAM-Rechten vorhanden ist.
- Das Installationspaket wird aktuell aus dem `main`-Branch geladen. Für unveränderliche Produktionsinstallationen sollte künftig ein signiertes GitHub-Release verwendet werden.
- Das interne Admin-Zertifikat ist selbstsigniert. Öffentliche Dienstzertifikate verwaltet Caddy automatisch.
- Mehrere Heim-CTs oder mehrere WireGuard-Peers werden in 0.1.0 nicht unterstützt.

## Quellen

- Caddy API und Caddyfile-Adapter: <https://caddyserver.com/docs/api>
- Caddy Reverse Proxy: <https://caddyserver.com/docs/caddyfile/directives/reverse_proxy>
- Oracle NSG-Regeln: <https://docs.oracle.com/en-us/iaas/Content/Network/Concepts/securityrules.htm>
