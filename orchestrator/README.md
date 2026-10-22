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

# Pförtner und die drei Module laufen (siehe kernel/README.md, faehigkeiten/*/README.md)
BOTICSTUDIO_ARBEITSDATEN=postgres://boticstudio_ablauf:...@localhost/boticstudio \
  python -m ablauf --einstellungen ../testdaten/ablauf.json
```

Ohne `BOTICSTUDIO_ARBEITSDATEN` werden die Ergebnisse nur gezählt, nicht gespeichert.

## Mit einem echten Test-Postfach

Die Zugangsdaten stehen nur in der Umgebung (`BOTICSTUDIO_TEST_IMAP_BENUTZER`, `BOTICSTUDIO_TEST_IMAP_PASSWORT`, optional `BOTICSTUDIO_TEST_IMAP_SERVER`, Standard `imap.gmail.com`), nie im Repo oder Chat.

```sh
python -m ablauf.echter_test --ordner /ausserhalb/des/repos/echt
```

Das schreibt `tresor.json` (nur für den Besitzer lesbar), `firma.json` (Testfirma mit Erlaubnissen für genau dieses Postfach) und `ablauf.json`. Dann den Pförtner mit `-testdaten …/firma.json -tresor …/tresor.json` starten und den Ablauf mit `--einstellungen …/ablauf.json`. Das Passwort wird nie ausgegeben, und der Ordner darf nicht im Repo liegen.

Für Google braucht das Postfach die Bestätigung in zwei Schritten und ein App-Passwort.

## Tests mit Datenbank

`BOTICSTUDIO_TEST_ARBEITSDATEN` (Rolle `boticstudio_ablauf`) und `BOTICSTUDIO_TEST_ARBEITSDATEN_EIGENTUEMER` auf eine migrierte Datenbank zeigen lassen. Fehlen sie, werden diese Tests übersprungen (in CI nicht erlaubt).
