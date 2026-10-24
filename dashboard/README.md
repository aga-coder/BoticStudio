# Dashboard (erste Stufe)

Die Oberfläche für den ersten Ablauf (TypeScript, React, Vite): Anmeldung mit E-Mail und Passwort, Zahlen je Kategorie, die eingeordneten Mails und der Knopf „Jetzt einordnen“. Mails mit Art.-9-Verdacht stehen unter „Von Hand prüfen“.

Die Daten kommen vom Dienst `python -m ablauf.dashboard` (siehe `orchestrator/README.md`). Gespeichert ist nur ein Verweis auf jede Mail. Betreff und Absender holt der Dienst bei jedem Laden live über den Pförtner und speichert sie nicht. Den Text einer Mail zeigt das Dashboard nie.

```sh
npm ci
npm test          # Tests
npm run build     # Typprüfung und Bau nach dist/, den der Dienst ausliefert
npm run dev       # zum Entwickeln, leitet /api an 127.0.0.1:8490 weiter
```

Noch nicht dabei (spätere Stufen): Registrieren mit Bestätigungslink, Passwort vergessen, frei anordenbare Bausteine, Einordnung korrigieren.
