# Änderungen und Prüfungen

## Ausgangspunkt

Änderungen beginnen im aktuellen `main`-Stand. Nach einer ausdrücklich angekündigten Historienbereinigung sollte das Repository neu geklont werden. Alte Branches nicht unverändert zurückmergen: Sie können entfernte persönliche Beispiele oder frühere Pakete wieder einführen.

## Lokale Prüfungen

Benötigt werden Go 1.27.3, Node.js für die Oberflächentests und Python 3 für die OCI-/Paketprüfungen. Go und Node.js werden auf den später installierten Systemen nicht benötigt; Python 3 ist dort für Installation und OCI-Hilfen erforderlich.

```text
go test ./...
go vet ./...
node tests/ui/callbacks.cjs
python3 tests/oci-rules.py
```

Die vollständigen Linux-Prüfungen und Produktions-Builds laufen in GitHub Actions. Der WireGuard-Integrationstest benötigt isolierte Linux-Netzwerkräume und entsprechende Rechte. Er ist kein Testskript für einen laufenden Produktivserver.

Die ausdrücklich gestartete, lokale Vorschau ist unter `tests/ui/` dokumentiert. Sie verwendet synthetische Daten und gehört nicht in den Installer.

## Leerer Erststart

- Keine persönlichen Domains, realen öffentlichen IPs, Cloud-Konto-IDs, Schlüssel oder kopierten Portlisten einchecken.
- Beispiele verwenden `example.net` und neutrale private Testnetze; öffentliche Test-IP-Adressen verwenden Dokumentationsnetze.
- Testdaten nur in isolierten Testdateien bzw. temporären Datenbanken anlegen. Die Produktionsinitialisierung darf keine Dienste, Suchtreffer oder Neustartaufträge erzeugen.
- Die Schnellsuche enthält allgemeine Prüfziele. Diese sind keine Dienstfreigaben. Persönliche Spiele-/Containerkataloge gehören nicht in die Vorgaben.
- Neue Laufzeitdateien ausdrücklich in die Paketfreigabeliste aufnehmen und den Paketcheck anpassen. Nicht ganze Arbeitsverzeichnisse in Release-Archive kopieren.

## Betrieb und Kompatibilität

SQLite-Änderungen müssen bestehende Installationen erhalten und wiederholt ausführbar sein. Vorhandene eigene Dienste dürfen bei Updates nicht durch Beispiele ersetzt oder ungefragt gelöscht werden. Neue systemd-Einheiten benötigen den vollständigen Installer, nicht nur ein Binary-Update.

Neustarttests verwenden simulierte Uhren, Boot-Kennungen und Neustartfunktionen. Keine echten Maschinen aus CI oder automatisierten Go-Tests neu starten. Produktive Änderungen und echte Neustarts benötigen die ausdrückliche Freigabe des Betreibers.

## Build-Ergebnis

Der bestehende Ablauf erzeugt amd64-/arm64-Binaries, ein Linux-Updatearchiv, ein vollständiges Installationsarchiv und SHA-256-Dateien. Geprüfte Ergebnisse stehen als Actions-Artefakt bereit. Ein automatisches GitHub Release ist nicht eingerichtet. Änderungen an Plattformen, Versionierung oder Veröffentlichung müssen mit dem Projektbetreiber abgestimmt werden.

Produktions-Builds verwenden ausschließlich `scripts/build.sh` in GitHub Actions. Im Repository werden nur die zwei fertigen Archive samt Prüfsummen veröffentlicht; einzelne Binärdateien bleiben im Actions-Artefakt und in den Archiven.
