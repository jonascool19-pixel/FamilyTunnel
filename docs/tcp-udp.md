# Container, Spieleports und vollständige TCP-Suche

## Einfache Freigaben

Die Hauptansicht „Geräte suchen und freigeben“ zeigt jede lokale IP einmal mit ausklappbarer Portliste. Die Auswahl neben der IP wählt alle geeigneten Ports dieses Geräts; einzelne Ports sind getrennt wählbar. „Auswahl freigeben“ speichert die Auswahl sofort und überträgt sie im Hintergrund. Die Auswahl bleibt während der regelmäßigen Aktualisierung erhalten. Doppelte Suchtreffer werden zusammengefasst. Manuelle Eingabe, Import und Oracle-Zielwahl stehen unter „Erweitert“.

Beim gemeinsamen Freigeben bleibt eine vorhandene aktive Zuordnung erhalten. Für ein zweites Gerät mit derselben gewünschten öffentlichen Portnummer wird die nächste freie Nummer gewählt und sichtbar als „öffentlich …“ angezeigt. Bereits vorhandene passende HTTPS-Webseiten werden erhalten. Verwaltungsports werden nicht automatisch veröffentlicht. Maximal 200 Ports können gemeinsam übernommen werden.

Der Einrichtungsstatus unterscheidet laufende Übertragung, Fehler, fehlende Oracle-Verbindung und eingerichtete Regeln. „Eingerichtet“ bestätigt keinen erfolgreichen Spielbeitritt oder externen Verbindungstest. Fehlende Cloud-Rechte werden nicht als Erfolg angezeigt. Die lokale Weiterleitung bleibt bei einem Cloud-Fehler gespeichert und die Synchronisierung wird erneut versucht.

Unter „Container und Spieleports“ genügen Name, lokale Container-IP, öffentliche Ports und TCP/UDP. Der interne Port darf abweichen; leer bedeutet dieselbe Nummer. Kommagetrennte Ports und „TCP und UDP“ erzeugen alle Zuordnungen gemeinsam. Neue Einträge sind standardmäßig inaktiv. JSON-Importe werden immer als inaktive Vorschläge gespeichert. Auswahl und „Ausgewählte aktivieren“ veröffentlichen nur die gewählten Einträge. Bearbeiten, Deaktivieren und Löschen funktionieren getrennt je Protokoll und öffentlichem Port.

Eine öffentliche IPv4 bietet pro Protokoll und Port genau ein Ziel. TCP/7777 und UDP/7777 können unterschiedliche Ziele haben, TCP/7777 kann aber nicht gleichzeitig zwei Container bedienen. Direkte TCP-Freigaben dürfen keinen aktiven HTTPS-Port belegen. Eine HTTPS-Freigabe auf 8096 muss deshalb erst deaktiviert oder auf einen anderen öffentlichen Port gelegt werden, bevor 8096 als direktes TCP verwendet wird. Die Hauptadresse auf HTTPS/443 bleibt erhalten.

Direkte Freigaben ändern die Anwendungsdaten nicht: Spielclients verwenden Domain oder VPS-IP mit Port. Ein HTTP-Backend bleibt HTTP, ein TLS-Backend bleibt TLS; direktes TCP fügt kein HTTPS hinzu. Aufgrund des gezielten SNAT sieht der Heimdienst die LAN-IP des FamilyTunnel-CT statt der ursprünglichen Internet-Client-IP. Dienste mit IP-Sperren oder öffentlich angekündigten Serveradressen müssen entsprechend konfiguriert werden. Spezielle Spielprotokolle, die weitere dynamische Ports aushandeln, benötigen passende zusätzliche Freigaben.

Die Tunnel-MTU bleibt zu beachten. Bei WireGuard-MTU 1420 passen 1392 Bytes IPv4-UDP-Nutzdaten ohne Fragmentierung. Größere Datagramme benötigen erlaubte IP-Fragmentierung oder eine passende MTU-Behandlung im Dienst. Das Programm entfernt ein gesetztes Don't-Fragment-Bit nicht stillschweigend. Der Integrationstest prüft sowohl korrekte ICMP-PMTU-Rückmeldung als auch große fragmentierbare UDP-Datagramme in beiden Richtungen.

## Linux und LXC

IP-Weiterleitung und Netfilter-NAT müssen in beiden Netzwerk-Namensräumen möglich sein. Bei aktiven Freigaben setzt FamilyTunnel `net.ipv4.ip_forward=1` und speichert den vorherigen Wert. Cleanup entfernt nur eigene Chains/Verbindungen und stellt den vorherigen Wert wieder her. Andere Firewallregeln werden nicht geleert. Die WireGuard-AllowedIPs bleiben auf die beiden Tunneladressen begrenzt. Änderungen entwerten nur zugehörige Conntrack-Einträge.

Bei unprivilegiertem LXC kann eine Proxmox-seitige Freigabe erforderlich sein. FamilyTunnel erzwingt weder Privilegierung noch Nesting. Ist IP-Weiterleitung/NAT nicht möglich, wird die Serverkonfiguration zurückgerollt und der Fehler angezeigt. Ein realer Abnahmetest auf dem jeweiligen Proxmox-Host bleibt erforderlich.

## Erweiterte lokale Suche

Die Suche arbeitet asynchron mit Fortschritt und Abbrechen. Ergebnisse landen in SQLite und überstehen Browser-Neuladen. Schnell prüft übliche Dienstports. Vollständig prüft TCP 1–65535 auf den eingetragenen Geräte-IPs oder bei leerem Feld auf bis zu 253 Geräten des gleichen privaten /24-Heimnetzes wie der CT. Maximal 64 parallele Verbindungsprüfungen, 250 ms pro Versuch und zwei Stunden Gesamtlaufzeit begrenzen die Last. Eine vollständige Netzsuche kann unvollständig enden; Abschluss und Abbruch/Zeitlimit werden unterschieden.

Es werden maximal 4096 Treffer und 200 Portvorschläge gespeichert. Veröffentlichbare TCP-Treffer werden automatisch als **inaktive** Vorschläge aufgenommen. Bei Portkollisionen bleiben die Funde sichtbar; die gemeinsame Freigabe wählt eine freie öffentliche Nummer. Verwaltungsports werden nicht automatisch veröffentlicht. Ein TCP-Handshake beweist keinen bestimmten Diensttyp. UDP wird ohne dienstspezifische Antwort nicht zuverlässig als offen erkannt und ist manuell oder über bekannte JSON-Freigaben hinzuzufügen.

## Optionale Oracle-Automation

Bei vorhandener CLI und IAM-Anmeldung erkennt der Server das Ziel selbst über IMDSv2, VNIC und Subnetz: genau eine zugewiesene NSG, sonst genau eine Sicherheitsliste. Bei mehreren Netzwerkkarten/NSGs/Listen wird keine beliebige ausgewählt; dann muss das Ziel einmalig unter „Erweitert“ hinterlegt werden. Eine explizit deaktivierte Anbindung wird respektiert. Die Anmeldung und nötigen Oracle-Kontorechte müssen einmal eingerichtet sein; die Software kann sich diese Rechte nicht selbst erteilen. Danach ist keine Cloud-Einrichtung je Port erforderlich. Metadaten werden ohne Proxy abgefragt. Grundlage: [Oracle IMDSv2](https://docs.oracle.com/en-us/iaas/Content/Compute/Tasks/gettingmetadata.htm).

Voraussetzung sind eine separat installierte OCI CLI und passende IAM-Rechte für genau die gewählte Sicherheitsliste oder zugewiesene NSG. Die GUI speichert nur Zieltyp, OCID und Aktivierung; keine privaten API-Schlüssel. Verwendet wird ein Instanz-Prinzipal oder eine geschützte OCI-CLI-Konfiguration samt Schlüssel unter `/var/lib/familytunnel/oci/`. Schlüsselpfade müssen in dieses Verzeichnis zeigen, da der Dienst nicht auf Home-Verzeichnisse zugreifen darf.

Nur Regeln mit Präfix `FamilyTunnel managed ` werden von der Automation entfernt oder ersetzt. Andere Regeln einschließlich SSH bleiben erhalten. Sicherheitslisten werden unter ETag-Vergleich aktualisiert, um parallele Änderungen nicht zu überschreiben. NSG-Ergänzungen werden in Paketen von höchstens 25 Regeln gesendet. Manuell angelegte Freigaben bleiben beim Deaktivieren eines Dienstes bestehen. NSGs müssen der Instanz bereits zugeordnet sein.

Ohne OCI-Anbindung wird nur die lokale Firewall übernommen. Cloud-Freigaben bleiben dann unbestätigt und benötigen die einmalige Anbindung oder manuelle Regeln. Ein gespeicherter Aktivierungswunsch allein beweist keine öffentliche Erreichbarkeit. IAM/CLI-Fehler erscheinen im Einrichtungsstatus; lokale Zuordnungen bleiben gespeichert. Zugangsdaten und IAM-Berechtigungen werden nicht automatisch erstellt.

## Tests und Aktualisierung

Zuerst Oracle-Server, danach CT aktualisieren. Vorhandene Webseiten werden nicht automatisch in direkte TCP-Freigaben umgewandelt. Importierte Vorschläge erst prüfen, dann gezielt aktivieren. Diese Änderung wurde nicht auf den produktiven Geräten installiert; keine produktiven Ports oder Server wurden während der Entwicklung verändert.

GitHub Actions führt Go-, GUI- und OCI-Regeltests aus. Der Integrationstest in `tests/raw-tunnel.sh` verwendet vier wegwerfbare Linux-Netzwerk-Namensräume, echte WireGuard-Schlüssel und TCP-/UDP-Echo-Dienste. Er prüft zwei NAT-Stufen, Datenintegrität, unterschiedliche interne/externe Ports, Deaktivieren und Cleanup. Er testet keine individuellen Spielversionen, keine echte OCI-IAM-Anbindung und keinen echten unprivilegierten Proxmox-CT.

Referenzen: [WireGuard](https://www.wireguard.com/papers/wireguard.pdf), [OCI-Sicherheitslisten](https://docs.oracle.com/en-us/iaas/tools/oci-cli/latest/oci_cli_docs/cmdref/network/security-list/update.html), [OCI-NSG-Regeln](https://docs.oracle.com/en-us/iaas/tools/oci-cli/latest/oci_cli_docs/cmdref/network/nsg/rules/add.html).
