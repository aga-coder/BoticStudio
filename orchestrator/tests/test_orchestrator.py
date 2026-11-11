"""Der Orchestrator hält bei "wartet" an und macht nach der Entscheidung weiter."""

import json

import pytest
from langgraph.graph import END, START, StateGraph

from ablauf.ablaeufe import MAIL_EINORDNEN, eingabe_aus, mail_einordnen
from ablauf.orchestrator import LaufUnbekannt, Orchestrator, Zustand, schritt
from ablauf.pfoertner import FreigabeAbgewiesen, PfoertnerFehler, fingerabdruck
from ablauf.speicher import ListenSpeicher
from test_einordnen import EINST, FalscherPfoertner

MELDEN = ("probe_melden", "1.0.0")
ZAEHLEN = ("probe_zaehlen", "1.0.0")


class FreigabePfoertner:
    """Verhält sich wie der Pförtner: probe_melden wirkt nach außen und braucht
    eine Freigabe für genau diesen Schritt mit genau diesen Daten, einmal."""

    def __init__(self):
        self.freigaben = {}  # id -> {"schritt", "stand"}
        self.gesendet = []
        self.aufrufe = []
        self.kaputt = False

    def _schritt(self, anfrage, eingaben):
        return json.dumps([anfrage, eingaben], sort_keys=True)

    def entscheide(self, freigabe_id, bestaetigt):
        assert self.freigaben[freigabe_id]["stand"] == "offen"
        self.freigaben[freigabe_id]["stand"] = "bestaetigt" if bestaetigt else "abgelehnt"

    # Was nur das Dashboard aufruft.
    leitende = ("anna",)

    def offene_freigaben(self, firma, person):
        return [
            {"id": fid, "firma": firma, "aufgabe": f["anfrage"]["aufgabe"], "person": f["anfrage"]["person"],
             "faehigkeit": f["anfrage"]["faehigkeit"], "version": f["anfrage"]["version"], "bereich": f["anfrage"].get("bereich", ""),
             "daten": {k: fingerabdruck(v) for k, v in f["eingaben"].items()}, "stand": "offen",
             "angefragt_am": "2026-11-11T18:00:00Z", "gueltig_bis": "2026-11-18T18:00:00Z"}
            for fid, f in self.freigaben.items() if f["stand"] == "offen" and f["anfrage"]["firma"] == firma
        ]

    def entscheide_freigabe(self, firma, freigabe, person, bestaetigt):
        if person not in self.leitende:
            raise FreigabeAbgewiesen("Nur leitende Personen können bestätigen oder ablehnen.")
        if self.freigaben.get(freigabe, {}).get("stand") != "offen":
            raise FreigabeAbgewiesen("Über diese Freigabe wurde schon entschieden.")
        self.entscheide(freigabe, bestaetigt)
        return {"id": freigabe, "stand": self.freigaben[freigabe]["stand"]}

    def fuehre(self, anfrage, eingaben):
        if self.kaputt:
            raise PfoertnerFehler("Pförtner nicht erreichbar.")
        self.aufrufe.append(anfrage["faehigkeit"])
        if anfrage["faehigkeit"] == ZAEHLEN[0]:
            return {"entscheidung": {"ergebnis": "erlaubt", "audit_nr": 1}, "ergebnis": {"zeichen": len(eingaben["text"])}}
        s = self._schritt(anfrage, eingaben)
        for fid, f in reversed(self.freigaben.items()):
            if f["schritt"] != s or f["stand"] == "verbraucht":
                continue
            if f["stand"] == "offen":
                return {"entscheidung": {"ergebnis": "wartet", "grund": "Eine leitende Person muss bestätigen.", "freigabe_id": fid}}
            if f["stand"] == "abgelehnt":
                return {"entscheidung": {"ergebnis": "abgelehnt", "grund": "Eine leitende Person hat diesen Schritt abgelehnt.", "freigabe_id": fid}}
            f["stand"] = "verbraucht"
            self.gesendet.append(eingaben["text"])
            return {"entscheidung": {"ergebnis": "erlaubt", "audit_nr": 7, "freigabe_id": fid}, "ergebnis": {"gesendet": True}}
        fid = f"f-{len(self.freigaben) + 1}"
        self.freigaben[fid] = {"schritt": s, "stand": "offen", "anfrage": anfrage, "eingaben": eingaben}
        return {"entscheidung": {"ergebnis": "wartet", "grund": "Eine leitende Person muss bestätigen.", "freigabe_id": fid}}


def probe(pf):
    """Probe-Ablauf: erst zählen (harmlos), dann melden (wirkt nach außen)."""

    def zaehlen(z: Zustand) -> Zustand:
        s = schritt(pf, z, ZAEHLEN, {"text": z["eingabe"]["text"]})
        return {"ergebnis": {"zeichen": s.ergebnis.get("zeichen"), "gezaehlt": s.erlaubt}}

    def melden(z: Zustand) -> Zustand:
        text = z["eingabe"]["text"]
        s = schritt(pf, z, MELDEN, {"text": text}, bereich="kanal/probe", anzeige={"text": text})
        return {"ergebnis": {**z["ergebnis"], "gemeldet": s.erlaubt, "grund": s.grund, "freigabe_id": s.freigabe_id}}

    def baue() -> StateGraph:
        g = StateGraph(Zustand)
        g.add_node("zaehlen", zaehlen)
        g.add_node("melden", melden)
        g.add_edge(START, "zaehlen")
        g.add_edge("zaehlen", "melden")
        g.add_edge("melden", END)
        return g

    return baue


def neu(pf, stand=None):
    return Orchestrator({"probe": probe(pf)}, stand)


def test_haelt_an_und_macht_nach_bestaetigung_weiter():
    pf = FreigabePfoertner()
    o = neu(pf)
    lauf = o.starte("probe", "a", "ben", {"text": "hallo"})
    assert lauf.stand == "wartet" and pf.gesendet == []
    assert lauf.ergebnis == {"zeichen": 5, "gezaehlt": True}
    [w] = lauf.wartet_auf
    assert w["freigabe_id"] == "f-1" and w["faehigkeit"] == "probe_melden" and w["anzeige"] == {"text": "hallo"}
    assert o.stand("a", lauf.id).wartet_auf == lauf.wartet_auf

    pf.entscheide("f-1", True)
    ende = o.setze_fort("a", lauf.id)
    assert ende.stand == "fertig" and pf.gesendet == ["hallo"]
    assert ende.ergebnis == {"zeichen": 5, "gezaehlt": True, "gemeldet": True, "grund": "", "freigabe_id": "f-1"}
    # Der erste Schritt lief nur einmal, der Lauf hat dort weitergemacht, wo er stand.
    assert pf.aufrufe.count("probe_zaehlen") == 1
    # Fertig heißt: kein Stand mehr.
    with pytest.raises(LaufUnbekannt):
        o.setze_fort("a", lauf.id)


def test_ohne_entscheidung_wartet_der_lauf_weiter():
    pf = FreigabePfoertner()
    o = neu(pf)
    lauf = o.starte("probe", "a", "ben", {"text": "hallo"})
    for _ in range(3):
        nochmal = o.setze_fort("a", lauf.id)
        assert nochmal.stand == "wartet" and nochmal.wartet_auf[0]["freigabe_id"] == "f-1"
    assert pf.gesendet == [] and len(pf.freigaben) == 1
    pf.entscheide("f-1", True)
    assert o.setze_fort("a", lauf.id).stand == "fertig" and pf.gesendet == ["hallo"]


def test_abgelehnt_laeuft_zu_ende_ohne_zu_senden():
    pf = FreigabePfoertner()
    o = neu(pf)
    lauf = o.starte("probe", "a", "ben", {"text": "hallo"})
    pf.entscheide("f-1", False)
    ende = o.setze_fort("a", lauf.id)
    assert ende.stand == "fertig" and pf.gesendet == []
    assert ende.ergebnis["gemeldet"] is False and "abgelehnt" in ende.ergebnis["grund"]


def test_der_orchestrator_kann_sich_nichts_selbst_bestaetigen():
    """Fortsetzen ist nur ein Anstoß. Ob der Schritt läuft, sagt allein der Pförtner."""
    pf = FreigabePfoertner()
    o = neu(pf)
    lauf = o.starte("probe", "a", "ben", {"text": "hallo"})
    o.setze_fort("a", lauf.id)
    assert pf.gesendet == [] and pf.freigaben["f-1"]["stand"] == "offen"


def test_lauf_nur_in_der_eigenen_firma():
    pf = FreigabePfoertner()
    o = neu(pf)
    lauf = o.starte("probe", "a", "ben", {"text": "hallo"})
    pf.entscheide("f-1", True)
    for firma, lauf_id in [("b", lauf.id), ("a", "probe-" + "0" * 32), ("a", "gibtsnicht-" + "0" * 32), ("a", "quatsch")]:
        with pytest.raises(LaufUnbekannt):
            o.setze_fort(firma, lauf_id)
        with pytest.raises(LaufUnbekannt):
            o.stand(firma, lauf_id)
    assert pf.gesendet == []


def test_pfoertner_weg_beim_start_hinterlaesst_keinen_stand():
    pf = FreigabePfoertner()
    pf.kaputt = True
    staende = []

    def stand(firma):
        from ablauf.stand import ArbeitsspeicherStand

        if not staende:
            staende.append(ArbeitsspeicherStand())
        return staende[0]

    with pytest.raises(PfoertnerFehler):
        neu(pf, stand).starte("probe", "a", "ben", {"text": "hallo"})
    assert not staende[0].storage


def test_pfoertner_weg_beim_fortsetzen_laesst_den_lauf_warten():
    pf = FreigabePfoertner()
    o = neu(pf)
    lauf = o.starte("probe", "a", "ben", {"text": "hallo"})
    pf.entscheide("f-1", True)
    pf.kaputt = True
    with pytest.raises(PfoertnerFehler):
        o.setze_fort("a", lauf.id)
    pf.kaputt = False
    assert o.setze_fort("a", lauf.id).stand == "fertig" and pf.gesendet == ["hallo"]


def test_ablauf_1_laeuft_ueber_den_orchestrator():
    pf, sp = FalscherPfoertner(), ListenSpeicher()
    o = Orchestrator({MAIL_EINORDNEN: mail_einordnen(pf, sp)})
    lauf = o.starte(MAIL_EINORDNEN, EINST.firma, "chefin", eingabe_aus(EINST))
    assert lauf.stand == "fertig"
    assert lauf.ergebnis["gelesen"] == 3 and lauf.ergebnis["eingeordnet"] == 2 and lauf.ergebnis["nicht_eingeordnet"] == 1
    # Der Lauf ist die Aufgabe beim Pförtner, und die Person kommt vom Lauf.
    assert {a[2]["aufgabe"] for a in pf.aufrufe} == {lauf.id}
    assert {a[2]["person"] for a in pf.aufrufe} == {"chefin"}
    assert len(sp.liste(EINST.firma)) == 3


def test_unbekannter_ablauf_und_fehlende_angaben():
    o = neu(FreigabePfoertner())
    with pytest.raises(KeyError):
        o.starte("gibtsnicht", "a", "ben")
    with pytest.raises(ValueError):
        o.starte("probe", "", "ben")
    with pytest.raises(ValueError):
        Orchestrator({"Nicht-Gut": probe(None)})
