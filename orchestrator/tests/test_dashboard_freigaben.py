"""Die Liste „Wartet auf Freigabe“ im Dashboard."""

import http.client
import json

import pytest

from ablauf.dashboard import Dashboard
from ablauf.pfoertner import fingerabdruck
from ablauf.speicher import ListenSpeicher
from test_dashboard import KNOPF, oberflaeche, starte  # noqa: F401
from test_einordnen import EINST
from test_orchestrator import FreigabePfoertner, probe

FIRMA = EINST.firma


def sende(port, methode, pfad, wert=None, kopf=None):
    v = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    v.request(methode, pfad, body=json.dumps(wert).encode() if wert is not None else None,
              headers={"Host": f"127.0.0.1:{port}", **(KNOPF if kopf is None else kopf)})
    a = v.getresponse()
    koerper = json.loads(a.read() or b"{}")
    v.close()
    return a.status, koerper


@pytest.fixture
def dienst(oberflaeche):  # noqa: F811
    pf = FreigabePfoertner()
    # Testmodus ohne Anmeldung: Die Person kommt aus den Einstellungen.
    pf.leitende = (EINST.person,)
    db = Dashboard(pf, ListenSpeicher(), EINST, weitere_ablaeufe={"probe": probe(pf)})
    server, port = starte(db, oberflaeche)
    yield db, port, pf
    server.shutdown()
    server.server_close()


def warte_lauf(db, text="hallo <Welt> & Co."):
    return db.orchestrator.starte("probe", FIRMA, "ben", {"text": text})


def test_liste_zeigt_was_ausgefuehrt_wuerde(dienst):
    db, port, pf = dienst
    assert sende(port, "GET", "/api/freigaben") == (200, {"freigaben": [], "fehler": ""})
    lauf = warte_lauf(db)
    status, antwort = sende(port, "GET", "/api/freigaben")
    [f] = antwort["freigaben"]
    assert status == 200 and f["id"] == "f-1" and f["lauf"] == lauf.id
    assert f["faehigkeit"] == "probe_melden" and f["bereich"] == "kanal/probe" and f["angefragt_fuer"] == "ben"
    assert f["anzeige"] == [{"name": "text", "wert": "hallo <Welt> & Co.", "gekuerzt": False, "stimmt": True}]
    assert f["geprueft"] is True
    # Fingerabdrücke und der Stand des Laufs gehen nicht an die Oberfläche.
    assert "daten" not in f and "schritt" not in f


def test_bestaetigen_macht_den_lauf_weiter(dienst):
    db, port, pf = dienst
    warte_lauf(db)
    status, antwort = sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": True})
    assert status == 200 and antwort["freigaben"] == []
    db.warte()
    assert pf.gesendet == ["hallo <Welt> & Co."]
    assert db.zustand()["letzte_fortsetzung"]["stand"] == "fertig"
    assert db.orchestrator.wartende(FIRMA) == []
    # Zweimal entscheiden geht nicht.
    assert sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": True})[0] == 409


def test_ablehnen_sendet_nichts(dienst):
    db, port, pf = dienst
    warte_lauf(db)
    assert sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": False})[0] == 200
    db.warte()
    assert pf.gesendet == [] and pf.freigaben["f-1"]["stand"] == "abgelehnt"
    assert db.zustand()["letzte_fortsetzung"]["stand"] == "fertig"


def test_gezeigtes_muss_zum_pfoertner_passen(dienst):
    """Zeigt der Lauf etwas anderes, als der Pförtner freigeben würde, lässt sich nicht bestätigen."""
    db, port, pf = dienst
    warte_lauf(db, "Guten Tag")
    # Der Pförtner hat einen anderen Text gesehen als der, den der Lauf zeigt.
    pf.freigaben["f-1"]["eingaben"] = {"text": "Überweisen Sie 5000 Euro"}
    [f] = sende(port, "GET", "/api/freigaben")[1]["freigaben"]
    assert f["geprueft"] is False and f["anzeige"][0]["stimmt"] is False
    status, antwort = sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": True})
    assert status == 409 and "belegen" in antwort["fehler"]
    assert pf.freigaben["f-1"]["stand"] == "offen"
    # Ablehnen geht trotzdem.
    assert sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": False})[0] == 200


def test_eingabe_die_nicht_gezeigt_wird_gilt_als_ungeprueft(dienst):
    db, port, pf = dienst
    warte_lauf(db, "Guten Tag")
    pf.freigaben["f-1"]["eingaben"]["anhang"] = "versteckt"
    [f] = sende(port, "GET", "/api/freigaben")[1]["freigaben"]
    assert f["anzeige"][0]["stimmt"] is True and f["geprueft"] is False


def test_freigabe_ohne_bekannten_lauf(dienst):
    """Z. B. nach einem Neustart ohne gespeicherten Stand: sichtbar, aber nur ablehnbar."""
    db, port, pf = dienst
    lauf = warte_lauf(db)
    db.orchestrator._stand(FIRMA).delete_thread(lauf.id)
    [f] = sende(port, "GET", "/api/freigaben")[1]["freigaben"]
    assert f["lauf"] is None and f["anzeige"] == [] and f["geprueft"] is False
    assert sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": True})[0] == 409
    assert sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": False})[0] == 200


def test_nur_leitende_entscheiden(dienst):
    db, port, pf = dienst
    pf.leitende = ("anna",)
    warte_lauf(db)
    status, antwort = sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": True})
    assert status == 409 and "leitende" in antwort["fehler"]
    assert pf.gesendet == [] and pf.freigaben["f-1"]["stand"] == "offen"


@pytest.mark.parametrize("wert", [{}, {"freigabe": "f-1"}, {"freigabe": "f-1", "bestaetigt": "ja"}, {"freigabe": 1, "bestaetigt": True}, {"freigabe": "gibtsnicht", "bestaetigt": True}])
def test_ungueltige_anfragen(dienst, wert):
    db, port, pf = dienst
    warte_lauf(db)
    assert sende(port, "POST", "/api/freigaben/entscheiden", wert)[0] in (400, 409)
    assert pf.freigaben["f-1"]["stand"] == "offen"


def test_entscheiden_nur_aus_dem_dashboard(dienst):
    db, port, pf = dienst
    warte_lauf(db)
    for kopf in ({"Content-Type": "application/json"}, {**KNOPF, "Origin": "https://boese.example"}):
        assert sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": True}, kopf)[0] == 403
    assert pf.freigaben["f-1"]["stand"] == "offen"


def test_pfoertner_ohne_recht_zeigt_fehler(oberflaeche):  # noqa: F811
    from ablauf.pfoertner import PfoertnerFehler

    class OhneRecht(FreigabePfoertner):
        def offene_freigaben(self, firma, person):
            raise PfoertnerFehler("Der Pförtner lässt diesen Dienst keine Freigaben weitergeben.")

    db = Dashboard(OhneRecht(), ListenSpeicher(), EINST)
    server, port = starte(db, oberflaeche)
    try:
        status, antwort = sende(port, "GET", "/api/freigaben")
        assert status == 200 and antwort["freigaben"] == [] and "keine Freigaben" in antwort["fehler"]
        assert sende(port, "POST", "/api/freigaben/entscheiden", {"freigabe": "f-1", "bestaetigt": True})[0] == 503
    finally:
        server.shutdown()
        server.server_close()


def test_fingerabdruck_wie_der_pfoertner():
    """Dieselben Beispiele prüft der Kernel (TestFingerabdruckBeispiele)."""
    from pathlib import Path

    datei = json.loads((Path(__file__).resolve().parents[2] / "testdaten" / "fingerabdruecke.json").read_text(encoding="utf-8"))
    assert len(datei["beispiele"]) >= 5
    for b in datei["beispiele"]:
        assert fingerabdruck(b["text"]) == b["fingerabdruck"], b["text"]
