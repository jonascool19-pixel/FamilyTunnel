# Koordinierte Neustarts

Die Verwaltung im Heim-CT bietet einen optionalen Zeitplan und einen echten Neustart-Test. Nach Neuinstallation ist der Zeitplan ausgeschaltet. Beide Seiten müssen mit dem vollständigen Installer aktualisiert werden, damit der separate systemd-Wartungsdienst vorhanden ist.

## Einstellungen

- Uhrzeit und IANA-Zeitzone sind frei einstellbar; Vorgabe 07:00 Europe/Berlin, zunächst deaktiviert.
- Täglich: feste Ortszeit. Sommer-/Winterzeit kann einen Abstand von 23 oder 25 Stunden ergeben. Ein doppelt vorkommender Tageszeitpunkt läuft höchstens einmal; eine nicht vorkommende Uhrzeit wird übersprungen.
- Alle 24 Stunden: erster Termin zur nächsten gewählten Ortszeit, anschließend exakt 24 Stunden Abstand in UTC.
- Verpasste Termine werden übersprungen. Es gibt kein Nachholen direkt nach dem Booten.
- Der Testknopf fordert nach ausdrücklicher Browserbestätigung echte Neustarts beider Geräte an; er ist kein Probelauf ohne Unterbrechung.

## Ablauf und Schutz gegen Schleifen

1. Der Heim-CT speichert die Zustimmung dauerhaft. Sein Wartungsdienst sendet authentifizierte Heartbeats über WireGuard an Oracle.
2. Der separate Oracle-Wartungsdienst verlangt eine frische, passende Zustimmung und aktive Dienste. Er reserviert einen Auftrag in SQLite, bevor er Oracle neu startet.
3. Ein tatsächlicher neuer Systemstart muss nachgewiesen sein. Dafür wird die Kernel-Boot-ID mit der Startzeit von PID 1 verbunden; somit wird auch ein LXC-Neustart erkannt, während ein bloßer Controller-Neustart nicht zählt.
4. Caddy, Controller, WireGuard und CT-Heartbeat müssen nach dem Oracle-Neustart bei wiederholten Prüfungen mindestens eine Minute stabil bleiben. Es gibt davor keine zusätzliche Wartefrist. Oracle muss bei der CT-Freigabe mindestens eine Minute laufen. Ein fehlgeschlagener Check oder eine Prüflücke setzt diese Stabilitätsfrist zurück.
5. Erst dann bestätigt der CT den Auftrag dauerhaft und startet ausschließlich sich selbst neu. Der Proxmox-Host und andere Container gehören nicht zum Ablauf.
6. Jede Neustartreservierung ist einmalig. Fehler, fehlende Zustimmung oder ein nach 20 Minuten abgelaufenes Wartungsfenster führen zu keinem weiteren Neustart. Bei einem Verbindungsabbruch zwischen dauerhafter CT-Reservierung und Serverbestätigung wird vorsichtshalber nicht erneut versucht.
7. Nach einem neuen CT-Systemstart bestätigt ein Heartbeat den Abschluss. Der Verlauf und aktuelle Zustand sind in der Oberfläche sichtbar.
8. Backup/Restore deaktiviert den wiederhergestellten Zeitplan und verwirft alte Neustartaufträge; der Betreiber muss ihn anschließend erneut einstellen.

Der root-Wartungsdienst läuft über einen eigenen systemd-Timer alle 30 Sekunden, unabhängig vom Browser. Der HTTP-Controller erhält keine zusätzliche Reboot-Capability. Öffentliche Verwaltungs- oder beliebige Shell-Befehls-Endpunkte werden nicht eingerichtet. Installieren allein startet keine Maschine neu. Ein täglicher Neustart ist optional und ersetzt keine Fehlerbehebung; Linux verwaltet seinen Arbeitsspeicher-Cache selbst.

## Prüfung und Grenzen

Automatisierte Tests simulieren beide Systemstarts und prüfen Reihenfolge, tatsächliche Boot-ID-Änderungen, Stabilitätsfristen, fehlende Zustimmung, Duplikate, Ausfälle, Ablauf und Zeitumstellung. Dabei wird kein echtes System neu gestartet. Ein realer Oracle-/LXC-Neustart ist ein separater Betriebstest über den ausdrücklich beschrifteten Knopf. Ein unprivilegierter CT muss normal mit systemd booten und seinen eigenen Neustart über `systemctl reboot` unterstützen. Nesting und Zugang zum Proxmox-Host sind dafür nicht vorgesehen.

Referenzen: [systemd.timer](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html), [systemctl](https://www.freedesktop.org/software/systemd/man/latest/systemctl.html), [Proxmox Container Toolkit](https://pve.proxmox.com/pve-docs/pct.1.html).

Der Wartungsdienst benötigt AF_NETLINK, um den WireGuard-Status tatsächlich lesen zu können. Fehlt diese systemd-Freigabe, bleibt die Bereitschaft ausdrücklich falsch. Der aktuelle Dienst erlaubt diesen Zugriff; die Oberfläche nennt die Ursache einer blockierten Prüfung. Der CI-Test tests/maintenance-sandbox.sh prüft die alte blockierende und die korrigierte Sandboxing-Konfiguration ohne Neustart.
