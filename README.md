# FamilyTunnel 0.1.0

FamilyTunnel veröffentlicht HTTP-/HTTPS-Webdienste sowie ausgewählte TCP-/UDP-Dienste aus einem Heimnetz über eine Oracle-Cloud-VM. Ein Ubuntu-24.04-LXC baut WireGuard ausgehend auf; am Heimrouter ist keine Portfreigabe nötig. Caddy beendet TLS für Webseiten. Direkte TCP-/UDP-Freigaben verwenden eigene NAT-/FORWARD-Regeln durch WireGuard.

Neu: **Dienste suchen und einrichten** verbindet Web- und Portsuche. Erkannte Dienstnamen und Ports erscheinen nach lokaler IP gruppiert. Ein Gerät oder einzelne Dienste auswählen; HTTPS (mit einmal hinterlegter Domain), direkte Ports und Oracle-Freigaben werden im Hintergrund eingerichtet. Eine gemeinsame Liste zeigt aktivierte Dienste und ihre Adressen. [Details und Grenzen](docs/tcp-udp.md). Unter Neustarts gibt es einen frei einstellbaren, zunächst ausgeschalteten [koordinierten Zeitplan](docs/maintenance-design.md) und einen ausdrücklich bestätigten echten Neustart-Test.

## Eine Domain, mehrere HTTPS-Ports

Mehrere Webdienste können dieselbe Domain verwenden. Jede Weiterleitung hat einen eigenen `public_port`; Domain und öffentlicher Port müssen zusammen eindeutig sein. Beispiel: `https://mein-zugang.example.net:8096/` leitet nach `http://192.168.1.30:8096`, während `https://mein-zugang.example.net:8123/` auf einen anderen Heimdienst zeigt. Öffentlicher und interner Port dürfen unterschiedlich sein. Die Hauptadresse ohne Port verwendet weiterhin 443.

Die Heimnetzsuche schlägt den gefundenen Port vor. Für niedrige oder reservierte Ports wird 443 vorgeschlagen. Jeder Dienst wird einzeln veröffentlicht; die Oberfläche zeigt seine fertige HTTPS-Adresse. Unterstützt: 443 oder 1024–65535, ausgenommen die internen Ports 2019, 8443, 9080 und 9081. Dies sind HTTPS-Weiterleitungen für Webdienste, keine beliebigen TCP-/UDP-Tunnel. Die bestehende Einschränkung für WebSockets bleibt bestehen.

**Aktualisierung:** zuerst Oracle-Server, danach Heim-CT mit dem Installer aktualisieren. Die Rolle wird aus der bestehenden Konfiguration übernommen. Der Installer sichert die vorhandene Datenbank nach `/var/backups/familytunnel/pre-install-*.db`; die Migration übernimmt bestehende Weiterleitungen unverändert auf Port 443. Ein alter Server wird vor der Synchronisierung zusätzlicher Ports erkannt und muss zuerst aktualisiert werden.

Die Ubuntu-Firewall auf Oracle übernimmt ausschließlich die aktiven veröffentlichten TCP-Ports und entfernt deren FamilyTunnel-Freigabe beim Deaktivieren/Löschen. **In der OCI-Sicherheitsliste oder einer zugewiesenen NSG muss jeder zusätzliche öffentliche TCP-Port ebenfalls freigegeben werden.** Beispiel: Quelle `0.0.0.0/0`, Protokoll TCP, Quellports alle, Zielport 8096, zustandsbehaftet. Ohne die optionale OCI-Anbindung werden Cloud-Regeln nicht automatisch geändert; mit eingerichteter Anbindung gilt der Abschnitt TCP/UDP-Automation. 80/TCP muss für die Zertifikatsausstellung erreichbar bleiben; 443/TCP und 51820/UDP bleiben wie bisher freigegeben.

„Öffentlich testen“ prüft den ausgewählten Port über TLS mit Zertifikatsprüfung und verlangt eine an Domain und Port gebundene Antwortbestätigung des Heim-Controllers. Die Prüfung startet vom CT über die öffentliche Oracle-IP; sie ist kein Test aus allen externen Netzen.

Porttests: `node scripts/test-web.cjs` und `go test ./...`. Mit gesetztem `CADDY_BIN` wird zusätzlich die echte Caddy-Konfigurationsumwandlung geprüft.

## Voraussetzungen

- Oracle Ubuntu 24.04 VM mit öffentlicher IPv4 und zugewiesener Sicherheitsliste oder NSG
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

Auf der Oracle-Instanz und auf dem Proxmox-CT wird derselbe Befehl verwendet:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-familytunnel.sh | sudo bash
```

Der Installer zeigt ein nummeriertes Menü. Auf der Oracle-Instanz wird `1` gewählt, auf dem Proxmox-CT `2`:

```text
Wo soll FamilyTunnel installiert werden?
  1) Server (Oracle Cloud-Instanz / öffentlicher VPS)
  2) Client (Proxmox Ubuntu CT zu Hause)
Auswahl [1 oder 2]:
```

Der SSH-Schlüssel der Oracle-Instanz dient nur zur Anmeldung bei Oracle und wird auf dem Proxmox-CT nicht verwendet.

### 1. Oracle-Cloud-Instanz

Dort den gemeinsamen Installationsbefehl ausführen und `1` wählen. Der folgende rollenfeste Befehl bleibt für automatisierte Installationen verfügbar:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-server.sh | sudo bash
```

Am Ende zeigt der Installer einen deutlich markierten **Pairing-Code für den Heim-CT** an. Der vollständige Code wird kopiert und später in das Eingabefeld des Client-Installers eingefügt. Er enthält geheime Zugangsdaten und gehört weder in einen Chat noch ins GitHub-Repository. Die zugrunde liegende Datei bleibt zusätzlich unter `/var/lib/familytunnel/pairing.json` gespeichert.

### 2. Proxmox-CT zu Hause

Im Terminal des Ubuntu-24.04-CT den gemeinsamen Installationsbefehl ausführen und `2` wählen. Der folgende rollenfeste Befehl bleibt für automatisierte Installationen verfügbar:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-client.sh | sudo bash
```

Der Installer fragt direkt nach dem Pairing-Code. Die eingefügte Zeichenfolge bleibt zur eindeutigen Kontrolle sichtbar; deshalb sollte währenddessen niemand den Bildschirm mitlesen. Der Installer legt daraus automatisch die geschützte Konfigurationsdatei an und prüft deren Inhalt. Anschließend erkennt er die private IPv4 des CT und zeigt sie als Vorgabe in eckigen Klammern. Wenn die angezeigte Adresse stimmt, genügt `Enter`. Der Oracle-SSH-Schlüssel und eine manuelle Dateiübertragung werden nicht benötigt.

## Vollständiger Reset

Der Reset entfernt FamilyTunnel-Dienste, Konfiguration, Datenbank, Schlüssel, WireGuard-Profil und FamilyTunnel-Backups. Auf der Oracle-Seite wird auch das ausschließlich für FamilyTunnel eingerichtete Caddy entfernt. Allgemeine Ubuntu-Pakete und Systemupdates bleiben erhalten.

Oracle-Server vollständig zurücksetzen und direkt frisch installieren:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/purge.sh | sudo bash && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-familytunnel.sh | sudo bash
```

Proxmox-CT vollständig zurücksetzen und direkt frisch installieren:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/purge.sh | sudo bash && curl -fsSL https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-familytunnel.sh | sudo bash
```

Der Installer:

- aktualisiert Ubuntu und installiert WireGuard, Caddy, SQLite und Hilfspakete;
- prüft Architektur, Ubuntu-Version, Prüfsummen und WireGuard-Kernelunterstützung;
- erzeugt WireGuard-Schlüssel, ein 256-Bit-Pairing-Token und ein zufälliges Admin-Passwort;
- richtet native systemd-Dienste, begrenzte Journal-Logs und Firewall-Regeln ein;
- führt Crypto-, Dienst-, Datenbank- und Dateiselbsttests aus;
- ist bei erneuter Ausführung idempotent und behält Konfiguration, Schlüssel und Datenbank.

Das Initialpasswort wird nur root-lesbar unter `/var/lib/familytunnel/initial-password` gespeichert. Nach der ersten Anmeldung sollte es geändert und die Datei gelöscht werden. Das selbstsignierte Zertifikat der internen Verwaltung wird mit SHA-256-Fingerabdruck angezeigt. Dieser Fingerabdruck muss vor einer Browser-Ausnahme geprüft werden.

Nach erfolgreicher Installation erscheint ganz am Ende ein farbiger Abschlusskasten mit Webadresse, Benutzername `admin`, tatsächlichem Startpasswort und installierter Rolle. Nach einer Passwortänderung wird die Datei mit dem Startpasswort automatisch entfernt und bei späteren Reparaturinstallationen kein veraltetes Passwort angezeigt.

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
- Automatisch erzeugtes Startpasswort mit 32 Zeichen; selbst gewählte Passwörter müssen 6–256 Zeichen lang sein
- Secure/HttpOnly/SameSite-Strict-Sitzungscookie, CSRF-Token, Login-Limit, maximal 64 Sitzungen
- Admin-GUI des CT nur auf einer tatsächlich vorhandenen privaten RFC1918-Adresse; Server-GUI nur im Tunnel
- Caddy-Admin-API nur auf `127.0.0.1`; internes Proxy/API nur auf WireGuard-Adressen
- Backend-Ziele müssen private LAN-IP-Adressen sein; Loopback und Tunneladressen werden abgewiesen
- Pairing-Endpunkte authentifiziert; Firewall beschränkt sie zusätzlich auf `ft0`
- Keine Schlüssel, Tokens oder Passwörter in Anwendungslogs; Journal-Rate-Limit und maximal 1.000 Datenbankereignisse
- SSH-Regeln werden absichtlich nicht verändert, damit der Installer keinen Fernzugriff sperrt oder unnötig öffnet

## Statistiken und Healthchecks

Der Heim-Controller zählt HTTP-Requests, Fehler sowie eingehende und ausgehende HTTP-Nutzdaten pro Domain und UTC-Stunde. Die GUI aggregiert Stunde, Tag, Monat oder Gesamt und zeigt Datenmengen automatisch als kB/MB/GB/TB. Der Statistikreset leert nach Bestätigung gespeicherte Werte und den noch offenen Zählerpuffer. TLS-, TCP- und WireGuard-Overhead ist nicht enthalten. Healthchecks prüfen WireGuard-Handshakes, SQLite, Server-Synchronisierung, Caddy, DNS-A-Auflösung und Backend-TCP-Erreichbarkeit. Der Reparatur-Timer läuft alle zwei Minuten.

Der Verbindungsstatus in der Weboberfläche aktualisiert sich automatisch alle 15 Sekunden. Der manuelle Knopf bleibt für eine sofortige Aktualisierung erhalten. Der Installer verwendet auf Server und Client farbige Banner, Fortschrittsstufen und einen eingerahmten Abschlussbereich.

Eine Weiterleitung verbindet eine öffentliche Domain mit einem lokalen Programm. Neutrales Beispiel: Der DNS-A-Eintrag `mein-zugang.example.net` zeigt auf die öffentliche Oracle-IPv4; als Backend wird die vom Heim-CT erreichbare Adresse `http://192.168.1.30:8096` eingetragen. Aufgerufen wird der Dienst anschließend über `https://mein-zugang.example.net`. Domainfelder enthalten kein `https://`; Backendfelder enthalten immer Protokoll, private IP und Port. Das öffentliche Projekt enthält keine persönliche Domain als Vorgabe.

In der Hauptansicht genügt „Heimnetz durchsuchen“. Die Suche kombiniert offene TCP-Ports und bestätigte HTTP-/HTTPS-Dienste, zeigt erkannte Seitentitel und gruppiert alles nach lokaler IP. Die öffentliche Domain wird einmal eingetragen oder aus eindeutigen vorhandenen Webseiten übernommen. Gewünschte Dienste oder ganze Geräte auswählen und „Auswahl einrichten“ anklicken: Webdienste erhalten Caddy-HTTPS, andere Dienste direkte Portweiterleitungen. Die gemeinsame Liste „Aktivierte Dienste“ zeigt gespeicherte Adressen sofort. Die Einrichtung läuft im Hintergrund; fehlende Oracle-Rechte, DNS- und Zertifikatsprobleme sind getrennt zu prüfen. Suchtreffer und Dienstnamen bleiben in SQLite erhalten. UDP ist ohne passende Dienstantwort nicht automatisch erkennbar. Unter „Ereignisse und Fehlerhilfe“ steht ein kopierbarer Diagnosebericht ohne Passwörter, Schlüssel oder Sitzungstoken bereit.

## Getestet

Automatisiert geprüft werden Build für Linux amd64/arm64, Passwort-Hashing, Eingabevalidierung, SSRF-Schutz für Backends, Caddy-Konfiguration, atomare Routenänderung und SQLite-Persistenz. `tests/static-checks.sh` prüft Shell-Syntax, systemd-Dateien und Release-Prüfsummen, sofern die jeweiligen Programme vorhanden sind.

Die echte Neuinstallation, Wiederholungsinstallation, Neustart-, Tunnelabbruch-, DNS-, Caddy-, Backend-, Backup/Restore- und Update-Rollback-Szenarien sind als Runbook in [TESTING.md](TESTING.md) beschrieben. Sie benötigen zwei echte Ubuntu-Systeme, OCI-Zugang, DNS und einen Backend-Dienst und können deshalb nicht in einer Windows-Arbeitsumgebung simuliert werden.

## Bekannte Grenzen

- HTTP/HTTPS und direkte TCP/UDP-Freigaben sind implementiert. WebSocket-Upgrades bleiben in dieser Version nicht unterstützt.
- DDNS wird geprüft und bei falscher IP gewarnt. Automatisches Ändern ist ohne Anbieter und Zugangsdaten nicht möglich.
- OCI-Regeln werden nur automatisiert, wenn eine konfigurierte OCI CLI mit passenden IAM-Rechten vorhanden ist.
- Das Installationspaket wird aktuell aus dem `main`-Branch geladen. Für unveränderliche Produktionsinstallationen sollte künftig ein signiertes GitHub-Release verwendet werden.
- Das interne Admin-Zertifikat ist selbstsigniert. Öffentliche Dienstzertifikate verwaltet Caddy automatisch.
- Mehrere Heim-CTs oder mehrere WireGuard-Peers werden in 0.1.0 nicht unterstützt.

## Quellen

- Caddy API und Caddyfile-Adapter: <https://caddyserver.com/docs/api>
- Caddy Reverse Proxy: <https://caddyserver.com/docs/caddyfile/directives/reverse_proxy>
- Oracle NSG-Regeln: <https://docs.oracle.com/en-us/iaas/Content/Network/Concepts/securityrules.htm>
