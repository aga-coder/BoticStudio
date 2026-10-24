# Ablauf (Vorstufe des Orchestrators)

Ein kleiner Ablauf-Starter für den ersten Ablauf: Mails eines Postfachs **lesen → auf besondere Daten prüfen → einordnen**. Der richtige Orchestrator (Python, LangGraph) kommt später und ersetzt diesen Starter.

- Jeder Schritt geht an den Pförtner (`/v1/ausfuehren`). Der Ablauf entscheidet nichts selbst. Auch eine Mail mit Art.-9-Verdacht legt er zum Einordnen vor: Der Pförtner lehnt ab, und die Ablehnung steht in seinem Audit.
- Prüfen und Einordnen bekommen dieselbe, unveränderte Mail. Daran erkennt der Pförtner, dass sie vorgeprüft ist.
- Ergebnisse landen in `arbeit.einordnungen` (Rolle `boticstudio_ablauf`, Row-Level Security je Firma). Gespeichert wird nur ein **Verweis** auf die Mail (Bereich, UID, Message-ID), Stand, Kategorie, Begründung, Markierungen und Modell. Nie Betreff, Absender oder Text.
- Ausgegeben werden nur Zahlen.

Stände: `eingeordnet` (mit Kategorie), `nicht_eingeordnet` (z. B. Art.-9-Verdacht oder Modell unsicher), `fehler` (z. B. Modell nicht erreichbar).

## Starten

```sh
python -m venv .venv && . .venv/bin/activate
pip install -r requirements-dev.txt
python -m pytest -q

# Pförtner und die vier Module laufen (siehe kernel/README.md, faehigkeiten/*/README.md)
BOTICSTUDIO_ARBEITSDATEN=postgres://boticstudio_ablauf:...@localhost/boticstudio \
  python -m ablauf --einstellungen ../testdaten/ablauf.json
```

Ohne `BOTICSTUDIO_ARBEITSDATEN` werden die Ergebnisse nur gezählt, nicht gespeichert.

## Dashboard

```sh
(cd ../dashboard && npm ci && npm run build)
BOTICSTUDIO_ARBEITSDATEN=postgres://boticstudio_ablauf:...@localhost/boticstudio \
  python -m ablauf.dashboard --einstellungen ../testdaten/ablauf.json
```

Dann http://127.0.0.1:8490 öffnen. Der Knopf „Jetzt einordnen“ startet den Ablauf im Hintergrund, immer nur einen zur selben Zeit.

- `GET /api/zustand`: Firma, Postfach, Kategorien, ob gerade eingeordnet wird, Zahlen des letzten Laufs.
- `GET /api/einordnungen`: die Ergebnisse der Firma, neueste zuerst. Je Mail nur Verweis, Stand, Kategorie, Begründung, Art.-9-Verdacht ja/nein und die Nachweis-Nummer im Audit. Nie Betreff, Absender, Text oder Message-ID.
- `GET /api/koepfe`: Absender, Betreff und Datum, live über den Pförtner (Fähigkeit `mail_kopf_lesen`, ohne Text). Nicht gespeichert, jeder Abruf steht im Audit. Nur mit der Kopfzeile `X-BoticStudio-Aktion: 1`, weil er einen Abruf am Postfach auslöst.
- `POST /api/einordnen`: startet das Einordnen. Nur mit der Kopfzeile `X-BoticStudio-Aktion: 1`.

Noch gibt es keine Anmeldung. Deshalb gilt bis dahin: Der Dienst hört nur auf 127.0.0.1, nimmt nur Anfragen an `127.0.0.1:<port>` oder `localhost:<port>` an (gegen DNS-Rebinding) und startet das Einordnen nur mit der eigenen Kopfzeile, die eine fremde Webseite nicht mitschicken kann. Für einen Betrieb im Netz braucht es vorher die Anmeldung.

## Mit einem echten Test-Postfach

Die Zugangsdaten stehen nur in der Umgebung (`BOTICSTUDIO_TEST_IMAP_BENUTZER`, `BOTICSTUDIO_TEST_IMAP_PASSWORT`, optional `BOTICSTUDIO_TEST_IMAP_SERVER`, Standard `imap.gmail.com`), nie im Repo oder Chat.

```sh
python -m ablauf.echter_test --ordner /ausserhalb/des/repos/echt
```

Das schreibt `tresor.json` (nur für den Besitzer lesbar), `firma.json` (Testfirma mit Erlaubnissen für genau dieses Postfach) und `ablauf.json`. Dann den Pförtner mit `-testdaten …/firma.json -tresor …/tresor.json` starten und den Ablauf mit `--einstellungen …/ablauf.json`. Das Passwort wird nie ausgegeben, und der Ordner darf nicht im Repo liegen.

Für Google braucht das Postfach die Bestätigung in zwei Schritten und ein App-Passwort.

## Tests mit Datenbank

`BOTICSTUDIO_TEST_ARBEITSDATEN` (Rolle `boticstudio_ablauf`) und `BOTICSTUDIO_TEST_ARBEITSDATEN_EIGENTUEMER` auf eine migrierte Datenbank zeigen lassen. Fehlen sie, werden diese Tests übersprungen (in CI nicht erlaubt).
