# Geprüftes Konzept: Oracle zuerst, danach FamilyTunnel-CT

Diese Funktion ist ein Konzept und in dieser Version weder als Neustart-Endpunkt noch als aktiver Zeitplan implementiert. Es gibt keinen automatischen täglichen Systemneustart. Die Prüfung und Planung verändert keine Server.

Eine spätere optionale Umsetzung kann täglich um 07:00 Europe/Berlin beginnen. Wegen Sommer-/Winterzeit ist das nicht immer genau 24 Stunden Abstand. Als Standard ist gezielte Diensterholung sinnvoller als ein täglicher Komplettneustart.

Erforderlicher Ablauf:

1. Explizite Aktivierung auf beiden Seiten. Proxmox-Host und andere Container bleiben außerhalb des Ablaufs.
2. Lokaler systemd-Timer auf Oracle speichert einen einmaligen Wartungsauftrag in SQLite und startet erst dann die VM neu.
3. Der CT ruft den Auftrag authentifiziert über WireGuard ab. Es gibt keine öffentliche Schnittstelle für beliebige Neustartbefehle.
4. Oracle muss tatsächlich neu gebootet haben. Caddy, WireGuard und Konfigurationsübertragung müssen danach mindestens zwei Minuten stabil sein.
5. Erst dann startet nur der FamilyTunnel-CT neu. Er bestätigt den Auftrag vorher dauerhaft in SQLite. Höchstens ein Durchlauf pro Tag und Gerät.
6. Kommt Oracle nicht rechtzeitig zurück oder scheitert ein Healthcheck, bleibt der CT an. Keine unbegrenzten Wiederholungen oder Neustartschleifen.
7. Kein Nachholen verpasster Neustarts direkt beim Booten (`Persistent=false`). Nicht jede neue Oracle-Boot-ID löst einen CT-Neustart aus.
8. Abnahmetests für Sommerzeit, verpasste Uhrzeit, Oracle-/CT-Ausfall, Tunnel-Unterbrechung, doppelte Aufträge und Neustart zwischen Bestätigung und Reboot.

Vor Einrichtung müssen Uhrzeit, Wartungsfenster und Ausfallfolgen bestätigt werden. Tatsächliche Server-/Dienstneustarts benötigen die ausdrückliche Freigabe des Betreibers.

Referenzen: [systemd.timer](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html), [Proxmox Container Toolkit](https://github.com/proxmox/pve-docs/blob/master/pct.adoc).
