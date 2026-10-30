<p align="center"><img src="assets/icon.png" alt="Nodelight" width="160"></p>

<p align="center"><a href="README.md">English</a> · <a href="README.fr.md">Français</a> · <a href="README.nl.md">Nederlands</a> · <b>Deutsch</b> · <a href="README.es.md">Español</a></p>

# Nodelight

Ein **portabler Moonlight-Client für Windows**, der sich **über Tailscale** mit deinem
Sunshine/Apollo-PC verbindet: keine Installation, kein Treiber, keine Administratorrechte und kein
Port, den du am Router öffnen müsstest.

> **KI-generiertes Projekt.** Der gesamte Code, alle Skripte und die Dokumentation dieses Repositorys
> wurden von **Claude Sonnet 5.5** (Anthropic) über Claude Code geschrieben, unter Anleitung des
> Repository-Besitzers („Vibe Coding"). Der Code wurde **nicht von einem Menschen geprüft** und nur
> **teilweise auf einem echten Rechner getestet** (er lässt sich bauen, startet und wird angezeigt; ein
> vollständiger Stream über Tailscale ist noch nicht bestätigt): Nutzung auf eigene Gefahr (siehe
> „Prüfstatus").

Dieses Repository ist ein **Fork von [moonlight-qt](https://github.com/moonlight-stream/moonlight-qt)**:
Der Client-Code stammt von Moonlight, die Änderungen von Nodelight wurden als Commits darüber
hinzugefügt (siehe `git log`). Das Originalrepository ist als `upstream` eingerichtet, um dessen
Updates zu übernehmen. Der Rest dieser Datei beschreibt nur, was Nodelight hinzufügt.

Nodelight steht in keiner Verbindung zu Tailscale Inc., dem Moonlight-Projekt oder
Artemis/Apollo/Sunshine. Tailscale ist eine Marke von Tailscale Inc.

## Funktionsweise

| Komponente | Sprache | Aufgabe |
| --- | --- | --- |
| `bridge/` → `Nodelight.exe` | Go | Tailscale-Knoten im Userspace (`tsnet`) + lokales TCP/UDP-Relay, startet Moonlight |
| `app/`, `moonlight-common-c/`… → `client\NodelightClient.exe` | C++/Qt | Der angepasste Moonlight-Client: „Remote"-Modus für das Relay, von Artemis übernommene Apollo-Option für virtuelle Bildschirme, integrierte Tailscale-Seite, Name und Icon von Nodelight |

Moonlight streamt über UDP (Video, Audio, Steuerung) mit rohen Sockets, die nicht durch einen
SOCKS5-Proxy gehen können, und liest die Portnummern aus den RTSP-Antworten des Hosts. Nodelight
lauscht deshalb auf **denselben Ports wie Sunshine** an einer eigenen Loopback-Adresse pro PC
(`127.77.0.1`, `127.77.0.2`, …) und leitet sie durch das Tailnet weiter.

```
Moonlight ──► 127.77.0.1:47989/47984/48010 (TCP), :47998-48002 (UDP)
                 │  Nodelight (Relay + tsnet/WireGuard, Userspace)
                 ▼
          Sunshine/Apollo-Host-PC  (über dein Tailnet)
```

Pro Host weitergeleitete Ports (Basis 47989): TCP 47984, 47989, 48010 (+ 47990 mit `--web-ui`),
UDP 47998, 47999, 48000, 48002.

## Verwendung

1. Doppelklicke auf `Nodelight.exe`: **Es öffnet sich keine Konsole**, Moonlight startet direkt (im
   portablen Modus dank einer Datei `portable.dat`: Einstellungen und Kopplungen bleiben im Ordner).
   Nodelight beendet sich mit Moonlight. Was es sonst ausgeben würde, steht in `Nodelight.log`, und
   Startfehler erscheinen in einem Dialogfenster.
2. Beim ersten Start öffnet Moonlight seine **Tailscale**-Seite (eine Schaltfläche mit
   Netzwerk-Icon in der oberen Leiste). Klicke auf **Connect**: Eine Webseite öffnet sich, um das
   Gerät zu autorisieren (einmalig). Die Identität wird in `data\` gespeichert (halte diesen Ordner
   privat; er lässt sich von einem PC auf einen anderen kopieren). Zum Verbinden ist kein PC nötig.
3. Sobald die Verbindung steht, wähle bei Bedarf den **Exit-Node** (siehe unten) und dann **Add a PC**:
   einen Namen und den Tailscale-Namen oder die IP des PCs, danach **Save PCs**. Der PC wird sofort
   übernommen.
4. Die PCs erscheinen **automatisch** in Moonlight (`127.77.0.1`, `127.77.0.2`… in der Reihenfolge der
   Liste; ändere sie nicht, wenn schon PCs gekoppelt sind). Koppeln wie gewohnt.

Aus einem Terminal gestartet, schreibt `Nodelight.exe` in dieses Terminal und stellt die Fragen des
ersten Starts (nützlich mit `--json` oder `--no-launch`).

Nützliche Optionen: `--host name=adresse[:basisport]` (wiederholbar), `--hostname`, `--state-dir`,
`--no-browser`, `--json` (Ereignisse zeilenweise), `--listen-prefix`, `--web-ui`, `--control-url` für
einen selbst gehosteten Headscale-Server und `--exit-node` (siehe unten).

### Einstellungsseite

Verbindung, Exit-Node, PCs und Profile lassen sich ohne Dateibearbeitung auf zwei Wegen einstellen:

- **In Moonlight**: Schaltfläche **Tailscale (Nodelight)** (Netzwerk-Icon) in der oberen Leiste. Die
  Seite bietet anklickbare Bedienelemente für: den Verbindungsstatus und **Connect / Disconnect**
  (plus eine Schaltfläche zum Öffnen der Tailscale-Anmeldeseite), den **Exit-Node** (Liste aus
  deinem Tailnet + **Apply exit node**), die **PC-Liste** (Name, Adresse, Basisport, lokale Adresse
  `127.77.0.N`, grüner Punkt, wenn der PC antwortet, **Add a PC** / ✕ / **Save PCs**) und die
  **Profile** (wechseln, löschen, anlegen). Die Schaltfläche erscheint nur, wenn Moonlight von
  Nodelight gestartet wurde. Ein kürzerer gleichwertiger Abschnitt bleibt in den Einstellungen.
- **Im Browser** (Notlösung): Die Adresse steht in `Nodelight.log`
  (`Settings page: http://127.0.0.1:PORT/?token=…`) oder im Terminal; gleicher Inhalt.

Ein **Profil** fasst eine Tailscale-Identität, einen Gerätenamen, einen optionalen Kontrollserver
(Headscale) und eine PC-Liste zusammen. So kannst du von einem Tailscale-Netzwerk zu einem anderen
wechseln; jedes Profil behält seine Identität in `data\profiles\<name>`, das Standardprofil bleibt in
`data\`. Profile werden in `hosts.json` gespeichert (Dateien früherer Versionen bleiben gültig).

Hinweise:
- Trennen, Profilwechsel oder Ändern des Exit-Nodes **unterbricht den laufenden Stream**.
- Ein auf der Kommandozeile angegebener Wert (`--exit-node`, `--host`…) gilt beim Start, ohne
  gespeichert zu werden; eine spätere Auswahl auf der Seite ersetzt ihn.
- Sicherheit: Der Server lauscht nur auf `127.0.0.1`, jeder Aufruf verlangt ein bei jedem Start
  zufällig erzeugtes Token (per Umgebungsvariable an Moonlight übergeben), und Anfragen, deren
  `Host`-Header nicht die lokale Adresse ist, werden abgelehnt (Schutz gegen „DNS-Rebinding"). Ein
  anderes Programm in deiner Windows-Sitzung kann diese Umgebung jedoch lesen: Das ist keine
  Sicherheitsgrenze gegen Code, der bereits auf deinem PC läuft.

### Host-PC ohne Tailscale (Exit-Node)

Hat dein Sunshine/Apollo-PC kein Tailscale, aber ein anderes Gerät in seinem lokalen Netz (Home
Assistant, NAS, Raspberry Pi…) ist **als Exit-Node** in deinem Tailnet, kann Nodelight darüber gehen:

```json
{
  "exitNode": "homeassistant",
  "hosts": [ { "name": "buero", "address": "192.168.1.20" } ]
}
```

- `exitNode`: Tailscale-Name oder -IP des Exit-Nodes (oder `--exit-node`, oder die Liste auf der
  Tailscale-Seite). `address` ist dann die **lokale Adresse** des Host-PCs (`192.168.x.y`), gesehen
  aus dem Netz des Exit-Nodes.
- Der Exit-Node muss in der Tailscale-Admin-Konsole genehmigt sein (Machines → Edit route settings →
  Use as exit node). Das Gerät `nodelight` muss ihn nutzen dürfen (mit den Standard-ACLs der Fall).
- Gleiches Prinzip wie bei einem entfernten Rechner, der diesen Exit-Node nutzt: Erreicht dein PC die
  lokale Adresse des Hosts so, sollte Nodelight es auch können. **Ungetestet**: Ich konnte nicht
  prüfen, ob die Bibliothek `tsnet` den UDP-Streaming-Verkehr wirklich über einen Exit-Node an eine
  lokale Adresse leitet. Das ist der erste Punkt zum Testen.
- Leistung: Der gesamte Videostream läuft über den Exit-Node-Rechner, mit WireGuard verschlüsselt. Ein
  kleiner Hausautomations-Server kann den Durchsatz begrenzen; senke die Videobitrate in Moonlight,
  wenn das Bild ruckelt. Kann der Host Tailscale bekommen, ist das immer einfacher und schneller
  (direkte Verbindung, `100.x`-Adresse oder MagicDNS-Name statt lokaler IP, und kein Exit-Node mehr).

### Host-Seite (Sunshine/Apollo)

- Der Host-PC muss im selben Tailnet und eingeschaltet sein, mit laufendem Sunshine/Apollo. Nodelight
  meldet beim Start, ob jeder Host auf seinem HTTP-Port antwortet.
- Die Windows-Firewall des Hosts muss Sunshine/Apollo auf der Tailscale-Schnittstelle erlauben
  (betreffendes Netzwerkprofil); deine Tailscale-ACLs müssen diese Ports zulassen.
- Sunshine erlaubt das Koppeln (`origin_pin_allowed`) standardmäßig nur aus dem lokalen Netz: Wird die
  Kopplung abgelehnt, passe diese Einstellung an oder koppele einmal im LAN.
- Für gute Leistung braucht es eine **direkte** Tailscale-Verbindung (nicht über ein DERP-Relay);
  die WireGuard-Verschlüsselung im Userspace braucht etwas CPU.

## Den Build ohne Entwicklungsumgebung erhalten

Die Binärdatei wird auf den kostenlosen Windows-Rechnern von GitHub Actions gebaut:

1. Reiter **Actions** → **Build Windows x64 (portable)** → **Run workflow** (30 bis 60 Minuten; ein
   `push` auf `master` startet ihn ebenfalls). Die ursprünglichen Workflows von Moonlight (AppImage,
   Steam Link, Windows/macOS-Signierung) wurden aus diesem Fork entfernt: Sie benötigen Secrets, die
   hier nicht existieren.
2. Lade das Artefakt `Nodelight-win-x64` (eine Zip-Datei) herunter und entpacke es: Das ist alles.

Die Zip-Datei enthält die Visual-C++-Laufzeit (lokale Kopie): keine Voraussetzungen auf dem
Zielrechner. Es werden keine erhöhten Rechte angefordert (`asInvoker`-Manifest, Bridge im Userspace,
lokale Ports > 1024 nur auf Loopback). Die ausführbare Datei ist nicht signiert: SmartScreen kann
um Bestätigung bitten, und die Windows-Firewall kann eine Netzwerkfreigabe vorschlagen, die du
abbrechen kannst.

## Manueller Build (Windows x64)

Voraussetzungen: Go 1.26+, Git, Qt 6.11+ (MSVC), Visual Studio 2026 (oder die von moonlight-qt
geforderte Version), PowerShell.

```powershell
git clone --recurse-submodules https://github.com/BKNadir/Nodelight
cd Nodelight
.\scripts\build-bridge.ps1      # Tests + dist\Nodelight.exe
.\setup-deps.ps1
scripts\build-arch.bat release  # aus einer Qt- + MSVC-Eingabeaufforderung
.\scripts\package.ps1           # dist\Nodelight-win-x64.zip
```

## Was von Artemis stammt und was nicht

Artemis ist ein **Android**-Client für Apollo. Viele seiner Funktionen ergeben unter Windows keinen
Sinn oder existieren bereits in moonlight-qt.

| Artemis-Funktion | Stand hier |
| --- | --- |
| Virtueller Bildschirm von Apollo | **Erledigt**: Kontrollkästchen in den Host-Einstellungen + `--virtual-display` |
| Eigene Auflösung und Bitrate | Bereits in moonlight-qt vorhanden |
| Apollo-Serverbefehle | Nicht umgesetzt: benötigt den Fork von `moonlight-common-c` von Artemis (`LiSendExecServerCmd`) |
| Zwischenablage-Synchronisierung | Nicht umgesetzt: HTTP-Aufrufe `actions/clipboard` + `QClipboard`-Integration zu schreiben |
| Virtuelle Tasten, Touch-Trackpad, DeX, Hochformat, SBS 3D… | Android-spezifisch, nicht anwendbar |

## Prüfstatus — bitte lesen

In der Entwicklungsumgebung geprüft (ohne Windows):
- Relay-Tests (TCP-Hin-und-zurück mit Half-Close, unabhängige UDP-Flüsse pro Client, Ablauf inaktiver
  Flüsse, „Alles-oder-nichts"-Bindung, sauberes Beenden): OK mit `-race`;
- Tests von Konfigurationsparser, Profilen und Eingabe beim ersten Start;
- Controller-Tests (Verbinden mit oder ohne PCs, Trennen, Verbindung mit Warten auf Autorisierung,
  Fehler, Exit-Node im laufenden Betrieb ändern, PC-Liste, Profil wechseln und löschen) und Tests der
  lokalen API (Token, `Host`-Schutz, Validierung), mit einem Fake-Tailscale, mehrfach mit `-race`
  ausgeführt;
- die JavaScript-Logik der QML-Seiten wurde unter Node mit dem echten JSON des Go-Codes ausgeführt;
- die Client-Änderungen lassen sich sauber auf den Upstream-Commit `3178022` von moonlight-qt anwenden.

Vom Autor unter Windows festgestellt: Der GitHub-Actions-Build läuft durch, die Anwendung startet,
die Icons erscheinen an den richtigen Stellen und die Tailscale-Seite öffnet sich in Moonlight.

**Nicht geprüft** (zu testen):
- die echte Tailscale-Verbindung aus der Anwendung (Autorisierung, Liste der Exit-Nodes);
- das echte Routing des Verkehrs über einen Exit-Node mit `tsnet` (insbesondere UDP);
- ein vollständiger Stream über Tailscale sowie das Verhalten von Firewall/ACLs;
- das Anhängen an das Terminal, die Logdatei und die Dialogfenster von `Nodelight.exe` in allen Fällen.

Schlägt ein Schritt fehl, eröffne ein Issue mit der Fehlermeldung (und `Nodelight.log`): Die
wahrscheinlichsten Ursachen sind eine Abweichung der `tsnet`-API (anzupassen in
`bridge/cmd/nodelight/tunnel_ts.go`) oder ein bereits belegter Port auf der Loopback-Adresse (zum
Beispiel ein lokales Sunshine).

## Code-Signierung

Die ausführbaren Dateien sind **noch nicht signiert** (SmartScreen kann warnen: „Weitere Informationen" → „Trotzdem ausführen"). Ein Antrag auf kostenlose Signierung bei der [SignPath Foundation](https://signpath.org/) läuft. Vollständige Richtlinie (auf Englisch): [Code signing policy](README.md#code-signing-policy).

## Lizenz

GPL-3.0 (siehe `LICENSE`): Der Code ist von moonlight-qt abgeleitet. Ausführliche Angaben in
`licenses/NOTICE.md`.
