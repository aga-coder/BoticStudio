import smtplib
import ssl

import pytest

from systemmail_senden.senden import Fehler, baue_mail, pruefe_link, sende, zerlege_bereich

BASIS = "https://app.boticstudio.de/"
LINK = BASIS + "bestaetigen?t=abc"
BEREICH = "systempost/konto@boticstudio.de"
ZUGANG = {"server": "smtp.example.org", "port": 587, "benutzer": "konto@boticstudio.de", "passwort": "ganz-geheim-123"}
ZIEL = {"art": "mailserver", "adresse": "smtp.example.org"}


class Verbindung:
    def __init__(self, login_fehler=None, sende_fehler=None, abgelehnt=None):
        self.login_fehler, self.sende_fehler, self.abgelehnt = login_fehler, sende_fehler, abgelehnt or {}
        self.gesendet, self.angemeldet, self.beendet = [], None, False

    def login(self, benutzer, passwort):
        if self.login_fehler:
            raise self.login_fehler
        self.angemeldet = (benutzer, passwort)

    def send_message(self, mail, from_addr=None):
        if self.sende_fehler:
            raise self.sende_fehler
        self.gesendet.append((from_addr, mail))
        return self.abgelehnt

    def quit(self):
        self.beendet = True


def schicke(verbindung=None, **anders):
    v = verbindung or Verbindung()
    werte = dict(bereich=BEREICH, zugang=ZUGANG, ziel=ZIEL, vorlage="konto_bestaetigen",
                 empfaenger="neu@kunde.de", werte={"link": LINK})
    werte.update(anders)
    return sende(**werte, link_basis=BASIS, verbinde=lambda *a: v), v


def test_sendet_bestaetigungsmail():
    ergebnis, v = schicke()
    assert ergebnis["gesendet"] is True and ergebnis["message_id"].endswith("@boticstudio.de>")
    assert v.angemeldet == ("konto@boticstudio.de", "ganz-geheim-123") and v.beendet
    absender, mail = v.gesendet[0]
    assert absender == "konto@boticstudio.de"
    assert mail["To"] == "neu@kunde.de" and "BoticStudio" in str(mail["From"])
    assert mail["Auto-Submitted"] == "auto-generated"
    assert LINK in mail.get_content() and "24 Stunden" in mail.get_content()
    # Das Ergebnis nennt weder Empfänger noch Zugang.
    assert "kunde.de" not in str(ergebnis) and "geheim" not in str(ergebnis)


@pytest.mark.parametrize("empfaenger", [
    "a@b.de\nBcc: boese@x.de", "a@b.de, c@d.de", "Name <a@b.de>", "ohne-at", "", None, "a@b", "a..b@c.de", "a@b.de\r\n",
])
def test_empfaenger_muss_genau_eine_adresse_sein(empfaenger):
    with pytest.raises(Fehler, match="Empfänger"):
        schicke(empfaenger=empfaenger)


@pytest.mark.parametrize("link", [
    "https://boese.example/bestaetigen", "http://app.boticstudio.de/x", "https://app.boticstudio.de.boese.example/x",
    "https://app.boticstudio.de@boese.example/x", "https://app.boticstudio.de:8443/x", "javascript:alert(1)",
    "https://app.boticstudio.de/x y", "https://app.boticstudio.de/x\nBcc: a@b.de", 5, "",
])
def test_link_muss_zu_boticstudio_fuehren(link):
    with pytest.raises(Fehler, match="Link"):
        schicke(werte={"link": link})


def test_link_basis_mit_pfad_und_lokal():
    assert pruefe_link("https://x.de/app/bestaetigen?t=1", "https://x.de/app")
    for fremd in ("https://x.de/anders", "https://x.de/app/../anders", "https://x.de/appx"):
        with pytest.raises(Fehler):
            pruefe_link(fremd, "https://x.de/app")
    assert pruefe_link("http://localhost:5173/bestaetigen?t=1", "http://localhost:5173/")
    with pytest.raises(Fehler, match="https"):
        pruefe_link("http://x.de/a", "http://x.de/")


def test_ohne_link_basis_wird_nichts_gesendet():
    v = Verbindung()
    with pytest.raises(Fehler, match="keine erlaubte Adresse"):
        sende(BEREICH, ZUGANG, ZIEL, "konto_bestaetigen", "neu@kunde.de", {"link": LINK}, link_basis="", verbinde=lambda *a: v)
    assert not v.gesendet


def test_nur_feste_vorlagen_und_passende_werte():
    with pytest.raises(Fehler, match="Vorlage"):
        schicke(vorlage="freitext")
    for werte in ({}, {"link": LINK, "text": "Kaufen Sie jetzt"}, {"betreff": "x"}, "text", None):
        with pytest.raises(Fehler, match="Werte"):
            schicke(werte=werte)


def test_bereich_muss_absender_postfach_sein():
    assert zerlege_bereich(BEREICH) == "konto@boticstudio.de"
    for bereich in ("postfach/konto@boticstudio.de", "systempost", "systempost/a@b.de/INBOX", "systempost/kein-postfach"):
        with pytest.raises(Fehler):
            zerlege_bereich(bereich)


def test_sendet_nur_an_das_gepruefte_ziel():
    for ziel in ({"art": "mailserver", "adresse": "anderer.example.org"}, {"art": "ki_modell", "adresse": "smtp.example.org"}, {}, None):
        ergebnis = None
        v = Verbindung()
        with pytest.raises(Fehler, match="Ziel"):
            ergebnis, _ = schicke(v, ziel=ziel)
        assert ergebnis is None and not v.gesendet


def test_unbekannter_versandweg_und_unvollstaendiger_zugang():
    with pytest.raises(Fehler, match="Versandweg"):
        schicke(zugang={**ZUGANG, "weg": "brieftaube"})
    with pytest.raises(Fehler, match="unvollständig"):
        schicke(zugang={"server": "smtp.example.org"})


@pytest.mark.parametrize("fehler, text", [
    (smtplib.SMTPAuthenticationError(535, b"falsch: ganz-geheim-123"), "Anmeldung abgelehnt"),
])
def test_anmeldefehler_ohne_geheimnis(fehler, text):
    v = Verbindung(login_fehler=fehler)
    with pytest.raises(Fehler, match=text) as f:
        schicke(v)
    assert "geheim" not in str(f.value) and f.value.__cause__ is None and v.beendet


def test_sendefehler_sind_feste_texte():
    for fehler, text in [
        (smtplib.SMTPRecipientsRefused({"neu@kunde.de": (550, b"nein")}), "Empfängeradresse abgelehnt"),
        (smtplib.SMTPDataError(554, b"spam? konto@boticstudio.de ganz-geheim-123"), "nicht angenommen"),
        (OSError("verbindung weg"), "nicht angenommen"),
    ]:
        with pytest.raises(Fehler, match=text) as f:
            schicke(Verbindung(sende_fehler=fehler))
        assert "geheim" not in str(f.value) and "kunde.de" not in str(f.value)
    with pytest.raises(Fehler, match="Empfängeradresse abgelehnt"):
        schicke(Verbindung(abgelehnt={"neu@kunde.de": (550, b"nein")}))


def test_verbindungsfehler_sind_feste_texte():
    def kaputt(fehler):
        def verbinde(*a):
            raise fehler
        return verbinde

    for fehler, text in [(ssl.SSLCertVerificationError("cert"), "nicht sicher"), (OSError("dns"), "nicht erreichbar")]:
        with pytest.raises(Fehler, match=text):
            sende(BEREICH, ZUGANG, ZIEL, "konto_bestaetigen", "neu@kunde.de", {"link": LINK}, link_basis=BASIS, verbinde=kaputt(fehler))


def test_kein_zeilenumbruch_in_kopfzeilen():
    mail = baue_mail("konto_bestaetigen", "konto@boticstudio.de", "neu@kunde.de", {"link": LINK}, BASIS)
    for kopf in ("From", "To", "Subject"):
        assert "\n" not in str(mail[kopf]) and "\r" not in str(mail[kopf])
