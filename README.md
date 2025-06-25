# Grundstückverkehrsgesetz Website Monitor

Ein Go-Programm zur Überwachung der Website [http://www.grundstueckverkehrsgesetz.nrw.de](http://www.grundstueckverkehrsgesetz.nrw.de) auf neue Links. Größtenteils Auto-Generiert.

## Funktionen

- Überwacht die Website in konfigurierbaren Intervallen
- Erkennt neue Links und gibt Benachrichtigungen aus
- Speichert gefundene Links persistent
- Konfigurierbare Einstellungen über JSON-Datei
- **Automatische Posts auf Lemmy-Server** für neue Links
- **Token-Caching** für effiziente API-Nutzung
- **Einmalige oder kontinuierliche Überwachung**

## Schnellinstallation

Für eine automatische Installation steht ein Installationsskript zur Verfügung:

```bash
bash ./install.sh
```

**Voraussetzungen:**
- Go 1.19 oder höher
- Git
- sudo-Rechte

