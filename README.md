# FamilyTunnel

**Heimdienste über eine Oracle-Cloud-VM erreichbar machen – mit WireGuard, HTTPS und einer lokalen Webverwaltung.**

FamilyTunnel verbindet einen Ubuntu-24.04-Container zu Hause mit einer öffentlichen Oracle-Cloud-VM. Der Heim-CT baut den Tunnel ausgehend auf. Eine Portfreigabe am Heimrouter ist dafür nicht nötig. Installation und Betrieb erfolgen als native systemd-Dienste; Docker ist nicht erforderlich.

**Eine Neuinstallation startet ohne Dienstfreigaben, Suchtreffer, persönliche Domain oder importierte Portlisten.** Du suchst dein eigenes Heimnetz und entscheidest selbst, was veröffentlicht wird. Ein Update einer vorhandenen Installation bewahrt dagegen bewusst deren eigene Einstellungen und Dienste.

## Inhalt

- [Funktionen](#funktionen)
- [Aufbau](#aufbau)
- [Voraussetzungen](#voraussetzungen)
- [Installation](#installation)
- [Die ersten Dienste einrichten](#die-ersten-dienste-einrichten)
- [Webverwaltung](#webverwaltung)
- [Updates und Wiederherstellung](#updates-und-wiederherstellung)
- [Sicherheit und persönliche Daten](#sicherheit-und-persönliche-daten)
- [Bekannte Grenzen](#bekannte-grenzen)
- [Entwicklung und Prüfungen](#entwicklung-und-prüfungen)
- [Projektstruktur](#projektstruktur)

## Funktionen

| Bereich | Enthalten |
| --- | --- |
| Verbindung | Ausgehender WireGuard-Tunnel, Keepalive und native systemd-Dienste |
| Webseiten | Caddy mit öffentlichen HTTPS-Zertifikaten, mehrere Dienste pro Domain über verschiedene Ports |
| Weitere Dienste | Direkte TCP- und UDP-Freigaben; interner und öffentlicher Port dürfen abweichen |
| Erkennung | Gemeinsame Suche für offene TCP-Ports und tatsächliche HTTP-/HTTPS-Antworten, nach lokaler IP gruppiert |
| HTTPS im Heimnetz | Erkennung privater Zertifikate und erforderlicher TLS-Servernamen (SNI), Zertifikatbindung nach Auswahl |
| Verwaltung | Anlegen, Bearbeiten, Aktivieren, Deaktivieren, Löschen und getrennte lokale/öffentliche Tests |
| Oracle | Automatische Werkzeuginstallation, Zielerkennung und Firewall-Verwaltung über OCI CLI/API; die einmalige Kontofreigabe bleibt erforderlich |
| Statistik | HTTP-Anfragen, Fehler und Datenmengen nach Stunde, Tag, Monat und insgesamt; lesbare kB/MB/GB/TB; Reset |
| Diagnose | Healthchecks, begrenzte Ereignisprotokolle, kopierbarer Fehlerbericht und Suchreset |
| Wartung | Backup/Restore, Reparatur, atomarer Binary-Wechsel mit Rollback bei Startfehlern, Deinstallation |
| Neustarts | Optionaler Zeitplan: Oracle zuerst, danach nur der FamilyTunnel-CT; standardmäßig ausgeschaltet |

## Aufbau

```text
Internet / Besucher
        |
        v
Oracle-VM mit öffentlicher IPv4
  Caddy + FamilyTunnel-Server
        |
        | WireGuard (vom Heim-CT ausgehend aufgebaut)
        |
FamilyTunnel-CT im Heimnetz
  FamilyTunnel-Client + Webverwaltung
        |
        +--> Eigene lokale Webseiten und Dienste
```

Der Controller ist in Go geschrieben. Die Weboberfläche ist eingebettet, die Daten liegen in SQLite. Auf den installierten Systemen werden weder Node.js noch ein Go-Compiler für den Betrieb benötigt.

### Welche Adresse gehört wohin?

- **Öffentliche Domain:** dein DNS-Name, beispielsweise `zugang.example.net`. Der A-Eintrag zeigt auf die öffentliche IPv4 der Oracle-VM, nicht auf die Internetadresse deines Heimrouters.
- **Backend / lokales Ziel:** die vom Heim-CT erreichbare private IP mit Protokoll und Port, beispielsweise `http://192.168.50.20:18080`.
- **Öffentlicher Port:** der Port auf Oracle. Ohne Portangabe verwendet eine HTTPS-Adresse standardmäßig 443.
- **Pairing-Code:** verbindet genau diesen Heim-CT mit genau diesem Oracle-Server. Er ist kein SSH-Schlüssel.

Die Adressen und Ports in dieser Dokumentation sind neutrale Beispiele. Sie werden nicht als Freigaben in die Installation übernommen.

## Voraussetzungen

### Oracle-Server

- Dedizierte Ubuntu-24.04-VM, Architektur amd64 oder arm64.
- Öffentliche IPv4, Internetverbindung und administrativer Zugang.
- Zugewiesene OCI-Sicherheitsliste oder Network Security Group.
- Für die automatische OCI-Verwaltung ist eine einmalige Oracle-Kontofreigabe erforderlich. Das Werkzeug installiert der Server-Installer selbst. Wenn Rechte fehlen, gibt er direkt einen fertigen Kontofreigabe-Befehl für Oracle Cloud Shell aus; eine NSG muss nicht manuell gesucht werden.

FamilyTunnel stellt die Nutzung innerhalb des Oracle-Always-Free-Angebots nicht sicher. Verfügbarkeit, Kontingente und mögliche Kosten sind im eigenen Oracle-Konto zu prüfen.

### Heim-CT

- Ubuntu 24.04 mit systemd unter Proxmox, möglichst unprivilegiert.
- Ressourcenziel: 1 CPU-Kern, 1 GB RAM und 8 GB Speicher. Der tatsächliche Bedarf hängt von Diensten, Suche und Last ab.
- Der Proxmox-Kernel muss WireGuard bereitstellen und dessen Nutzung im CT erlauben.
- Der CT erreicht die gewünschten lokalen Zielgeräte.

Der Installer aktiviert kein Nesting und macht einen CT nicht automatisch privilegiert. Eine vorhandene fremde Caddy-Konfiguration wird nicht ungefragt übernommen.

### Technische Basisports

| Auf Oracle | Zweck |
| --- | --- |
| 80/TCP | HTTP und reguläre ACME-Zertifikatsausstellung |
| 443/TCP | HTTPS auf dem Standardport |
| 51820/UDP | WireGuard |
| Jeweils ausgewählte zusätzliche Ports | Erst nach eigener Dienstfreigabe |

Diese Basisports sind technische Programmvorgaben. Es wird keine persönliche Spiele-, Medien- oder Container-Portliste vorinstalliert. Die Verwaltungsports bleiben intern; SSH-Freigaben werden vom Installer nicht geändert.

## Installation

**Reihenfolge: zuerst Oracle, anschließend der Heim-CT.** Die Befehle gehören in die jeweilige SSH-/Container-Konsole, nicht in den Browser.

### 1. Auf der Oracle-VM

Bei Anmeldung als Ubuntu-Benutzer mit sudo:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -4 -fsSL --connect-timeout 10 --max-time 60 https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-server.sh | sudo bash
```

Der Installer erkennt die öffentliche IPv4, aktualisiert Ubuntu, installiert Pakete, prüft WireGuard und richtet Server, Caddy und systemd ein. Die IP-Erkennung verwendet eine HTTPS-Abfrage an api.ipify.org; FT_PUBLIC_IP kann stattdessen ausdrücklich vorgegeben werden. Schlüssel und Zugangsdaten werden neu generiert.

Am Ende erscheint ein Abschlusskasten mit Rolle, Verwaltungsadresse, Benutzer `admin`, Initialpasswort und dem Pairing-Code für den Heim-CT. Den vollständigen Pairing-Code sicher kopieren; er enthält geheime Zugangsdaten.

### 2. Im Ubuntu-CT zu Hause

Als root in der Proxmox-CT-Konsole oder einer passenden SSH-Sitzung:

```bash
apt-get update && apt-get install -y ca-certificates curl && curl -4 -fsSL --connect-timeout 10 --max-time 60 https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-client.sh | bash
```

Den zuvor erzeugten Pairing-Code einfügen. Die Eingabe ist zur Kontrolle sichtbar. Die lokale Verwaltungs-IP wird automatisch erkannt und angezeigt. FT_ADMIN_IP kann bei abweichenden Netzaufbauten ausdrücklich vorgegeben werden.

Der Oracle-SSH-Schlüssel wird dafür nicht in den Heim-CT kopiert. Beide Geräte haben ihren eigenen normalen SSH-Zugang; die Verbindung zwischen den FamilyTunnel-Komponenten erfolgt über WireGuard.

### Gemeinsamer Installer mit Rollenmenü

Wer die Rolle während der Installation auswählen möchte, verwendet stattdessen `install-familytunnel.sh` aus diesem Repository. Auf einer neuen Installation bietet er:

```text
1) Server (Oracle Cloud-Instanz / öffentlicher VPS)
2) Client (Proxmox Ubuntu CT zu Hause)
```

Bei einer bereits vorhandenen Installation wird die gespeicherte Rolle beibehalten. Alle drei Einstiegsskripte verwenden denselben geprüften Installer.

### Einmalige Oracle-Kontofreigabe

Der Server-Installer installiert die OCI CLI bei Bedarf in einem eigenen Verzeichnis und erkennt die zugehörige Sicherheitsliste oder bereits zugewiesene NSG automatisch. Mit vorhandenen Rechten richtet er die Cloud-Regeln selbst ein. Ein zusätzlicher Konfigurationsschritt in der Weboberfläche ist dann nicht erforderlich.

Eine neu erstellte VM hat jedoch nicht automatisch das Recht, ihre Oracle-Cloud-Firewall zu ändern. Bei fehlenden Rechten zeigt der Installer einen fertigen Befehl für einen Kontoadministrator in Oracle Cloud Shell. Dieser erzeugt eine auf genau diese VM beschränkte dynamische Gruppe und begrenzte Netzwerkberechtigungen. Danach den Installer erneut ausführen; Schlüssel und Daten bleiben erhalten. Eine nicht bestätigte Oracle-Anbindung wird nicht als erfolgreiche vollständige Installation ausgegeben.

Die VM kann sich diese Kontorechte nicht selbst erteilen. Kontoadministrator-Anmeldung und Freigabe lassen sich nicht durch den Pairing-Code ersetzen. Es werden keine Administratorrechte für die VM, kein neues Cloud-System und keine SSH-Freigaben erzeugt. Bewusst manuell verwaltete oder fremde VPS können die automatische Prüfung mit FT_SKIP_OCI_AUTOMATION=1 ausdrücklich überspringen; dann sind Cloud-Freigaben nicht bestätigt.

### Verwaltung öffnen

- Die am Ende angezeigte interne Verwaltungsadresse des Heim-CT im Heimnetz aufrufen.
- Mit `admin` und dem individuellen Initialpasswort anmelden und das Passwort ändern.
- Die Oracle-Verwaltung ist standardmäßig nur über WireGuard bzw. einen geeigneten eigenen SSH-Tunnel erreichbar.
- Die interne Verwaltung verwendet ein selbstsigniertes Zertifikat. Den angezeigten Fingerabdruck vor einer etwaigen Browserausnahme prüfen. Öffentliche Webdienste verwenden separat Caddys reguläre HTTPS-Zertifikate.

Nach einer Passwortänderung wird die Datei mit dem Initialpasswort entfernt. Eine erneute Installation setzt das geänderte Passwort nicht zurück.

## Die ersten Dienste einrichten

1. Unter **Dienste** bei Bedarf die eigene öffentliche Domain eintragen.
2. **Heimnetz durchsuchen** starten. Die Schnellsuche prüft allgemeine Web-/Medienports; sie legt keine Freigaben vorab an.
3. Für Spiele oder andere abweichende Ports unter **Suche anpassen** eine lokale IP und **Alle TCP-Ports** wählen. Eine vollständige Netzsuche kann lange dauern und wird nach zwei Stunden begrenzt beendet.
4. Gewünschte Geräte oder einzelne Treffer auswählen und **Auswahl einrichten** anklicken.
5. Die Liste **Aktivierte Dienste** zeigt die gespeicherten Freigaben. Tunnel-, lokale Firewall- und gegebenenfalls OCI-Regeln werden im Hintergrund eingerichtet.
6. Lokale und öffentliche Tests verfolgen. Bei einer geänderten oder zuvor falsch erkannten Webfreigabe kann **Automatisch einrichten** die Erkennung erneuern.

Ein Domainname allein verrät keine lokalen IP-Adressen oder offenen Ports. Der Heim-CT sucht sein eigenes privates /24-Netz. Ein gefundener Port ist noch keine veröffentlichte Freigabe.

HTTPS wird anhand einer tatsächlichen Antwort erkannt, auch wenn ein Dienst dafür einen Domainnamen beim TLS-Aufbau benötigt. Bei privaten Zertifikaten wird das bei der ausdrücklichen Einrichtung erkannte Zertifikat gebunden. Eine neue Zertifikatbindung benötigt eine erneute bewusste Einrichtung.

UDP lässt sich ohne passende Dienstantwort nicht zuverlässig automatisch erkennen. Solche Freigaben können manuell als UDP oder TCP + UDP angelegt werden. Zusätzliche UDP-Freigaben zu Webdiensten benötigen einen öffentlichen Port ab 1024. [Details zur Weiterleitung und Erkennung](docs/tcp-udp.md).

## Webverwaltung

| Bereich | Bedienung |
| --- | --- |
| Übersicht | Verbindung, Betriebszeit und Synchronisierung; automatische Aktualisierung |
| Dienste | Gemeinsame Suche, Auswahl, manuelles Hinzufügen und Bearbeiten, lokale/öffentliche Tests |
| Statistik | Zeitraum wählen, HTTP-Datenmengen vergleichen, Statistik nach Bestätigung zurücksetzen |
| Neustarts | Uhrzeit, Zeitzone und Wiederholung einstellen; echten Neustart-Test ausdrücklich bestätigen |
| Einstellungen | Eigenes Verwaltungspasswort ändern |
| Fehlerhilfe | Ereignisse ansehen, Diagnosebericht kopieren und Ereignisse nach Bestätigung leeren |

**Suchergebnisse zurücksetzen** stoppt die Suche und entfernt gespeicherte Erkennungen sowie dazugehörige inaktive Vorschläge. Aktivierte Dienste bleiben erhalten. Eine neue Suche beginnt ohne diese alten Treffer.

**Statistik zurücksetzen** leert die historischen Werte und den noch offenen Zählerpuffer. Neue und noch laufende Zugriffe werden danach wieder gezählt. Die Anzeige verwendet Datenmengen wie MB oder GB, keine Mbit/s-Geschwindigkeitsmessung. Direkter TCP-/UDP-Verkehr wird dadurch nicht als HTTP-Verkehr erfasst.

### Optionale koordinierte Neustarts

Der Zeitplan ist auf neuen Installationen ausgeschaltet. Frei wählbar sind Uhrzeit, IANA-Zeitzone und täglich feste Ortszeit oder exakt alle 24 Stunden. Bei fester Ortszeit wirken sich Sommer-/Winterzeit auf den Abstand aus; verpasste Termine werden nicht nachgeholt.

Oracle startet zuerst neu. Nach mindestens **einer Minute stabiler Dienste und Tunnelverbindung** wird nur der FamilyTunnel-CT neu gestartet. Der Proxmox-Host und andere Container gehören nicht zum Ablauf. Dauerhafte Aufträge, Bestätigungen und ein begrenztes Wartungsfenster verhindern Neustartschleifen.

**Beide jetzt neu starten (Test)** ist ein echter Neustart mit Verbindungsunterbrechung, kein unverbindlicher Prüflauf. Ein regelmäßiger Neustart ersetzt keine Fehlerbehebung. [Ablauf und Grenzen](docs/maintenance-design.md).

## Updates und Wiederherstellung

### Vollständiges Update

Die jeweiligen Installationsbefehle erneut ausführen: zuerst Oracle, danach Heim-CT. Der Installer sichert eine vorhandene Datenbank unter `/var/backups/familytunnel/` und bewahrt Konfiguration, eigene Dienste, Schlüssel und Passwort.

Das ist auch der passende Weg, wenn neue systemd-Dienste oder Installationshilfen ergänzt wurden. Ein bloßes Binary-Update richtet fehlende neue Systemkomponenten nicht automatisch ein. Anschließend die Webverwaltung mit Strg+F5 neu laden.

Die Downloads im `main`-Branch bilden den aktuellen geprüften Installationsstand. Für eine festgehaltene Version können Einstiegsskript und Paket vom selben konkreten Commit verwendet werden. SHA-256-Dateien prüfen die Paketintegrität; sie sind keine unabhängige digitale Signatur.

### CLI

```text
sudo familytunnel status
sudo familytunnel repair
sudo familytunnel backup
sudo familytunnel restore /pfad/zum/backup.tar.gz
sudo familytunnel update https://example.net/familytunnel-linux.tar.gz
sudo familytunnel uninstall --yes
```

Der Update-Link im Beispiel ist ein Platzhalter. Für ein Binary-Update ist ein tatsächlich veröffentlichtes Linux-Paket samt passender `.sha256`-Datei erforderlich.

- **status:** Dienste und Datenbankzustand anzeigen.
- **repair:** FamilyTunnel, WireGuard und eigene Firewall-Regeln prüfen; ausgefallene zugehörige Dienste gezielt wiederherstellen.
- **backup:** konsistente SQLite-Sicherung, Konfiguration und Schlüssel sichern. Das Archiv ist vertraulich.
- **restore:** Archiv und Datenbank prüfen, vorab sichern und wiederherstellen. Ein wiederhergestellter Neustartplan bleibt ausgeschaltet; alte Neustartaufträge werden verworfen.
- **update:** geprüftes Binary in ein neues Verzeichnis legen und atomar umschalten; bei Startfehlern zurückrollen.
- **uninstall:** vorher sichern; FamilyTunnel entfernen. Sicherungen und das Caddy-Paket bleiben erhalten.

`purge.sh` ist ein gesonderter vollständiger Reset und entfernt auch FamilyTunnel-Sicherungen. Nicht für ein normales Update verwenden.

## Sicherheit und persönliche Daten

- Individuell erzeugte WireGuard-Schlüssel, Pairing-Token und zufällige Initialpasswörter.
- Argon2id, sichere Sitzungscookies, CSRF-Schutz und begrenzte Login-Versuche.
- Interne Verwaltung und Kontrollschnittstellen; Caddys Verwaltungs-API nur auf localhost.
- Private Backend-Ziele; Loopback und Tunneladressen sind keine zulässigen Heimdienste.
- Keine vorkonfigurierten Nutzer-Domains, Container-Adressen, Dienstfreigaben, Cloud-IDs oder Neustartzustimmungen.
- Die Demo-Oberfläche liegt ausschließlich unter `tests/`. Sie und die isolierten Testdateien werden nicht in das Installationspaket kopiert.
- Keine Datenbanken, privaten Schlüssel, Pairing-Dateien, Logs oder lokalen Kontokonfigurationen im Paket. Eine Dateifreigabeliste und ein automatischer Paketcheck sichern diese Trennung ab.
- Updates einer bestehenden Installation bewahren die Daten dieser Installation; sie werden weder aus GitHub übernommen noch nach GitHub hochgeladen.

Den Pairing-Code, Schlüssel, Passwortdateien und Backups nicht öffentlich teilen. Die sechs Zeichen Mindestlänge für selbst gewählte Passwörter sind eine bestehende Produkteinstellung; für die Verwaltung ist ein langes eigenes Passwort sinnvoll. [Sicherheitsinformationen](SECURITY.md).

## Bekannte Grenzen

- Ein Oracle-Server und ein Heim-CT; mehrere Clients/Peers sind nicht implementiert.
- WebSocket-Upgrades werden in dieser Version nicht weitergeleitet.
- Öffentliche Tests starten vom Heim-CT über Oracle. Sie sind kein Nachweis aus jedem fremden Netz und keine Prüfung auf eine Google-Safe-Browsing-Einstufung.
- UDP bleibt ohne dienstspezifische Antwort unbestätigt.
- Die Statistik zählt HTTP-Nutzdaten; sie ist kein vollständiger Netzwerkabrechnungszähler.
- DNS/DDNS wird geprüft. Automatische Änderungen beim DNS-Anbieter benötigen eine zusätzliche Anbieteranbindung und Zugangsdaten.
- Automatische OCI-Regeln setzen die eingerichtete CLI und passende IAM-Rechte voraus. Mehrdeutige Cloud-Ziele werden nicht beliebig ausgewählt.
- Ein eigener systemd-Sandbox-Test prüft den nötigen Netlink-Zugriff des Wartungsdienstes, ohne Maschinen neu zu starten. Die echten Installations-, Neustart- und Wiederherstellungsabläufe müssen zusätzlich auf den jeweiligen Ubuntu-/Proxmox-Systemen geprüft werden. Automatisierte Neustarttests verwenden simulierte Systemstarts.

## Entwicklung und Prüfungen

Quellcode-Prüfungen laufen bei Push und Pull Request in [GitHub Actions](https://github.com/jonascool19-pixel/FamilyTunnel/actions/workflows/ci.yml). Der Ablauf prüft Go-Tests, Datenrennen, tatsächliche Oberflächenfunktionen, Shell-/Python-Syntax, OCI-Regelabgleich und echte TCP-/UDP-Übertragung durch WireGuard in isolierten Linux-Netzwerkräumen. Anschließend entstehen Linux-Binaries für amd64/arm64 sowie Pakete und SHA-256-Dateien.

Zusätzliche Prüfungen stellen sicher, dass eine frische Datenbank keine Dienste enthält und der Installer nur freigegebene Laufzeitdateien mitnimmt. GitHub Actions stellt geprüfte Build-Artefakte bereit; ein automatisches GitHub Release ist nicht eingerichtet.

```text
go test ./...
go vet ./...
node tests/ui/callbacks.cjs
python3 tests/oci-rules.py
```

Die Produktions-Builds erfolgen über GitHub Actions. Hinweise für Änderungen und lokale Prüfungen: [CONTRIBUTING.md](CONTRIBUTING.md). Ausführliche Betriebsszenarien: [TESTING.md](TESTING.md).

## Projektstruktur

```text
cmd/familytunnel/    Programmstart
internal/app/       Controller, API, SQLite und automatisierte Go-Tests
internal/app/web/   Eingebettete Oberfläche
scripts/            Laufzeithilfen und Buildskripte
systemd/            Dienste und Timer
tests/             Isolierte Integrationstests und ausdrücklich gestartete UI-Vorschau
docs/              Vertiefende Betriebsdokumentation
dist/              Aktuelles geprüftes Installations- und Updatepaket
.github/workflows/  Automatisierte Prüfungen und Linux-Builds
```

Technische Grundlagen: [WireGuard](https://www.wireguard.com/), [Caddy](https://caddyserver.com/docs/), [Oracle-Netzwerkregeln](https://docs.oracle.com/en-us/iaas/Content/Network/Concepts/securityrules.htm) und [Proxmox-Container](https://pve.proxmox.com/pve-docs/pct.1.html).