# Pförtner (Kernel)

Der Pförtner prüft jeden Schritt, den der Orchestrator ausführen möchte, und antwortet mit **erlaubt**, **abgelehnt** oder **wartet**. Er plant nichts und wählt weder Modell noch Fähigkeit.

## Zwei Grundregeln

- **Regel A: Der Pförtner kennt keine einzelne Fähigkeit.** Er vergleicht nur den Beipackzettel mit den Erlaubnissen der Firma. Ein Test durchsucht den Code nach Namen von Fähigkeiten, ein zweiter lädt eine ganz neue Fähigkeit nur über ihren Zettel.
- **Regel B: Der Beipackzettel hat Fassungen.** Neue Felder kommen dazu, alte werden nie gestrichen oder umgedeutet. Fehlt ein Feld, gilt der strengste Wert (`risiko: hoch`, `wirkt_nach_aussen: true`, kein Datenausgang). Eine neuere Fassung, als der Pförtner kennt, lehnt er ab.

## Was er prüft, der Reihe nach

1. Wer fragt: Firma, Person (Rolle kommt aus seinen eigenen Daten, nie aus der Anfrage), Aufgabe.
2. Läuft die Aufgabe noch, ist die Obergrenze an Schritten erreicht?
3. Steht die Fähigkeit in dieser Version im Katalog? Bleibt die Aufgabe bei ihrer Version?
4. Gibt es eine gültige Erlaubnis für genau diesen Bereich (Teil für Teil verglichen)?
5. Verlassen Daten BoticStudio: Erlaubt der Zettel das Ziel, steht es auf der Liste der Firma, liegt eine Einwilligung vor, und sperrt eine Markierung (z. B. `art9_verdacht`) die Region?
6. Wirkt es nach außen oder ist es riskant: dann **wartet**.

Jede Entscheidung geht an den Auditor (nur anhängen, Hash-Kette). Kann er nicht schreiben, wird abgelehnt.

## Ausführen

`POST /v1/ausfuehren` prüft zuerst wie oben. Nur bei **erlaubt** geht es weiter:

1. Eingaben dürfen nur die sein, die der Beipackzettel nennt. `bereich` und `zugang` setzt allein der Pförtner.
2. Nennt der Beipackzettel einen `zugang` (ab Fassung 2), holt der Pförtner ihn aus dem Tresor der eigenen Firma, passend zum geprüften Bereich.
3. Er ruft das Modul der Fähigkeit über MCP auf, mit Zeitlimit. Welches Modul wo läuft, steht in der Modul-Liste (`-module`).
4. Steht ein geheimer Wert aus dem Zugang im Ergebnis, wird es zurückgehalten. Fehlermeldungen werden geschwärzt.
5. Das Audit bekommt nur, was ausgeführt wurde und wie groß das Ergebnis war, nie den Inhalt.

## Starten und testen

```sh
cd kernel
go test -race ./...
go run ./cmd/pfoertner -katalog ../katalog -testdaten ../testdaten/testfirma.json \
  -module ../testdaten/module.json -tresor ../testdaten/tresor.json
```

`tresor.json` legst du nur lokal an, nach dem Muster `testdaten/tresor.beispiel.json`. Die Datei wird von Git ignoriert.

Beispiel:

```sh
curl -X POST localhost:8470/v1/pruefen -d '{"firma":"testfirma-mueller","person":"ben","aufgabe":"a1","faehigkeit":"mail_lesen","version":"1.0.0","bereich":"postfach/info@mueller-test.de/INBOX"}'
```

## Noch nicht dabei

- Speichern in PostgreSQL (Erlaubnisse, Audit) und der echte Tresor (STACKIT Secrets Manager). Bis dahin liegt alles im Arbeitsspeicher.
- Anmeldung der Aufrufer. Bis dahin hört der Pförtner nur auf `127.0.0.1`. Erlaubnisse vergeben geht bewusst nicht über HTTP.
- Wartende Schritte speichern und bestätigen (kommt mit Schritt 2, Antwort senden).
