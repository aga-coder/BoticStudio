# Fähigkeiten „Mail lesen“ und „Kopfzeilen lesen“

Holt die Mails der letzten Tage aus einem Postfach-Ordner per IMAP. Im Postfach ändert sich nichts: Der Ordner wird nur zum Lesen geöffnet, und Mails werden so geholt, dass sie ungelesen bleiben.

- Beipackzettel: [`katalog/mail_lesen.json`](../../katalog/mail_lesen.json)
- Aufgerufen wird sie nur vom Pförtner über MCP. Er setzt `bereich` (welches Postfach, welcher Ordner) und `zugang` (aus dem Tresor). Der Orchestrator gibt nur `seit_tagen` (höchstens 31) und `hoechstens` (höchstens 100) mit.
- Fehlermeldungen sind feste, eigene Texte. Zugangsdaten stehen nie darin.

## „Kopfzeilen lesen“ (`mail_kopf_lesen`)

Eine eigene Fähigkeit mit eigenem Beipackzettel ([`katalog/mail_kopf_lesen.json`](../../katalog/mail_kopf_lesen.json)) und eigenem Prozess (Port 8484). Sie teilt sich nur den IMAP-Code mit „Mail lesen“.

- Holt nur Absender, Betreff, Datum und Message-ID (`BODY.PEEK[HEADER.FIELDS …]`). Den Text lädt der Mailserver gar nicht erst herunter, auch nicht die Empfänger.
- Das Dashboard zeigt damit live, um welche Mail es geht. Gespeichert wird davon nichts.
- Sie braucht dasselbe Recht wie „Mail lesen“ (`mail.lesen`) für denselben Bereich. Jeder Abruf steht im Audit des Pförtners.
- `seit_tagen` höchstens 31, `hoechstens` höchstens 500.
- Für sie musste am Pförtner nichts geändert werden: Beipackzettel in den Katalog, Adresse in die Modulliste, fertig.

## Starten und testen

```sh
python -m venv .venv && . .venv/bin/activate
pip install -r requirements-dev.txt
python -m pytest -q
python -m mail_lesen.server --port 8481
python -m mail_lesen.kopf_server --port 8484
```

Microsoft- und Google-Anmeldung (OAuth) kommen in einer späteren Stufe dazu. Dafür braucht BoticStudio eigene App-Registrierungen bei Microsoft und Google.
