# Fähigkeit „Systemmail senden“

Verschickt Mails von BoticStudio selbst, zuerst den Bestätigungslink bei der Registrierung. Sie hat nichts mit den Postfächern der Kunden zu tun: eigener Bereich (`systempost/…`), eigenes Recht (`systemmail.senden`), eigener Zugang im Tresor.

- Beipackzettel: [`katalog/systemmail_senden.json`](../../katalog/systemmail_senden.json)
- Aufgerufen wird sie nur vom Pförtner über MCP. Er setzt `bereich` (Absender, z. B. `systempost/konto@boticstudio.de`), `zugang` (aus dem Tresor) und `ziel` (der geprüfte Mailserver).
- Für sie musste am Pförtner nichts geändert werden (Regel A).

## Schutzregeln

1. **Nur feste Vorlagen.** Der Aufrufer nennt `vorlage`, `empfaenger` und `werte` für die Lücken. Freien Text kann er nicht senden. Heute gibt es eine Vorlage: `konto_bestaetigen` mit der Lücke `link`.
2. **Links nur zu BoticStudio.** Ein Link muss unter der Adresse liegen, die beim Start festgelegt wird (`--link-basis` oder `BOTICSTUDIO_LINK_BASIS`). Ohne diese Angabe wird nichts gesendet.
3. **Genau ein Empfänger.** Listen, Namen und Zeilenumbrüche in der Adresse werden abgelehnt.
4. **Nur verschlüsselt und nur an das geprüfte Ziel.** Port 465 von Anfang an verschlüsselt, sonst STARTTLS, immer mit Prüfung des Zertifikats (Zertifikate des Betriebssystems). Der Server im Zugang muss das Ziel sein, das der Pförtner geprüft hat.
5. **Feste Fehlertexte.** Zugangsdaten, Empfänger und Antworten des Mailservers stehen nie in einer Fehlermeldung.

Im Beipackzettel steht ehrlich `wirkt_nach_aussen: true`. Der Pförtner lässt darum jede Mail auf die Bestätigung einer leitenden Person warten. Für die echte Registrierung (niemand kann jede Bestätigungsmail einzeln freigeben) braucht es eine Entscheidung, wie eine dauerhafte Vorab-Freigabe aussehen soll. Das ist nicht Teil dieser Stufe.

## Versandweg

Der Weg steht im Tresor-Eintrag (`"weg"`, Standard `smtp`). Ein Versanddienst kommt später als weiterer Weg dazu, ohne Änderung für Aufrufer oder Pförtner.

Tresor-Eintrag (nur lokal, nie ins Repo), Muster in `testdaten/tresor.beispiel.json`:

```json
{ "boticstudio-plattform": { "systempost/konto@boticstudio.de": {
    "weg": "smtp", "server": "smtp.example.org", "port": 587,
    "benutzer": "konto@boticstudio.de", "passwort": "…" } } }
```

Die Firma braucht dazu das Ziel `{"art": "mailserver", "adresse": "<server>", "region": "eu"}` und eine Erlaubnis für `systemmail.senden` im Bereich `systempost/<absender>`.

## Starten und testen

```sh
python -m venv .venv && . .venv/bin/activate
pip install -r requirements-dev.txt
python -m pytest -q
python -m systemmail_senden.server --port 8485 --link-basis https://app.boticstudio.de/
```

Die Tests sprechen auch echtes SMTP mit einem Übungs-Mailserver auf dem eigenen Rechner (verschlüsselt, mit Anmeldung, selbst ausgestelltes Zertifikat nur für den Test).

## Grenzen

- Noch nicht mit einem echten Postfach gelaufen.
- Keine eigene Mengenbegrenzung. Die Registrierung muss selbst begrenzen, wie oft sie Mails anfordert.
- Nur Text-Mails, nur Deutsch.
