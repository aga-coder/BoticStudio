import copy
import json

from ablauf.einordnen import HINWEIS_ART9, Einstellungen, ordne_postfach_ein
from ablauf.speicher import ListenSpeicher

EINST = Einstellungen(
    firma="f", person="p", postfach="postfach/info@f.de/INBOX",
    ziel={"art": "ki_modell", "adresse": "openrouter.ai"}, modell="anbieter/modell",
    kategorien=["Anfrage", "Beschwerde", "Sonstiges"],
)

MAILS = [
    {"uid": "1", "message_id": "<1@x>", "von": "a@x", "betreff": "Angebot", "text": "Bitte ein Angebot."},
    {"uid": "2", "message_id": "<2@x>", "von": "b@x", "betreff": "Termin", "text": "Ich bin krank."},
    {"uid": "3", "message_id": "<3@x>", "von": "c@x", "betreff": "Ärger", "text": "Ware kaputt."},
]


class FalscherPfoertner:
    """Verhält sich wie der Pförtner: Einordnen nur für vorgeprüfte Mails, markierte nicht ins Ausland."""

    def __init__(self, mails=MAILS, lesen_erlaubt=True, modell_fehler=False):
        self.mails = mails
        self.lesen_erlaubt = lesen_erlaubt
        self.modell_fehler = modell_fehler
        self.geprueft = {}
        self.aufrufe = []

    def fuehre(self, anfrage, eingaben):
        self.aufrufe.append((anfrage["faehigkeit"], copy.deepcopy(eingaben), anfrage))
        name = anfrage["faehigkeit"]
        if name == "mail_lesen":
            if not self.lesen_erlaubt:
                return {"entscheidung": {"ergebnis": "abgelehnt", "grund": "Es fehlt die Erlaubnis."}}
            return {"entscheidung": {"ergebnis": "erlaubt"}, "ergebnis": {"anzahl": len(self.mails), "mails": copy.deepcopy(self.mails)}}
        fp = json.dumps(eingaben["mail"], sort_keys=True)
        if name == "besondere_daten_pruefen":
            m = ["art9_verdacht"] if "krank" in eingaben["mail"]["text"] else []
            self.geprueft[fp] = m
            return {"entscheidung": {"ergebnis": "erlaubt"}, "ergebnis": {"markierungen": m}}
        if name == "mail_einordnen":
            if fp not in self.geprueft:
                return {"entscheidung": {"ergebnis": "abgelehnt", "grund": "nicht vorgeprüft"}}
            if self.geprueft[fp]:
                return {"entscheidung": {"ergebnis": "abgelehnt", "grund": "art9_verdacht", "audit_nr": 9}}
            if self.modell_fehler:
                return {"entscheidung": {"ergebnis": "erlaubt"}, "fehler": "Das KI-Modell war nicht erreichbar."}
            kat = "Beschwerde" if "kaputt" in eingaben["mail"]["text"] else "Anfrage"
            return {"entscheidung": {"ergebnis": "erlaubt", "audit_nr": 5},
                    "ergebnis": {"kategorie": kat, "begruendung": "weil", "unsicher": False, "modell": "anbieter/modell"}}
        raise AssertionError(name)


def test_ganzer_ablauf():
    pf, sp = FalscherPfoertner(), ListenSpeicher()
    bericht = ordne_postfach_ein(pf, sp, EINST)
    assert (bericht.gelesen, bericht.eingeordnet, bericht.nicht_eingeordnet, bericht.fehler) == (3, 2, 1, 0)
    assert bericht.kategorien == {"Anfrage": 1, "Beschwerde": 1}
    nach_uid = {e.uid: e for e in sp.eintraege}
    assert nach_uid["1"].stand == "eingeordnet" and nach_uid["1"].kategorie == "Anfrage"
    assert nach_uid["2"].stand == "nicht_eingeordnet" and nach_uid["2"].begruendung == HINWEIS_ART9
    assert nach_uid["2"].markierungen == ["art9_verdacht"] and nach_uid["2"].audit_nr == 9


def test_auch_markierte_mail_geht_an_den_pfoertner():
    # Der Ablauf entscheidet nicht selbst, der Pförtner lehnt ab (und protokolliert).
    pf = FalscherPfoertner()
    ordne_postfach_ein(pf, ListenSpeicher(), EINST)
    eingeordnet = [e for (name, e, _) in pf.aufrufe if name == "mail_einordnen"]
    assert len(eingeordnet) == 3


def test_dieselbe_mail_fuer_pruefen_und_einordnen():
    pf = FalscherPfoertner()
    ordne_postfach_ein(pf, ListenSpeicher(), EINST)
    pruef = [e["mail"] for (n, e, _) in pf.aufrufe if n == "besondere_daten_pruefen"]
    einord = [e["mail"] for (n, e, _) in pf.aufrufe if n == "mail_einordnen"]
    assert pruef == einord


def test_eine_aufgabe_je_lauf_und_ziel_vom_einstellungen():
    pf = FalscherPfoertner()
    ordne_postfach_ein(pf, ListenSpeicher(), EINST)
    assert len({a["aufgabe"] for (_, _, a) in pf.aufrufe}) == 1
    assert all(a["ziel"] == EINST.ziel for (n, _, a) in pf.aufrufe if n == "mail_einordnen")


def test_keine_inhalte_im_speicher():
    sp = ListenSpeicher()
    ordne_postfach_ein(FalscherPfoertner(), sp, EINST)
    alles = json.dumps([e.__dict__ for e in sp.eintraege], ensure_ascii=False)
    for verboten in ("Angebot", "krank", "kaputt", "a@x", "Termin"):
        assert verboten not in alles


def test_lesen_abgelehnt_bricht_ab():
    bericht = ordne_postfach_ein(FalscherPfoertner(lesen_erlaubt=False), ListenSpeicher(), EINST)
    assert bericht.abbruch.startswith("Lesen nicht möglich") and bericht.gelesen == 0


def test_modellfehler_wird_gespeichert():
    sp = ListenSpeicher()
    bericht = ordne_postfach_ein(FalscherPfoertner(modell_fehler=True), sp, EINST)
    assert bericht.fehler == 2
    assert {e.begruendung for e in sp.eintraege if e.stand == "fehler"} == {"Das KI-Modell war nicht erreichbar."}
