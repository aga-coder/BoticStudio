# Dashboard (erste Stufe)

Die Oberfläche für den ersten Ablauf (TypeScript, React, Vite): Zahlen je Kategorie, die eingeordneten Mails und der Knopf „Jetzt einordnen“. Mails mit Art.-9-Verdacht stehen unter „Von Hand prüfen“.

Die Daten kommen vom Dienst `python -m ablauf.dashboard` (siehe `orchestrator/README.md`). Er liefert nur Arbeitsdaten, also einen Verweis auf jede Mail und nie ihren Inhalt. Deshalb zeigt diese Stufe noch keinen Betreff und keinen Absender.

```sh
npm ci
npm test          # Tests
npm run build     # Typprüfung und Bau nach dist/, den der Dienst ausliefert
npm run dev       # zum Entwickeln, leitet /api an 127.0.0.1:8490 weiter
```

Noch nicht dabei (spätere Stufen): Anmeldung, frei anordenbare Bausteine, Betreff und Absender (live aus dem Postfach über den Pförtner), Einordnung korrigieren.
