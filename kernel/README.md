# Pförtner (Kernel)

Der Pförtner prüft jeden Schritt, den der Orchestrator ausführen möchte, und antwortet mit **erlaubt**, **abgelehnt** oder **wartet**. Er plant nichts und wählt weder Modell noch Fähigkeit.

## Zwei Grundregeln

- **Regel A: Der Pförtner kennt keine einzelne Fähigkeit.** Er vergleicht nur den Beipackzettel mit den Erlaubnissen der Firma. Ein Test durchsucht den Code nach Namen von Fähigkeiten, ein zweiter lädt eine ganz neue Fähigkeit nur über ihren Zettel.
- **Regel B: Der Beipackzettel hat Fassungen.** Neue Felder kommen dazu, alte werden nie gestrichen oder umgedeutet. Fehlt ein Feld, gilt der strengste Wert (`risiko: hoch`, `wirkt_nach_aussen: true`, kein Datenausgang, bei Datenausgang muss jede Eingabe vorgeprüft sein). Eine neuere Fassung, als der Pförtner kennt, lehnt er ab.
  - Fassung 2: `zugang`. Fassung 3: `prueft` und `braucht_vorpruefung`.

## Was er prüft, der Reihe nach

1. Wer fragt: Firma, Person (Rolle kommt aus seinen eigenen Daten, nie aus der Anfrage), Aufgabe.
2. Läuft die Aufgabe noch, ist die Obergrenze an Schritten erreicht?
3. Steht die Fähigkeit in dieser Version im Katalog? Bleibt die Aufgabe bei ihrer Version?
4. Gibt es eine gültige Erlaubnis für genau diesen Bereich (Teil für Teil verglichen)?
5. Verlassen Daten BoticStudio: Ist ein Ziel genannt (Pflicht bei Datenausgang)? Sind die Daten vorgeprüft (siehe unten)? Erlaubt der Zettel das Ziel, steht es auf der Liste der Firma, liegt eine Einwilligung vor, und sperrt eine Markierung (z. B. `art9_verdacht`) die Region?
6. Wirkt es nach außen oder ist es riskant: dann **wartet**, bis ein Mensch bestätigt (siehe „Freigabe durch einen Menschen“).

Jede Entscheidung geht an den Auditor (nur anhängen, Hash-Kette je Firma). Kann er nicht schreiben, wird abgelehnt. Entscheidung, Audit-Eintrag und Folgen (z. B. eine einmalige Erlaubnis gilt als verbraucht) werden gemeinsam gespeichert oder gar nicht.

## Vorprüfung vor dem Datenausgang

Der Pförtner kennt keine einzelne Prüfung, nur zwei Felder im Beipackzettel:

- `prueft`: Diese Fähigkeit prüft die genannten Eingaben und gibt `markierungen` aus. Sie darf selbst keine Daten hinausgeben. Nach dem Ausführen merkt sich der Pförtner ihr Ergebnis zum Fingerabdruck (SHA-256) genau dieser Daten. Der Inhalt selbst wird nicht gespeichert.
- `braucht_vorpruefung`: Diese Eingaben gehen nur an ein Ziel, wenn es zu genau diesem Inhalt ein Prüfergebnis gibt. Die Markierungen daraus setzt der Pförtner selbst. Der Orchestrator kann Markierungen nur hinzufügen, nie entfernen. Eine spätere Prüfung ohne Treffer nimmt eine Markierung nicht weg.

Die Fingerabdrücke bildet nur die Ausführung aus den echten Eingaben. Eine reine Prüfanfrage (`/v1/pruefen`) kann eine Vorprüfung darum nicht belegen und wird abgelehnt. Prüfergebnisse gelten nur in der eigenen Firma (eigene Tabelle mit RLS, nur anhängen).

Für den ersten Ablauf heißt das: „Besondere Daten prüfen“ läuft in der EU zuerst. Findet sie etwas (`art9_verdacht`), geht die Mail nicht an das Modell außerhalb der EU.

## Freigabe durch einen Menschen

Der Pförtner kennt auch hier keine einzelne Fähigkeit. Er liest nur `wirkt_nach_aussen` und `risiko` aus dem Beipackzettel.

- Sagt er beim Ausführen **wartet**, legt er eine **Freigabe** an und nennt ihre Nummer (`freigabe_id`). Sie gehört zu genau einem Schritt: Aufgabe, Person, Fähigkeit, Version, Bereich, Ziel und die Fingerabdrücke aller Eingaben. Gespeichert werden nur Fingerabdrücke, nie die Daten.
- Eine Person mit der Rolle Admin oder Leitend aus derselben Firma bestätigt oder lehnt ab. Die Entscheidung steht und lässt sich nicht ändern.
- Nach der Bestätigung läuft **genau dieser Schritt mit genau diesen Daten einmal**. Ein anderer Text, ein anderer Bereich, eine andere Aufgabe oder eine andere Person passt nicht und wartet neu. Danach ist die Freigabe verbraucht.
- Fristen: Eine offene Freigabe kann 7 Tage lang entschieden werden, eine Bestätigung gilt 24 Stunden.
- Die Bestätigung ersetzt keine andere Prüfung. Beim Ausführen läuft die ganze Reihe 1 bis 5 noch einmal: Ist die Erlaubnis inzwischen widerrufen oder die Aufgabe gestoppt, bleibt es bei abgelehnt.
- Anfragen, Bestätigen, Ablehnen und Verbrauchen stehen im Audit, jeweils mit der Nummer der Freigabe.
- Eine reine Prüfanfrage (`/v1/pruefen`) legt keine Freigabe an, weil sie die echten Eingaben nicht kennt.

Über HTTP:

- `POST /v1/freigaben/offen` (`{"firma", "person"}`): was in der Firma auf einen Menschen wartet.
- `POST /v1/freigaben/entscheiden` (`{"firma", "freigabe", "person", "bestaetigt"}`).

Beide Wege nimmt der Pförtner nur von einem Dienst an, bei dem in der Datei der Dienste `"darf_freigeben": true` steht (`go run ./cmd/dienstschluessel ... -freigeben`). Das ist für das Dashboard gedacht, das Menschen selbst anmeldet. Der Orchestrator bekommt das Recht nicht: Mit seinem Schlüssel kann er eine Freigabe weder sehen noch entscheiden, also auch keine Zustimmung vortäuschen.

Grenze: Der Pförtner glaubt dem Dashboard, welche Person angemeldet ist, wie bei allen anderen Anfragen. Ob die Person leitend ist, prüft er selbst. Dieselbe Person darf einen Schritt anfragen und bestätigen, sonst könnte eine Firma mit nur einer leitenden Person nichts freigeben.

## Wer anfragen darf

Jede Anfrage außer `GET /gesund` braucht den Schlüssel eines bekannten Dienstes, als Kopfzeile `Authorization: Bearer <schlüssel>`. Jeder Dienst (Dashboard, Ablauf) hat seinen eigenen.

- Der Pförtner kennt nur den SHA-256 jedes Schlüssels, aus der Datei `-dienste`. Ohne diese Datei startet er nicht. Ein fehlerhafter Eintrag verhindert den Start.
- Zu jedem Dienst steht dort, für welche Firmen er anfragen darf. Fragt er für eine andere Firma, lehnt der Pförtner ab (403).
- Ohne oder mit falschem Schlüssel: 401, egal welcher Weg. Der Schlüssel steht in keinem Log, nur der Name des Dienstes.
- Das ist eine bewusste, kleine Ausnahme im Pförtner-Code (von Galip freigegeben). Regel A gilt weiter: Für eine neue Fähigkeit ändert sich am Pförtner nichts.

Schlüssel anlegen (je Dienst einmal, ein neuer Aufruf ersetzt den alten Schlüssel):

```sh
go run ./cmd/dienstschluessel -name dashboard -firmen testfirma-mueller \
  -dienste /ausserhalb/des/repos/dienste.json -ausgabe /ausserhalb/des/repos/dashboard.schluessel
```

Der Schlüssel steht danach nur in der Datei `-ausgabe` (nur für den Besitzer lesbar). Der Dienst findet sie über `BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI`.

## Ausführen

`POST /v1/ausfuehren` prüft zuerst wie oben. Nur bei **erlaubt** geht es weiter:

1. Eingaben dürfen nur die sein, die der Beipackzettel nennt. `bereich`, `zugang` und `ziel` setzt allein der Pförtner.
2. Nennt der Beipackzettel einen `zugang` (ab Fassung 2), holt der Pförtner ihn aus dem Tresor der eigenen Firma, passend zum geprüften Bereich.
3. Er ruft das Modul der Fähigkeit über MCP auf, mit Zeitlimit. Welches Modul wo läuft, steht in der Modul-Liste (`-module`).
4. Steht ein geheimer Wert aus dem Zugang im Ergebnis, wird es zurückgehalten. Fehlermeldungen werden geschwärzt.
5. Das Audit bekommt nur, was ausgeführt wurde und wie groß das Ergebnis war, nie den Inhalt.

## Datenbank und Mandantentrennung

Ohne Datenbank hält der Pförtner alles im Arbeitsspeicher (zum Ausprobieren). Mit `BOTICSTUDIO_DATENBANK` speichert er in PostgreSQL. Die Trennung der Firmen erzwingt die Datenbank selbst, nicht nur der Go-Code:

- Jede Tabelle hat Row-Level Security mit `FORCE`. Sichtbar und schreibbar ist nur, was zur Firma des laufenden Vorgangs gehört. Ist keine Firma gesetzt, ist nichts sichtbar.
- Der Pförtner meldet sich als `boticstudio_pfoertner` an: kein Superuser, darf RLS nicht umgehen, ist nicht Eigentümer der Tabellen, darf nichts löschen.
- Der Orchestrator (`orchestrator/`) hat eine eigene Rolle `boticstudio_ablauf`. Sie darf nur Ergebnisse in `arbeit.einordnungen` und ihre eigenen, verschlüsselten Ablaufstände (`arbeit.ablaufstaende`, `arbeit.ablaufschritte`) schreiben, ebenfalls mit RLS je Firma, und kommt weder an Erlaubnisse noch an das Audit noch an die Vorprüfungen.
- Das Audit lässt sich nur anhängen. Ändern, Löschen und Leeren blockiert ein Trigger, auch für den Eigentümer.
- Die Tests in `internal/datenbank` beweisen das direkt in der Datenbank: Abfragen ohne Filter sehen nur die eigene Firma, Schreiben in eine fremde Firma scheitert, jede Tabelle hat erzwungene Trennung.

Zugangsdaten zur Datenbank kommen nur aus Umgebungsvariablen, nie aus Startparametern (die wären in der Prozessliste sichtbar). Fehlermeldungen enthalten nie das Passwort, auch nicht bei einer kaputten Verbindungsangabe.

| Variable | Wofür |
| --- | --- |
| `BOTICSTUDIO_DATENBANK` | Verbindung des Pförtners (Rolle `boticstudio_pfoertner`) |
| `BOTICSTUDIO_DATENBANK_EIGENTUEMER` | Nur für `cmd/migrieren`, Rolle, der die Tabellen gehören |
| `BOTICSTUDIO_TEST_DATENBANK` | Nur für Tests. Fehlt sie, werden die PostgreSQL-Tests übersprungen (in CI nicht erlaubt). |

Einrichten (lokal, einmal):

```sh
# als Datenbank-Admin: Eigentümer anlegen (kein Superuser, sonst gilt RLS nicht)
psql -c "CREATE ROLE boticstudio_eigentuemer LOGIN CREATEDB CREATEROLE PASSWORD '...'"
psql -c "CREATE DATABASE boticstudio OWNER boticstudio_eigentuemer"

# Schema anlegen bzw. aktualisieren
BOTICSTUDIO_DATENBANK_EIGENTUEMER=postgres://boticstudio_eigentuemer:...@localhost/boticstudio go run ./cmd/migrieren

# dem Pförtner das Anmelden erlauben
psql -c "ALTER ROLE boticstudio_pfoertner LOGIN PASSWORD '...'"
psql -c "ALTER ROLE boticstudio_ablauf LOGIN PASSWORD '...'"
```

## Starten und testen

```sh
cd kernel
go test -race ./...
# mit PostgreSQL-Tests:
BOTICSTUDIO_TEST_DATENBANK=postgres://boticstudio_eigentuemer:...@localhost/boticstudio_test go test -race ./...

BOTICSTUDIO_DATENBANK=postgres://boticstudio_pfoertner:...@localhost/boticstudio \
go run ./cmd/pfoertner -katalog ../katalog -testdaten ../testdaten/testfirma.json \
  -module ../testdaten/module.json -tresor ../testdaten/tresor.json -dienste /ausserhalb/des/repos/dienste.json
```

Ohne `BOTICSTUDIO_DATENBANK` läuft alles im Arbeitsspeicher. Testdaten werden nur geladen, wenn die Firma noch nicht in der Datenbank steht.

`tresor.json` legst du nur lokal an, nach dem Muster `testdaten/tresor.beispiel.json`. Die Datei wird von Git ignoriert.

Beispiel:

```sh
curl -X POST localhost:8470/v1/pruefen -H "Authorization: Bearer $(cat /ausserhalb/des/repos/ablauf.schluessel)" -d '{"firma":"testfirma-mueller","person":"ben","aufgabe":"a1","faehigkeit":"mail_lesen","version":"1.0.0","bereich":"postfach/info@mueller-test.de/INBOX"}'
```

## Noch nicht dabei

- Der echte Tresor (STACKIT Secrets Manager). Bis dahin kommen Zugangsdaten aus einer lokalen Datei.
- Verschlüsselte Verbindung (HTTPS) zwischen Diensten und Pförtner, kommt mit dem Hosting. Bis dahin hört der Pförtner nur auf `127.0.0.1`. Erlaubnisse vergeben geht bewusst nicht über HTTP.
- Der Name des Dienstes steht im Log, noch nicht im Audit.
- Eine Oberfläche für Freigaben (kommt ins Dashboard) und eine Fähigkeit, die wirklich nach außen wirkt. Bisher ist die Freigabe nur mit Test-Fähigkeiten belegt.
- Vier-Augen-Freigabe (zwei verschiedene Personen) für besonders riskante Schritte.
