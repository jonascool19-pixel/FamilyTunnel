# FamilyTunnel

Macht deine Heimdienste über eine Oracle-Cloud-VM erreichbar: **Internet → Oracle → WireGuard → Heimnetz**. Der Ubuntu-Container zu Hause baut den Tunnel selbst auf. Keine Portfreigabe am Heimrouter und kein Docker nötig.

## Das brauchst du

- **Oracle:** eigene Ubuntu-24.04-VM mit öffentlicher IPv4, amd64 oder arm64.
- **Zu Hause:** Ubuntu-24.04-CT unter Proxmox, mit systemd und WireGuard-Unterstützung. Ziel: 1 CPU, 1 GB RAM, 8 GB Speicher.
- **Für Webseiten:** eine Domain, deren DNS-A-Eintrag auf die Oracle-IP zeigt. Direkte TCP-/UDP-Dienste funktionieren auch ohne Domain.

## 1. Oracle installieren

In der SSH-Konsole der Oracle-VM als Benutzer mit sudo ausführen:

```bash
sudo apt-get update && sudo apt-get install -y ca-certificates curl && curl -4 -fsSL --connect-timeout 10 --max-time 60 https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-server.sh | sudo bash
```

Der Installer aktualisiert Ubuntu und richtet FamilyTunnel, WireGuard, Caddy und die Firewall ein. **Den am Ende angezeigten Pairing-Code sicher kopieren.** Er verbindet später den Heim-CT mit Oracle und enthält geheime Zugangsdaten.

**Oracle-Berechtigung:** Das Oracle-Werkzeug und die Netzwerkregeln werden automatisch eingerichtet. Fehlen Kontorechte, zeigt der Installer einen fertigen Befehl für Oracle Cloud Shell. Ein Kontoadministrator führt ihn einmal aus; danach den Installer erneut starten. Eine NSG musst du nicht selbst suchen.

## 2. Heim-CT installieren

In der Konsole deines Ubuntu-CT **als root** ausführen:

```bash
apt-get update && apt-get install -y ca-certificates curl && curl -4 -fsSL --connect-timeout 10 --max-time 60 https://raw.githubusercontent.com/jonascool19-pixel/FamilyTunnel/main/install-client.sh | bash
```

Den Pairing-Code von Oracle einfügen. Am Ende stehen **Webadresse, Benutzer `admin` und dein zufälliges Initialpasswort** im Abschlusskasten. Die Webadresse im Heimnetz öffnen und das Passwort ändern. Die interne Verwaltung verwendet ein eigenes Zertifikat; vor einer Browserausnahme dessen angezeigten Fingerabdruck prüfen.

Optional gibt es den [gemeinsamen Installer](install-familytunnel.sh) mit **1 = Server / 2 = Client**. Bestehende Installationen behalten ihre Rolle.

## 3. Dienste freigeben

1. Unter **Dienste** für Webseiten deine Domain eintragen.
2. **Heimnetz durchsuchen** anklicken. Für Spiele oder andere Ports unter **Suche anpassen** eine Geräte-IP und **Alle TCP-Ports** wählen.
3. Gewünschte Geräte oder Ports auswählen und **Auswahl einrichten** anklicken.
4. Unter **Aktivierte Dienste** die lokalen und öffentlichen Tests prüfen. Dort kannst du Dienste bearbeiten, deaktivieren oder löschen.

**Lokales Ziel / Backend** bedeutet IP und Port deines Heimdienstes. **Öffentlicher Port** bedeutet den Zugang auf Oracle. Webdienste erhalten HTTPS; zusätzliche Dienste können dieselbe Domain mit anderen Ports verwenden.

Neue Suchen ersetzen alte Treffer der durchsuchten Geräte. TCP-Ziele werden vor dem Einrichten erneut geprüft. Ist eines nicht erreichbar, wird die gesamte Auswahl ohne neue Freigaben abgelehnt. UDP lässt sich ohne passende Dienstantwort nicht zuverlässig als erreichbar bestätigen.

## Aktualisieren

**Die beiden Installationsbefehle erneut ausführen: zuerst Oracle, danach Heim-CT.** Sie laden immer das aktuelle veröffentlichte, geprüfte Paket aus `main`. Ubuntu wird mit aktualisiert. Eigene Dienste, Schlüssel und Passwort bleiben erhalten; die vorhandene Datenbank wird vorher gesichert. Anschließend die Webseite mit **Strg+F5** neu laden.

## Was enthalten ist

- Gemeinsame Dienstsuche, HTTPS sowie direkte TCP-/UDP-Freigaben.
- Webverwaltung mit Anmeldung, Status, Diensttests und Fehlerbericht.
- HTTP-Statistik mit lesbaren Datenmengen und Reset; Such- und Ereignisreset.
- Backup/Restore, Reparatur, Updates mit Rollback und Deinstallation.
- Optionaler Neustartplan: Oracle zuerst, danach der FamilyTunnel-CT nach einer Minute stabiler Verbindung. Auf Neuinstallationen ausgeschaltet. Der Testknopf startet beide tatsächlich neu; der Proxmox-Host bleibt außen vor.

## Gut zu wissen

Eine Neuinstallation enthält **keine persönlichen Domains, Dienstfreigaben oder Suchergebnisse**. Die Verwaltung bleibt standardmäßig im Heimnetz/WireGuard. Öffentlich werden 80/TCP, 443/TCP, 51820/UDP und deine ausgewählten Dienstports benötigt; SSH wird nicht zusätzlich geöffnet.

Unterstützt werden ein Oracle-Server und ein Heim-CT. WebSockets und automatische DNS-Anbieter-Updates sind derzeit nicht enthalten. Die Statistik zählt HTTP-Nutzdaten, keinen vollständigen TCP-/UDP-Verkehr. Eine erfolgreiche Verbindung entfernt keine Browserwarnung zu Phishing/Malware. Oracle-Kontingente und mögliche Kosten bitte im eigenen Konto prüfen.

## Weitere Hilfe

[Ausführliche Anleitung und Befehle](docs/manual.md) · [TCP/UDP und Suche](docs/tcp-udp.md) · [Neustarts](docs/maintenance-design.md) · [Sicherheit](SECURITY.md) · [Tests](TESTING.md) · [Entwicklung](CONTRIBUTING.md) · [GitHub-Prüfungen](https://github.com/jonascool19-pixel/FamilyTunnel/actions/workflows/ci.yml)
