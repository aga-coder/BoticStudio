# Fähigkeit „Mail einordnen“

Legt Betreff und Text einer Mail mit den Kategorien der Firma einem KI-Modell vor und bekommt Kategorie, kurze Begründung und „unsicher“ zurück.

- Beipackzettel: [`katalog/mail_einordnen.json`](../../katalog/mail_einordnen.json). Datenausgang `ki_modell`, und `"braucht_vorpruefung": ["mail"]`.
- Der Pförtner ruft dieses Modul nur, wenn genau diese Mail vorher von „Besondere Daten prüfen“ geprüft wurde und ihr Ziel erlaubt ist. Bei `art9_verdacht` geht sie an kein Ziel außerhalb der EU.
- Das Ziel setzt der Pförtner (`ziel`), nicht der Orchestrator. Das Modul kennt nur Ziele aus seiner festen Liste (zurzeit `openrouter.ai`).
- Das Modell wählt der Orchestrator (`modell`), nicht der Pförtner.
- Gesendet werden nur Betreff und Text (auf 8.000 Zeichen gekürzt), **kein Absender**. Bei OpenRouter werden Anbieter ausgeschlossen, die Eingaben speichern oder zum Training nutzen (`data_collection: deny`).
- Nennt das Modell eine Kategorie, die es nicht gibt, oder ist die Antwort unlesbar, ist das Ergebnis `unsicher` ohne Kategorie.
- Der Schlüssel steht in `OPENROUTER_API_KEY` in der Umgebung des Moduls. Er steht nie in einem Ergebnis oder einer Fehlermeldung. Später kommt er aus dem Tresor.

## Starten und testen

```sh
python -m venv .venv && . .venv/bin/activate
pip install -r requirements-dev.txt
python -m pytest -q
OPENROUTER_API_KEY=... python -m mail_einordnen.server --port 8483
```

Für Probeläufe ohne echtes Modell kann `MAIL_EINORDNEN_PROBE_URLS` ein Ziel auf einen Ersatz unter `127.0.0.1` umleiten, z. B. `{"openrouter.ai": "http://127.0.0.1:8599/v1"}`. Andere Umleitungen lehnt das Modul ab.
