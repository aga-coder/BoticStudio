import http.client
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from ablauf.dashboard import Dashboard, baue_handler
from ablauf.pfoertner import PfoertnerFehler
from ablauf.speicher import ListenSpeicher
from test_einordnen import EINST, MAILS, FalscherPfoertner


@pytest.fixture
def oberflaeche(tmp_path):
    (tmp_path / "index.html").write_text("<!doctype html><title>BoticStudio</title>", encoding="utf-8")
    (tmp_path / "assets").mkdir()
    (tmp_path / "assets" / "app.js").write_text("console.log(1)", encoding="utf-8")
    (tmp_path.parent / "geheim.txt").write_text("nicht ausliefern", encoding="utf-8")
    return tmp_path


def starte(db, oberflaeche):
    server = ThreadingHTTPServer(("127.0.0.1", 0), BaseHTTPRequestHandler)
    port = server.server_address[1]
    server.RequestHandlerClass = baue_handler(db, port, oberflaeche)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server, port


@pytest.fixture
def dienst(oberflaeche):
    pf, sp = FalscherPfoertner(), ListenSpeicher()
    db = Dashboard(pf, sp, EINST)
    server, port = starte(db, oberflaeche)
    yield db, port, pf
    server.shutdown()
    server.server_close()


def anfrage(port, methode, pfad, kopf=None, host=None):
    v = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    k = {"Host": host or f"127.0.0.1:{port}", **(kopf or {})}
    v.request(methode, pfad, body=b"{}" if methode == "POST" else None, headers=k)
    a = v.getresponse()
    koerper = a.read()
    v.close()
    return a.status, dict(a.getheaders()), koerper


KNOPF = {"X-BoticStudio-Aktion": "1", "Content-Type": "application/json"}


def test_knopf_ordnet_ein_und_liste_zeigt_ergebnis(dienst):
    db, port, _ = dienst
    status, _, koerper = anfrage(port, "POST", "/api/einordnen", KNOPF)
    assert status == 202 and json.loads(koerper)["firma"] == EINST.firma
    db.warte()
    status, _, koerper = anfrage(port, "GET", "/api/zustand")
    zustand = json.loads(koerper)
    assert zustand["laeuft"] is False and zustand["kategorien"] == ["Anfrage", "Beschwerde", "Sonstiges"]
    assert zustand["letzter_lauf"]["bericht"]["eingeordnet"] == 2 and zustand["letzter_lauf"]["abbruch"] == ""

    status, _, koerper = anfrage(port, "GET", "/api/einordnungen")
    zeilen = {z["uid"]: z for z in json.loads(koerper)["einordnungen"]}
    assert zeilen["1"]["kategorie"] == "Anfrage" and zeilen["1"]["nachweis"] == 5
    assert zeilen["2"]["stand"] == "nicht_eingeordnet" and zeilen["2"]["art9_verdacht"] is True
    assert zeilen["3"]["kategorie"] == "Beschwerde"


def test_antworten_ohne_mailinhalt(dienst):
    db, port, _ = dienst
    anfrage(port, "POST", "/api/einordnen", KNOPF)
    db.warte()
    alles = b"".join(anfrage(port, "GET", p)[2] for p in ("/api/zustand", "/api/einordnungen"))
    for mail in MAILS:
        for feld in ("von", "betreff", "text", "message_id"):
            assert mail[feld].encode() not in alles, feld


@pytest.mark.parametrize(
    "kopf",
    [
        {},  # ohne eigene Kopfzeile, so wie ein Formular einer fremden Seite
        {"X-BoticStudio-Aktion": "1", "Origin": "https://boese.example"},
    ],
)
def test_knopf_nur_aus_dem_dashboard(dienst, kopf):
    db, port, pf = dienst
    status, _, _ = anfrage(port, "POST", "/api/einordnen", kopf)
    assert status == 403
    assert pf.aufrufe == [] and db.zustand()["letzter_lauf"] is None


def test_fremder_host_abgewiesen(dienst):
    # Schutz gegen DNS-Rebinding: Eine fremde Seite, deren Name auf 127.0.0.1 zeigt, kommt nicht durch.
    _, port, pf = dienst
    assert anfrage(port, "GET", "/api/einordnungen", host="boese.example")[0] == 403
    assert anfrage(port, "POST", "/api/einordnen", KNOPF, host=f"boese.example:{port}")[0] == 403
    assert pf.aufrufe == []


def test_nur_ein_lauf_gleichzeitig(oberflaeche):
    freigabe = threading.Event()

    class LangsamerPfoertner(FalscherPfoertner):
        def fuehre(self, anfrage, eingaben):
            freigabe.wait(10)
            return super().fuehre(anfrage, eingaben)

    db = Dashboard(LangsamerPfoertner(), ListenSpeicher(), EINST)
    server, port = starte(db, oberflaeche)
    try:
        assert anfrage(port, "POST", "/api/einordnen", KNOPF)[0] == 202
        assert anfrage(port, "POST", "/api/einordnen", KNOPF)[0] == 409
        freigabe.set()
        db.warte()
        assert anfrage(port, "POST", "/api/einordnen", KNOPF)[0] == 202
        db.warte()
    finally:
        server.shutdown()
        server.server_close()


def test_abbruch_ohne_interna(oberflaeche):
    class KaputterPfoertner:
        def fuehre(self, anfrage, eingaben):
            raise ValueError("geheimer Inhalt aus der Tiefe")

    class FernerPfoertner:
        def fuehre(self, anfrage, eingaben):
            raise PfoertnerFehler("Pförtner nicht erreichbar.")

    for pf, erwartet in ((KaputterPfoertner(), "Interner Fehler beim Einordnen."), (FernerPfoertner(), "Pförtner nicht erreichbar.")):
        db = Dashboard(pf, ListenSpeicher(), EINST)
        assert db.starte()
        db.warte()
        lauf = db.zustand()["letzter_lauf"]
        assert lauf["abbruch"] == erwartet and lauf["ende"] and "geheim" not in json.dumps(lauf)


def test_oberflaeche_und_sicherheitskopf(dienst, oberflaeche):
    _, port, _ = dienst
    status, kopf, koerper = anfrage(port, "GET", "/")
    assert status == 200 and b"BoticStudio" in koerper
    assert "default-src 'self'" in kopf["Content-Security-Policy"] and kopf["X-Frame-Options"] == "DENY"
    status, kopf, _ = anfrage(port, "GET", "/assets/app.js")
    assert status == 200 and "javascript" in kopf["Content-Type"]
    # Nichts außerhalb des Ordners der Oberfläche
    for pfad in ("/../geheim.txt", "/assets/../../geheim.txt", "/%2e%2e/geheim.txt"):
        assert b"nicht ausliefern" not in anfrage(port, "GET", pfad)[2]
    assert anfrage(port, "GET", "/api/gibtsnicht")[0] == 404


def test_koepfe_live_ueber_den_pfoertner(dienst):
    db, port, pf = dienst
    status, _, koerper = anfrage(port, "GET", "/api/koepfe", KNOPF)
    assert status == 200
    antwort = json.loads(koerper)
    assert antwort["fehler"] == "" and antwort["bereich"] == EINST.postfach
    assert antwort["koepfe"]["1"] == {"von": "a@x", "betreff": "Angebot", "datum": "2026-10-21T10:00:00+02:00"}
    # Nur Kopfzeilen: kein Text, keine Message-ID
    for mail in MAILS:
        assert mail["text"].encode() not in koerper and mail["message_id"].encode() not in koerper
    name, eingaben, gesendet = pf.aufrufe[-1]
    assert name == "mail_kopf_lesen" and gesendet["bereich"] == EINST.postfach and gesendet["person"] == EINST.person
    assert gesendet["aufgabe"].startswith("ansehen-")
    # Gespeichert wird davon nichts.
    assert db.speicher.liste(EINST.firma) == []


def test_koepfe_nur_aus_dem_dashboard(dienst):
    _, port, pf = dienst
    assert anfrage(port, "GET", "/api/koepfe")[0] == 403
    assert anfrage(port, "GET", "/api/koepfe", {**KNOPF, "Origin": "https://boese.example"})[0] == 403
    assert anfrage(port, "GET", "/api/koepfe", KNOPF, host="boese.example")[0] == 403
    assert pf.aufrufe == []


def test_koepfe_abgelehnt_zeigt_grund(oberflaeche):
    db = Dashboard(FalscherPfoertner(lesen_erlaubt=False), ListenSpeicher(), EINST)
    antwort = db.koepfe()
    assert antwort["koepfe"] == {} and antwort["fehler"] == "Es fehlt die Erlaubnis."

    class FernerPfoertner:
        def fuehre(self, anfrage, eingaben):
            raise PfoertnerFehler("Pförtner nicht erreichbar.")

    assert Dashboard(FernerPfoertner(), ListenSpeicher(), EINST).koepfe()["fehler"] == "Pförtner nicht erreichbar."
