# Fähigkeit „Mail lesen“

Holt die Mails der letzten Tage aus einem Postfach-Ordner per IMAP. Im Postfach ändert sich nichts: Der Ordner wird nur zum Lesen geöffnet, und Mails werden so geholt, dass sie ungelesen bleiben.

- Beipackzettel: [`katalog/mail_lesen.json`](../../katalog/mail_lesen.json)
- Aufgerufen wird sie nur vom Pförtner über MCP. Er setzt `bereich` (welches Postfach, welcher Ordner) und `zugang` (aus dem Tresor). Der Orchestrator gibt nur `seit_tagen` (höchstens 31) und `hoechstens` (höchstens 100) mit.
- Fehlermeldungen sind feste, eigene Texte. Zugangsdaten stehen nie darin.

## Starten und testen

```sh
python -m venv .venv && . .venv/bin/activate
pip install -r requirements-dev.txt
python -m pytest -q
python -m mail_lesen.server --port 8481
```

Microsoft- und Google-Anmeldung (OAuth) kommen in einer späteren Stufe dazu. Dafür braucht BoticStudio eigene App-Registrierungen bei Microsoft und Google.
