import json
import urllib.error

import pytest

from mail_einordnen import einordnen
from mail_einordnen.einordnen import Fehler, ordne_ein

SCHLUESSEL = "sk-test-geheim-123"
ZIEL = {"art": "ki_modell", "adresse": "openrouter.ai"}
KATEGORIEN = [{"name": "Anfrage", "beschreibung": "will etwas wissen oder kaufen"}, "Beschwerde", "Sonstiges"]
MAIL = {"betreff": "Lieferung", "text": "Die Ware kam kaputt an.", "von": "kunde@example.org", "uid": "7"}


@pytest.fixture(autouse=True)
def schluessel(monkeypatch):
    monkeypatch.setenv("OPENROUTER_API_KEY", SCHLUESSEL)
    monkeypatch.delenv(einordnen.UMLEITUNG_VARIABLE, raising=False)


def modell_antwortet(inhalt):
    gesendet = {}

    def senden(url, kopf, daten, zeitlimit):
        gesendet.update(url=url, kopf=kopf, koerper=json.loads(daten))
        return json.dumps({"choices": [{"message": {"content": inhalt}}]}).encode()

    return senden, gesendet


def test_ordnet_ein_und_sendet_nur_noetiges():
    senden, gesendet = modell_antwortet('{"kategorie": "beschwerde", "begruendung": "Ware beschädigt.", "unsicher": false}')
    ergebnis = ordne_ein(MAIL, KATEGORIEN, ZIEL, "anbieter/modell", senden=senden)
    assert ergebnis == {"kategorie": "Beschwerde", "begruendung": "Ware beschädigt.", "unsicher": False, "modell": "anbieter/modell"}
    assert gesendet["url"] == "https://openrouter.ai/api/v1/chat/completions"
    text = json.dumps(gesendet["koerper"])
    assert "kaputt" in text and "kunde@example.org" not in text  # kein Absender
    assert gesendet["koerper"]["provider"] == {"data_collection": "deny"}
    assert SCHLUESSEL not in text  # Schlüssel nur im Kopf


def test_unbekannte_kategorie_ist_unsicher():
    senden, _ = modell_antwortet('{"kategorie": "Spam", "begruendung": "x", "unsicher": false}')
    ergebnis = ordne_ein(MAIL, KATEGORIEN, ZIEL, "m", senden=senden)
    assert ergebnis["kategorie"] is None and ergebnis["unsicher"] is True


def test_unlesbare_antwort_ist_unsicher():
    senden, _ = modell_antwortet("kein json")
    assert ordne_ein(MAIL, KATEGORIEN, ZIEL, "m", senden=senden)["unsicher"] is True


def test_nur_bekannte_ziele():
    senden, gesendet = modell_antwortet("{}")
    with pytest.raises(Fehler, match="kennt das Modul nicht"):
        ordne_ein(MAIL, KATEGORIEN, {"art": "ki_modell", "adresse": "boese.example"}, "m", senden=senden)
    assert gesendet == {}


def test_umleitung_nur_lokal(monkeypatch):
    monkeypatch.setenv(einordnen.UMLEITUNG_VARIABLE, json.dumps({"openrouter.ai": "https://boese.example/v1"}))
    with pytest.raises(Fehler, match="127.0.0.1"):
        ordne_ein(MAIL, KATEGORIEN, ZIEL, "m", senden=modell_antwortet("{}")[0])


def test_fehler_ohne_schluessel(monkeypatch):
    def senden(url, kopf, daten, zeitlimit):
        raise urllib.error.HTTPError(url, 401, f"bad key {SCHLUESSEL}", {}, None)

    with pytest.raises(Fehler) as f:
        ordne_ein(MAIL, KATEGORIEN, ZIEL, "m", senden=senden)
    assert "401" in str(f.value) and SCHLUESSEL not in str(f.value)

    monkeypatch.delenv("OPENROUTER_API_KEY")
    with pytest.raises(Fehler, match="kein Schlüssel"):
        ordne_ein(MAIL, KATEGORIEN, ZIEL, "m", senden=senden)


def test_text_wird_gekuerzt():
    senden, gesendet = modell_antwortet('{"kategorie": "Anfrage"}')
    ordne_ein({"betreff": "x", "text": "a" * 50_000}, KATEGORIEN, ZIEL, "m", senden=senden)
    assert len(gesendet["koerper"]["messages"][1]["content"]) < einordnen.MAX_TEXT + 100


@pytest.mark.parametrize("kategorien", [[], "Anfrage", [{"beschreibung": "ohne name"}], ["x"] * 21])
def test_ungueltige_kategorien(kategorien):
    with pytest.raises(Fehler):
        ordne_ein(MAIL, kategorien, ZIEL, "m", senden=modell_antwortet("{}")[0])
