"""Echtes SMTP-Gespräch gegen einen Übungs-Mailserver auf dem eigenen Rechner.

Der Server verlangt Verschlüsselung (STARTTLS) und Anmeldung, wie ein echtes
Postfach. Sein Zertifikat ist für den Test selbst ausgestellt. Der Test
vertraut genau diesem Zertifikat, sonst nichts wird gelockert.
"""

import email
import email.policy
import shutil
import socket
import ssl
import subprocess

import pytest
from aiosmtpd.controller import Controller
from aiosmtpd.smtp import AuthResult, LoginPassword

from systemmail_senden import senden

BENUTZER, PASSWORT = "konto@boticstudio.de", "uebung-geheim-123"
BASIS = "https://app.boticstudio.de/"

pytestmark = pytest.mark.skipif(shutil.which("openssl") is None, reason="openssl fehlt")


class Eingang:
    def __init__(self):
        self.mails = []

    async def handle_DATA(self, server, session, envelope):
        self.mails.append((session.auth_data, envelope.mail_from, envelope.rcpt_tos, envelope.content.decode("utf-8")))
        return "250 OK"


def anmeldung(server, session, envelope, mechanism, auth_data):
    ok = isinstance(auth_data, LoginPassword) and (auth_data.login, auth_data.password) == (BENUTZER.encode(), PASSWORT.encode())
    return AuthResult(success=ok, handled=False, auth_data=auth_data if ok else None)


@pytest.fixture
def uebungsserver(tmp_path, monkeypatch):
    cert, key = tmp_path / "cert.pem", tmp_path / "key.pem"
    subprocess.run(
        ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=localhost",
         "-addext", "subjectAltName=DNS:localhost", "-keyout", str(key), "-out", str(cert)],
        check=True, capture_output=True,
    )
    server_tls = ssl.create_default_context(ssl.Purpose.CLIENT_AUTH)
    server_tls.load_cert_chain(cert, key)
    eingang = Eingang()
    with socket.socket() as frei:
        frei.bind(("127.0.0.1", 0))
        port = frei.getsockname()[1]
    controller = Controller(
        eingang, hostname="127.0.0.1", port=port, tls_context=server_tls, require_starttls=True,
        authenticator=anmeldung, auth_required=True,
    )
    controller.start()
    # Der Test vertraut nur dem Zertifikat des Übungsservers. Name und Zertifikat werden weiter geprüft.
    monkeypatch.setattr(senden, "_tls_kontext", lambda: ssl.create_default_context(cafile=str(cert)))
    try:
        yield eingang, port
    finally:
        controller.stop()


def schicke(port, passwort=PASSWORT):
    zugang = {"server": "localhost", "port": port, "benutzer": BENUTZER, "passwort": passwort}
    return senden.sende(
        "systempost/" + BENUTZER, zugang, {"art": "mailserver", "adresse": "localhost"},
        "konto_bestaetigen", "neu@kunde.de", {"link": BASIS + "bestaetigen?t=abc"}, link_basis=BASIS,
    )


def test_mail_kommt_verschluesselt_und_angemeldet_an(uebungsserver):
    eingang, port = uebungsserver
    ergebnis = schicke(port)
    assert ergebnis["gesendet"] is True
    (auth, von, an, inhalt), = eingang.mails
    assert auth.login == BENUTZER.encode()
    assert von == BENUTZER and an == ["neu@kunde.de"]
    assert "Auto-Submitted: auto-generated" in inhalt
    # So liest es das Mailprogramm des Empfängers: Der Link kommt unverändert an.
    gelesen = email.message_from_string(inhalt, policy=email.policy.default)
    assert BASIS + "bestaetigen?t=abc" in gelesen.get_content()
    assert gelesen["Subject"] == "Bitte bestätigen Sie Ihre E-Mail-Adresse"
    assert PASSWORT not in inhalt


def test_falsches_passwort_sendet_nichts(uebungsserver):
    eingang, port = uebungsserver
    with pytest.raises(senden.Fehler, match="Anmeldung abgelehnt") as f:
        schicke(port, passwort="falsch-falsch-1")
    assert not eingang.mails and "falsch-falsch-1" not in str(f.value)


def test_fremdes_zertifikat_wird_abgelehnt(uebungsserver, monkeypatch):
    eingang, port = uebungsserver
    # Ohne Vertrauen in das Übungszertifikat: so sähe ein dazwischengeschalteter Server aus.
    monkeypatch.setattr(senden, "_tls_kontext", ssl.create_default_context)
    with pytest.raises(senden.Fehler, match="nicht sicher"):
        schicke(port)
    assert not eingang.mails
