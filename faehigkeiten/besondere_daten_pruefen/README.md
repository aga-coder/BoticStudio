# Fähigkeit „Besondere Daten prüfen“

Erste Filterstufe für besondere Daten nach Art. 9 DSGVO (Gesundheit, Religion, politische Meinung, Gewerkschaft, ethnische Herkunft, Sexualleben, genetische und biometrische Daten).

- Beipackzettel: [`katalog/besondere_daten_pruefen.json`](../../katalog/besondere_daten_pruefen.json). Er erklärt mit `"prueft": ["mail"]`, dass diese Fähigkeit prüft. Darum merkt sich der Pförtner ihr Ergebnis zu genau dieser Mail.
- Läuft innerhalb von BoticStudio (EU). Kein Datenausgang, kein Recht nötig.
- Findet die Wortliste etwas, kommt die Markierung `art9_verdacht` zurück. Der Pförtner lässt die Mail dann an kein Ziel außerhalb der EU.
- Das Ergebnis nennt nur Bereiche (z. B. `gesundheit`), nie die gefundenen Wörter oder Textstellen.

## Grenze

Die Wortliste ist **keine Garantie**. Was sie nicht erkennt, geht an das Modell. Sie markiert lieber einmal zu viel als einmal zu wenig. Spätestens ab dem ersten echten Kunden steht diese Grenze in der Dokumentation für den Kunden.

## Starten und testen

```sh
python -m venv .venv && . .venv/bin/activate
pip install -r requirements-dev.txt
python -m pytest -q
python -m besondere_daten_pruefen.server --port 8482
```
