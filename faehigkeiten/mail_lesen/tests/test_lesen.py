import datetime as dt
import imaplib

import pytest

from mail_lesen.lesen import TEIL_GANZ, TEIL_KOPF, Fehler, imap_datum, lies, lies_koepfe, zerlege_bereich

ZUGANG = {"server": "imap.test", "benutzer": "info@test.de", "passwort": "geheim-123"}


def mail(nr: int, betreff: str, text: str = "Hallo", html: bool = False) -> bytes:
    art = "text/html" if html else "text/plain"
    return (
        f"Message-ID: <{nr}@test>\r\nFrom: Kunde {nr} <k{nr}@kunde.de>\r\nTo: info@test.de\r\n"
        f"Subject: {betreff}\r\nDate: Wed, 21 Oct 2026 10:0{nr}:00 +0200\r\n"
        f"Content-Type: {art}; charset=utf-8\r\n\r\n{text}\r\n"
    ).encode()


class FalscherServer:
    """Merkt sich jeden Befehl. Schreibende Befehle lassen den Test scheitern."""

    def __init__(self, mails: dict[bytes, bytes], passwort: str = "geheim-123", ordner_ok: bool = True):
        self.mails = mails
        self.passwort = passwort
        self.ordner_ok = ordner_ok
        self.befehle: list[tuple] = []
        self.abgemeldet = False

    def __call__(self, server, port, timeout=None):
        self.befehle.append(("verbinde", server, port))
        return self

    def login(self, benutzer, passwort):
        self.befehle.append(("login", benutzer))
        if passwort != self.passwort:
            raise imaplib.IMAP4.error("AUTHENTICATIONFAILED")
        return "OK", [b""]

    def select(self, ordner, readonly=False):
        self.befehle.append(("select", ordner, readonly))
        return ("OK", [b"3"]) if self.ordner_ok else ("NO", [b"nicht da"])

    def uid(self, befehl, *args):
        self.befehle.append(("uid", befehl, *args))
        if befehl == "SEARCH":
            return "OK", [b" ".join(sorted(self.mails))]
        if befehl == "FETCH":
            uid, teil = args
            assert teil in (TEIL_GANZ, TEIL_KOPF), "mails dürfen nicht als gelesen markiert werden"
            roh = self.mails[uid]
            if teil == TEIL_KOPF:
                # Der Server liefert dann nur die Kopfzeilen, ohne Text.
                roh = roh.split(b"\r\n\r\n", 1)[0] + b"\r\n\r\n"
            return "OK", [(b"1 (UID " + uid + b" BODY[] {n}", roh), b")"]
        raise AssertionError(f"verbotener befehl {befehl}")

    def store(self, *args):
        raise AssertionError("STORE ändert das postfach")

    def logout(self):
        self.abgemeldet = True


def test_liest_neueste_zuerst_und_nur_lesend():
    server = FalscherServer({b"1": mail(1, "Anfrage"), b"2": mail(2, "Beschwerde"), b"3": mail(3, "Newsletter")})
    mails = lies("postfach/info@test.de/INBOX", ZUGANG, seit_tagen=7, hoechstens=2,
                 heute=dt.date(2026, 10, 22), verbinde=server)
    assert [m.betreff for m in mails] == ["Newsletter", "Beschwerde"]
    assert mails[0].von == "Kunde 3 <k3@kunde.de>"
    assert mails[0].message_id == "<3@test>"
    assert mails[0].datum.startswith("2026-10-21T10:03")
    assert ("select", '"INBOX"', True) in server.befehle
    assert ("uid", "SEARCH", None, "SINCE", "15-Oct-2026") in server.befehle
    assert server.abgemeldet


def test_ohne_ordner_wird_inbox_gelesen():
    assert zerlege_bereich("postfach/info@test.de") == ("info@test.de", "INBOX")
    assert zerlege_bereich("postfach/info@test.de/Archiv/2026") == ("info@test.de", "Archiv/2026")


@pytest.mark.parametrize("bereich", ["kalender/x", "postfach", "postfach//INBOX", "postfach/a/../b", ""])
def test_ungueltiger_bereich(bereich):
    with pytest.raises(Fehler):
        zerlege_bereich(bereich)


def test_html_wird_zu_text():
    server = FalscherServer({b"1": mail(1, "Html", "<p>Guten <b>Tag</b></p><script>x()</script>", html=True)})
    (m,) = lies("postfach/info@test.de", ZUGANG, verbinde=server)
    assert m.text == "Guten Tag"


def test_falsches_passwort_ohne_passwort_in_meldung():
    server = FalscherServer({}, passwort="anders")
    with pytest.raises(Fehler) as fehler:
        lies("postfach/info@test.de", ZUGANG, verbinde=server)
    assert "geheim-123" not in str(fehler.value)
    assert server.abgemeldet


def test_unvollstaendiger_zugang():
    with pytest.raises(Fehler, match="unvollständig"):
        lies("postfach/info@test.de", {"server": "x"}, verbinde=FalscherServer({}))


def test_ordner_fehlt():
    with pytest.raises(Fehler, match="nicht gefunden"):
        lies("postfach/info@test.de/Gibtsnicht", ZUGANG, verbinde=FalscherServer({}, ordner_ok=False))


def test_grenzen_werden_eingehalten():
    server = FalscherServer({str(i).encode(): mail(i % 10, f"M{i}") for i in range(1, 200)})
    mails = lies("postfach/info@test.de", ZUGANG, seit_tagen=999, hoechstens=999,
                 heute=dt.date(2026, 10, 22), verbinde=server)
    assert len(mails) == 100
    assert ("uid", "SEARCH", None, "SINCE", "21-Sep-2026") in server.befehle


def test_langer_text_wird_gekuerzt():
    server = FalscherServer({b"1": mail(1, "Lang", "x" * 30_000)})
    (m,) = lies("postfach/info@test.de", ZUGANG, verbinde=server)
    assert len(m.text) == 20_000 and m.gekuerzt


def test_imap_datum_ist_englisch():
    assert imap_datum(dt.date(2026, 3, 5)) == "05-Mar-2026"


def test_standardverbindung_prueft_zertifikat(monkeypatch):
    import ssl

    import truststore

    from mail_lesen import lesen

    gesehen = {}

    def ersatz(server, port, ssl_context=None, timeout=None):
        gesehen["kontext"] = ssl_context
        raise OSError("abgebrochen")

    monkeypatch.setattr(lesen.imaplib, "IMAP4_SSL", ersatz)
    with pytest.raises(Fehler):
        lies("postfach/info@test.de", ZUGANG)
    kontext = gesehen["kontext"]
    assert kontext is not None
    assert kontext.verify_mode == ssl.CERT_REQUIRED and kontext.check_hostname
    # Zertifikate des Betriebssystems, damit z. B. WEB.DE auch unter Windows geprüft werden kann
    assert isinstance(kontext, truststore.SSLContext)


def test_ungueltiges_zertifikat_klare_meldung(monkeypatch):
    import ssl

    from mail_lesen import lesen

    def ersatz(server, port, ssl_context=None, timeout=None):
        raise ssl.SSLCertVerificationError("certificate verify failed")

    monkeypatch.setattr(lesen.imaplib, "IMAP4_SSL", ersatz)
    with pytest.raises(Fehler, match="Zertifikat"):
        lies("postfach/info@test.de", ZUGANG)


def test_kopfzeilen_ohne_text_und_nur_lesend():
    server = FalscherServer({b"1": mail(1, "Anfrage", "Geheimer Inhalt"), b"2": mail(2, "Beschwerde", "Ich bin krank")})
    koepfe = lies_koepfe("postfach/info@test.de/INBOX", ZUGANG, seit_tagen=31, heute=dt.date(2026, 10, 22), verbinde=server)
    assert [(k.uid, k.betreff, k.von) for k in koepfe] == [("2", "Beschwerde", "Kunde 2 <k2@kunde.de>"), ("1", "Anfrage", "Kunde 1 <k1@kunde.de>")]
    assert koepfe[0].datum.startswith("2026-10-21T10:02") and koepfe[0].message_id == "<2@test>"
    # Nur die Kopfzeilen werden geholt, der Text verlässt das Postfach nicht.
    holen = [b for b in server.befehle if b[:2] == ("uid", "FETCH")]
    assert holen and all(b[3] == TEIL_KOPF for b in holen)
    assert not hasattr(koepfe[0], "text")
    assert ("select", '"INBOX"', True) in server.befehle and server.abgemeldet


def test_kopfzeilen_grenzen():
    mails = {str(i).encode(): mail(i % 10, f"M{i}") for i in range(1, 600)}
    server = FalscherServer(mails)
    koepfe = lies_koepfe("postfach/info@test.de", ZUGANG, seit_tagen=999, hoechstens=10_000, heute=dt.date(2026, 10, 22), verbinde=server)
    assert len(koepfe) == 500
    assert ("uid", "SEARCH", None, "SINCE", "21-Sep-2026") in server.befehle
