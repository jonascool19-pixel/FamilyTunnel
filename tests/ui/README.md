# Isolierte Oberfläche für Entwicklung

```text
node tests/ui/callbacks.cjs
node tests/ui/preview.cjs
```

Der erste Befehl prüft die tatsächlichen Oberflächenfunktionen mit simulierten Antworten, ohne Browser und ohne Heimnetzsuche.

Der zweite startet eine ausdrücklich aufgerufene Vorschau auf `http://127.0.0.1:18767/`. Alle dort dargestellten Dienste, Adressen, Statistiken und Neustartmeldungen sind synthetisch. Es werden keine echten Freigaben angelegt und keine Maschinen neu gestartet. Mit Strg+C beenden.

Diese Dateien werden nicht in das Installationspaket übernommen und nicht beim normalen FamilyTunnel-Start geladen. Eine echte Neuinstallation startet ohne diese Beispieldaten.
