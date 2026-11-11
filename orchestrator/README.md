# Orchestrator

Der Orchestrator führt feste Abläufe Schritt für Schritt aus (Python, LangGraph). Er ersetzt den früheren Ablauf-Starter. Das Paket heißt weiter `ablauf`.

- **Jeder Schritt geht an den Pförtner** (`/v1/ausfuehren`), mit dem eigenen Schlüssel des Dienstes aus der Datei in `BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI` (siehe kernel/README.md, „Wer anfragen darf“). Der Orchestrator entscheidet nichts selbst.
- **Freigabe durch einen Menschen:** Sagt der Pförtner „wartet“, hält der Lauf an und nennt die Freigabe (`freigabe_id`) und was ein Mensch dazu sehen soll (`anzeige`). Nach der Entscheidung macht `Orchestrator.setze_fort(firma, lauf)` dort weiter: Er legt dem Pförtner denselben Schritt mit denselben Daten noch einmal vor. Ob der Schritt jetzt läuft, sagt allein der Pförtner. Der Orchestrator kann sich keine Zustimmung geben, sein Schlüssel wird beim Entscheiden abgewiesen (siehe kernel/README.md, „Freigabe durch einen Menschen“).
- **Stand eines Laufs (Checkpoint):** Damit ein wartender Lauf einen Neustart übersteht, steht sein Stand in PostgreSQL (`arbeit.ablaufstaende`, `arbeit.ablaufschritte`). Getrennt nach Firma mit Row-Level Security, der Inhalt verschlüsselt mit AES-256-GCM und an die Firma gebunden. Ist der Lauf fertig, wird der Stand gelöscht. Details in `ablauf/stand.py`.
- **Der Lauf ist die Aufgabe beim Pförtner.** Stoppen (`/v1/stoppen`) und die Obergrenze an Schritten gelten damit je Lauf.

Dateien: `orchestrator.py` (Läufe starten und fortsetzen, der Baustein `schritt`), `ablaeufe.py` (die festen Abläufe), `stand.py` (Stände), `einordnen.py` (Ablauf 1).

## Was er noch nicht kann

- Er sucht sich keine Fähigkeiten selbst aus dem Katalog und plant nichts mit einem KI-Modell. Abläufe sind fest beschrieben.
- Das Dashboard zeigt wartende Läufe noch nicht. Dashboard und Befehlszeile nutzen den Stand in PostgreSQL noch nicht (nächster Schritt) und halten Stände bis dahin nur im Arbeitsspeicher. Ablauf 1 hat ohnehin keine Stelle, an der ein Mensch bestätigen muss.
- Die Freigabe ist nur mit einem Probe-Ablauf in den Tests belegt. Im Katalog gibt es noch keine Fähigkeit, die nach außen wirkt.
- Ein Schlüssel für die Stände gilt für alle Firmen. Je Firma ein eigener aus dem Schlüsseldienst (STACKIT KMS) kommt mit dem Hosting. Einen Schlüsselwechsel gibt es noch nicht.
- Wartende Läufe räumt noch niemand auf, wenn ihre Freigabe abläuft.

## Regel für neue Abläufe

In den Zustand eines Laufs gehört nur, was zum Weitermachen nötig ist. Mailtexte bleiben innerhalb eines Schritts und kommen nie in den Zustand. Ein Schritt beginnt beim Fortsetzen von vorn, er darf also vor dem Aufruf des Pförtners nichts tun, was nicht wiederholt werden darf.

## Ablauf 1: Postfach einordnen

Mails eines Postfachs **lesen → auf besondere Daten prüfen → einordnen**, als ein einziger Schritt im Graphen, damit keine Mail in den gespeicherten Stand gerät.

- Auch eine Mail mit Art.-9-Verdacht legt er zum Einordnen vor: Der Pförtner lehnt ab, und die Ablehnung steht in seinem Audit.
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
BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI=/ausserhalb/des/repos/ablauf.schluessel \
  python -m ablauf --einstellungen ../testdaten/ablauf.json
```

Ohne `BOTICSTUDIO_ARBEITSDATEN` werden die Ergebnisse nur gezählt, nicht gespeichert.

Schlüssel für die Stände anlegen (einmal, außerhalb des Repos, nur für den eigenen Benutzer lesbar):

```sh
python -m ablauf.stand --neu /ausserhalb/des/repos/ablaufstand.schluessel
```

## Dashboard

```sh
(cd ../dashboard && npm ci && npm run build)
BOTICSTUDIO_ARBEITSDATEN=postgres://boticstudio_ablauf:...@localhost/boticstudio \
BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI=/ausserhalb/des/repos/dashboard.schluessel \
  python -m ablauf.dashboard --einstellungen ../testdaten/ablauf.json
```

Vorher ein Konto anlegen. Die Person muss es in der Firma schon geben, das Passwort (mindestens 12 Zeichen) fragt der Befehl verdeckt ab:

```sh
BOTICSTUDIO_ARBEITSDATEN=postgres://boticstudio_ablauf:...@localhost/boticstudio \
  python -m ablauf.konto --firma testfirma-echt --person ablauf --email chefin@firma.de
```

Dann http://127.0.0.1:8490 öffnen und anmelden. Der Knopf „Jetzt einordnen“ startet den Ablauf im Hintergrund, immer nur einen zur selben Zeit.

- `GET /api/ich`: ob eine Anmeldung nötig ist und wer angemeldet ist. Ohne Sitzung erlaubt, alles andere unter `/api` braucht eine.
- `POST /api/anmelden` (`{"email", "passwort"}`) und `POST /api/abmelden`: nur mit der Kopfzeile `X-BoticStudio-Aktion: 1`.
- `GET /api/zustand`: Firma, Postfach, Kategorien, ob gerade eingeordnet wird, Zahlen des letzten Laufs.
- `GET /api/einordnungen`: die Ergebnisse der Firma, neueste zuerst. Je Mail nur Verweis, Stand, Kategorie, Begründung, Art.-9-Verdacht ja/nein und die Nachweis-Nummer im Audit. Nie Betreff, Absender, Text oder Message-ID.
- `GET /api/koepfe`: Absender, Betreff und Datum, live über den Pförtner (Fähigkeit `mail_kopf_lesen`, ohne Text). Nicht gespeichert, jeder Abruf steht im Audit. Nur mit der Kopfzeile `X-BoticStudio-Aktion: 1`, weil er einen Abruf am Postfach auslöst.
- `POST /api/einordnen`: startet das Einordnen. Nur mit der Kopfzeile `X-BoticStudio-Aktion: 1`.

### Anmeldung

- Konten und Sitzungen liegen im Schema `konto`, getrennt nach Firma mit Row-Level Security wie alle anderen Tabellen. Beim Anmelden findet der SHA-256 der E-Mail-Adresse die Firma (`konto.anmeldenamen`, für alle lesbar, enthält keine Adresse).
- Passwörter nur als scrypt-Hash (N=2^17, r=8, p=1). Der Sitzungsschlüssel steht nur im Cookie (`HttpOnly`, `SameSite=Strict`), in der Datenbank nur sein SHA-256.
- Eine Sitzung gilt 8 Stunden, nach 60 Minuten ohne Nutzung endet sie früher.
- Nach 5 Fehlversuchen ist das Konto 15 Minuten gesperrt. Die Antwort ist immer dieselbe, ob es die Adresse gibt oder nicht, und dauert gleich lang.
- Dem Pförtner nennt das Dashboard die angemeldete Person. Er prüft deren Erlaubnisse und schreibt sie ins Audit. Der Pförtner-Code bleibt dafür unverändert.
- Ein Dienst gehört zu einer Firma: Konten anderer Firmen kommen nicht hinein.
- Passwörter, Schlüssel und E-Mail-Adressen stehen in keinem Log und keiner Fehlermeldung.

Ohne `BOTICSTUDIO_ARBEITSDATEN` läuft das Dashboard im Testmodus ohne Anmeldung, mit der Person aus den Einstellungen.

Weiter gilt: Der Dienst hört nur auf 127.0.0.1, nimmt nur Anfragen an `127.0.0.1:<port>` oder `localhost:<port>` an (gegen DNS-Rebinding) und löst nichts ohne die eigene Kopfzeile aus, die eine fremde Webseite nicht mitschicken kann. Für einen Betrieb im Netz fehlt noch HTTPS (dann mit `Secure`-Cookie).

## Mit einem echten Test-Postfach

Die Zugangsdaten stehen nur in der Umgebung (`BOTICSTUDIO_TEST_IMAP_BENUTZER`, `BOTICSTUDIO_TEST_IMAP_PASSWORT`, optional `BOTICSTUDIO_TEST_IMAP_SERVER`, Standard `imap.gmail.com`), nie im Repo oder Chat.

```sh
python -m ablauf.echter_test --ordner /ausserhalb/des/repos/echt
```

Das schreibt `tresor.json` (nur für den Besitzer lesbar), `firma.json` (Testfirma mit Erlaubnissen für genau dieses Postfach), `ablauf.json`, `dienste.json` und je einen eigenen Schlüssel `ablauf.schluessel` und `dashboard.schluessel`. Dann den Pförtner mit `-testdaten …/firma.json -tresor …/tresor.json -dienste …/dienste.json` starten, den Ablauf mit `BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI=…/ablauf.schluessel` und `--einstellungen …/ablauf.json`, das Dashboard mit `BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI=…/dashboard.schluessel`. Das Passwort wird nie ausgegeben, und der Ordner darf nicht im Repo liegen.

Für Google braucht das Postfach die Bestätigung in zwei Schritten und ein App-Passwort.

## Tests mit Datenbank

`BOTICSTUDIO_TEST_ARBEITSDATEN` (Rolle `boticstudio_ablauf`) und `BOTICSTUDIO_TEST_ARBEITSDATEN_EIGENTUEMER` auf eine migrierte Datenbank zeigen lassen. Fehlen sie, werden diese Tests übersprungen (in CI nicht erlaubt).
