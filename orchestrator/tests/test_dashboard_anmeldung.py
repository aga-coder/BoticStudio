import http.client
import json
from functools import partial

import pytest

from ablauf.dashboard import COOKIE, Dashboard
from ablauf.konten import Anmeldung, ListenKonten, hashe_passwort
from ablauf.speicher import ListenSpeicher
from test_dashboard import KNOPF, oberflaeche, starte  # noqa: F401
from test_einordnen import EINST, FalscherPfoertner

SCHNELL = partial(hashe_passwort, n=2**10)
PASSWORT = "ein-langes-passwort"


@pytest.fixture
def dienst(oberflaeche):
    pf, sp = FalscherPfoertner(), ListenSpeicher()
    anm = Anmeldung(ListenKonten(), hashen=SCHNELL)
    anm.lege_konto_an(EINST.firma, "chefin", "Chefin@F.de", PASSWORT)
    anm.lege_konto_an("andere", "fremd", "fremd@andere.de", PASSWORT)
    db = Dashboard(pf, sp, EINST, anm)
    server, port = starte(db, oberflaeche)
    yield db, port, pf
    server.shutdown()
    server.server_close()


def rufe(port, methode, pfad, kopf=None, koerper=None):
    v = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    k = {"Host": f"127.0.0.1:{port}", **(kopf or {})}
    daten = json.dumps(koerper).encode() if isinstance(koerper, dict) else koerper
    if methode == "POST" and daten is None:
        daten = b"{}"
    v.request(methode, pfad, body=daten, headers=k)
    a = v.getresponse()
    inhalt = a.read()
    v.close()
    return a.status, a.getheader("Set-Cookie") or "", inhalt


def melde_an(port, email="chefin@f.de", passwort=PASSWORT):
    status, keks, inhalt = rufe(port, "POST", "/api/anmelden", KNOPF, {"email": email, "passwort": passwort})
    return status, keks, json.loads(inhalt)


def mit(keks):
    return {**KNOPF, "Cookie": keks.split(";")[0]}


def test_ohne_anmeldung_nur_ich(dienst):
    _, port, pf = dienst
    status, _, inhalt = rufe(port, "GET", "/api/ich")
    assert status == 200 and json.loads(inhalt) == {"anmeldung_noetig": True, "angemeldet": False, "person": None, "firma": None}
    for methode, pfad in (("GET", "/api/zustand"), ("GET", "/api/einordnungen"), ("GET", "/api/koepfe"), ("POST", "/api/einordnen")):
        status, _, inhalt = rufe(port, methode, pfad, KNOPF)
        assert status == 401 and json.loads(inhalt)["anmelden"] is True, pfad
    assert pf.aufrufe == []


def test_anmelden_setzt_sicheren_cookie(dienst):
    _, port, _ = dienst
    status, keks, antwort = melde_an(port, email="  CHEFIN@f.de ")
    assert status == 200 and antwort["person"] == "chefin"
    assert keks.startswith(COOKIE + "=") and "HttpOnly" in keks and "SameSite=Strict" in keks and "Path=/" in keks
    assert "Max-Age=28800" in keks
    status, _, inhalt = rufe(port, "GET", "/api/ich", mit(keks))
    assert json.loads(inhalt)["angemeldet"] is True


def test_anmelden_braucht_die_kopfzeile(dienst):
    _, port, _ = dienst
    status, keks, _ = rufe(port, "POST", "/api/anmelden", {"Content-Type": "application/json"}, {"email": "chefin@f.de", "passwort": PASSWORT})
    assert status == 403 and keks == ""


def test_falsches_passwort_und_unbekannte_adresse_gleich(dienst):
    _, port, _ = dienst
    a = melde_an(port, passwort="falsch-falsch-falsch")
    b = melde_an(port, email="niemand@f.de")
    assert a[0] == b[0] == 401 and a[2] == b[2] and a[1] == b[1] == ""
    assert PASSWORT not in json.dumps(a[2])


def test_person_der_sitzung_geht_an_den_pfoertner(dienst):
    db, port, pf = dienst
    _, keks, _ = melde_an(port)
    status, _, _ = rufe(port, "GET", "/api/koepfe", mit(keks))
    assert status == 200
    status, _, _ = rufe(port, "POST", "/api/einordnen", mit(keks))
    assert status == 202
    db.warte()
    assert pf.aufrufe and {a[2]["person"] for a in pf.aufrufe} == {"chefin"}
    assert db.zustand()["letzter_lauf"]["gestartet_von"] == "chefin"


def test_abmelden_beendet_die_sitzung(dienst):
    _, port, _ = dienst
    _, keks, _ = melde_an(port)
    status, loeschen, _ = rufe(port, "POST", "/api/abmelden", mit(keks))
    assert status == 200 and "Max-Age=0" in loeschen
    status, _, _ = rufe(port, "GET", "/api/zustand", mit(keks))
    assert status == 401


def test_konto_einer_anderen_firma_kommt_nicht_hinein(dienst):
    db, port, _ = dienst
    status, keks, _ = melde_an(port, email="fremd@andere.de")
    assert status == 403 and keks == ""
    # Auch ein gültiger Cookie der anderen Firma gilt hier nicht.
    _, wert = db.anmeldung.melde_an("fremd@andere.de", PASSWORT)
    status, _, _ = rufe(port, "GET", "/api/zustand", {"Cookie": f"{COOKIE}={wert}"})
    assert status == 401


def test_gefaelschter_cookie(dienst):
    _, port, _ = dienst
    for wert in ("f." + "x" * 43, "kaputt", '"; Path=/'):
        status, _, _ = rufe(port, "GET", "/api/zustand", {"Cookie": f"{COOKIE}={wert}"})
        assert status == 401


def test_zu_grosse_oder_kaputte_anfrage(dienst):
    _, port, _ = dienst
    status, _, _ = rufe(port, "POST", "/api/anmelden", KNOPF, b"x" * 5000)
    assert status == 413
    status, _, _ = rufe(port, "POST", "/api/anmelden", KNOPF, b"[1, 2]")
    assert status == 400


def test_sperre_nach_fuenf_fehlversuchen(dienst):
    _, port, _ = dienst
    for _ in range(5):
        assert melde_an(port, passwort="falsch-falsch-falsch")[0] == 401
    assert melde_an(port)[0] == 401
