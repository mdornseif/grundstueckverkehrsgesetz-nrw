# Grundstückverkehrsgesetz Monitor

Überwacht eine Website auf neue Links und postet diese automatisiert zu Lemmy und/oder Mastodon.

## Mastodon
- Getestet mit GoToSocial.
- Es wird ein Access Token benötigt (siehe unten).
- ClientID/ClientSecret sind für GoToSocial nicht erforderlich, wenn ein Access Token verwendet wird.
- Für klassische Mastodon-Server (z.B. mastodon.social) kann alternativ ein Login per Username/Passwort erfolgen. Dafür müssen `mastodon_client_id` und `mastodon_client_secret` in der Konfiguration gesetzt werden. Diese erhält man durch das Anlegen einer eigenen Anwendung im Mastodon-Webinterface unter Einstellungen → Entwicklung → Eigene Anwendungen.
- Felder:
  - `mastodon_access_token`: Empfohlen für GoToSocial, reicht für die meisten Anwendungsfälle.
  - `mastodon_username`, `mastodon_password`, `mastodon_client_id`, `mastodon_client_secret` 

### Hinweis zu GoToSocial: Redirect-URI/Callback-URL
Um im GoToSocial-Webinterface im Bereich „Access Tokens“ einen Token für eine Anwendung generieren zu können, muss die Redirect-URI der Anwendung **zusätzlich** die folgende Callback-URL enthalten:

```
https://[deine_instanz]/settings/user/applications/callback
```

Beispiel für Redirect-URIs beim Anlegen der App:
```
urn:ietf:wg:oauth:2.0:oob https://social.23.nu/settings/user/applications/callback
```

Nur dann ist der Button „Request access token“ im Webinterface aktiv und du kannst einen Token generieren.

## Fehlerverhalten
- Schlägt das Posten zu Lemmy oder Mastodon fehl, wird der Link nicht als erledigt markiert und beim nächsten Durchlauf erneut versucht.
- Ist keine Plattform konfiguriert, wird ein Fehler geloggt und der Link bleibt unbearbeitet.

## Beispielkonfiguration (`config.json`)
```json
{
  "url": "http://www.grundstueckverkehrsgesetz.nrw.de",
  "check_interval": 43200,
  "data_file": "links.json",
  "lemmy_server": "https://lemmy.example.org",
  "lemmy_community": "kulturlandschaft",
  "lemmy_username": "gvgbot",
  "lemmy_password": "CHANGEME",
  "ignore_dirs": ["guetersloh"],
  "mastodon_server": "https://social.23.nu/",
  "mastodon_access_token": "OTI2Y2NMMDGTMWNHZS0ZNGRILTG0MGMTMDQXZMVMZGM1ZJQ5",
  "mastodon_visibility": "unlisted"
}
```

## Update
- `install.sh` überschreibt keine bestehenden Konfigurationsdateien in `/opt/grundstueckverkehrsgesetz/`.
- Binary-Update läuft auch bei laufendem Service.

## Service-Management
- Start/Stop/Status: `sudo systemctl [start|stop|status] grundstueckverkehrsgesetz-monitor`
- Logs: `sudo journalctl -u grundstueckverkehrsgesetz-monitor -f -n 50`

