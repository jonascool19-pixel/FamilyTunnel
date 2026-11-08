# Sicherheitsinformationen

## Vertrauliche Daten

Nicht in öffentliche Issues, Pull Requests, Bildschirmfotos oder Git-Commits aufnehmen:

- WireGuard-Privatschlüssel und private SSH-Schlüssel;
- Pairing-Code bzw. `pairing.json` und Pairing-Token;
- Passwörter, Sitzungs-Cookies und CSRF-Token;
- OCI-Konfiguration mit privaten Schlüsseln;
- Datenbanken, Diagnoseauszüge mit vertraulichen Angaben und Backup-Archive.

Ein Backup enthält Zugangsdaten. Es muss geschützt gespeichert und übertragen werden. Gitignore-Regeln sind nur ein Schutz vor versehentlichem Hinzufügen; sie ersetzen keine Prüfung bereits versionierter Dateien.

## Sicherheitsproblem melden

Sicherheitslücken zunächst vertraulich an den Repositorybetreiber melden. Falls auf GitHub die private Sicherheitsmeldung angeboten wird, diese verwenden. Keine verwertbaren Zugangsdaten oder ungefilterten Produktiv-Backups öffentlich veröffentlichen.

Bei einem bereits veröffentlichten Schlüssel oder Token muss dieser auf dem betroffenen System ersetzt werden. Eine Git-Historienbereinigung macht zuvor kopierte Zugangsdaten nicht ungültig.

## Technische Grenzen

Die interne Verwaltung ist auf private bzw. WireGuard-Adressen beschränkt. Eine zusätzliche Veröffentlichung dieser Verwaltung im Internet ist nicht Teil der Standardeinrichtung. Caddys Verwaltungs-API bleibt auf localhost.

Die Erkennung eines privaten HTTPS-Dienstes ist eine bewusste erste Vertrauensentscheidung im eigenen LAN. Danach wird das gewählte Zertifikat gebunden. Eine geänderte Bindung erfordert eine ausdrückliche erneute Einrichtung. Öffentliche TLS-Zertifikate bleiben davon getrennt.

Öffentliche Erreichbarkeitstests bestätigen technische Verbindung und gegebenenfalls TLS/Tunnel-Nachweis. Sie sind kein Malware-Scan und entfernen keine Sicherheitsbewertung durch Browseranbieter.

## Bereinigte Historie

Eine bestätigte Historienbereinigung entfernt betroffene Inhalte aus den veröffentlichten Branches. Bereits vorhandene Klone, Forks und zwischengespeicherte GitHub-Ansichten können ältere Inhalte behalten. Alte Branches nicht wieder in die bereinigte Historie mergen. [GitHub beschreibt die verbleibenden Kopien und Cache-Grenzen](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository).

## Prüfung vom 8. November 2026

Der Code wurde auf offensichtliche Fehler bei Anmeldung, interner API, privaten Zieladressen, Archivverarbeitung und Updates geprüft. Go-Abhängigkeiten und Build-Werkzeuge wurden aktualisiert. `govulncheck` mit Go 1.27.3 meldet keine bekannten Schwachstellen in den tatsächlich verwendeten Paketen. Eine Modulwarnung betrifft das ungenutzte OpenPGP-Paket in `golang.org/x/crypto`; FamilyTunnel importiert ausschließlich Argon2 aus diesem Modul. Ein solcher Check ersetzt kein vollständiges externes Sicherheitsaudit.

Archive werden vor dem Schreiben vollständig geprüft: keine symbolischen oder harten Links, Geräte, absoluten Pfade, Pfadwechsel, doppelten Einträge oder privilegierten Dateimodi. Der Update-Rollback greift auch bei einem sofort scheiternden Dienststart. Temporäre Backup-Verzeichnisse werden nach dem Ende der Funktion zuverlässig entfernt. Entsprechende Negativ- und Rollbacktests laufen in GitHub Actions ohne Produktivsysteme zu verändern.
