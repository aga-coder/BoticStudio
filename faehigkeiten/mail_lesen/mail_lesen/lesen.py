"""Mails aus einem IMAP-Postfach lesen, ohne dort etwas zu ändern.

Der Ordner wird nur zum Lesen geöffnet (EXAMINE) und Mails werden mit
BODY.PEEK geholt. So bleiben ungelesene Mails ungelesen.

Postfach und Ordner kommen ausschließlich aus dem Bereich, den der
Pförtner geprüft hat. Die Zugangsdaten kommen vom Pförtner aus dem Tresor
und werden nirgends gespeichert oder ausgegeben.
"""

from __future__ import annotations

import datetime as dt
import email
import email.policy
import imaplib
import re
import ssl
from dataclasses import asdict, dataclass
from email.message import EmailMessage
from email.utils import parsedate_to_datetime
from typing import Any, Callable

import truststore

MAX_TAGE = 31
MAX_MAILS = 100
MAX_TEXT = 20_000

_MONATE = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]


class Fehler(Exception):
    """Ein Fehler, dessen Text gefahrlos an den Nutzer gehen darf."""


@dataclass
class Mail:
    uid: str
    message_id: str
    von: str
    an: str
    betreff: str
    datum: str
    text: str
    gekuerzt: bool


def zerlege_bereich(bereich: str) -> tuple[str, str]:
    """"postfach/<adresse>[/<ordner>...]" → (adresse, ordner). Ohne Ordner: INBOX."""
    teile = bereich.strip("/").split("/")
    if len(teile) < 2 or teile[0] != "postfach" or not teile[1]:
        raise Fehler(f"Bereich {bereich!r} ist kein Postfach.")
    if any(t in ("", ".", "..") for t in teile):
        raise Fehler(f"Bereich {bereich!r} ist ungültig.")
    ordner = "/".join(teile[2:]) or "INBOX"
    return teile[1], ordner


def imap_datum(tag: dt.date) -> str:
    """IMAP verlangt englische Monatsnamen, unabhängig von der Spracheinstellung."""
    return f"{tag.day:02d}-{_MONATE[tag.month - 1]}-{tag.year}"


def _ordnername(ordner: str) -> str:
    return '"' + ordner.replace("\\", "\\\\").replace('"', '\\"') + '"'


def _text(nachricht: EmailMessage) -> str:
    teil = nachricht.get_body(preferencelist=("plain", "html"))
    if teil is None:
        return ""
    try:
        inhalt = teil.get_content()
    except (LookupError, ValueError):
        inhalt = teil.get_payload(decode=True) or b""
        inhalt = inhalt.decode("utf-8", errors="replace")
    if teil.get_content_subtype() == "html":
        inhalt = re.sub(r"(?is)<(script|style).*?</\1>", " ", inhalt)
        inhalt = re.sub(r"<[^>]+>", " ", inhalt)
        inhalt = re.sub(r"[ \t]+", " ", inhalt)
    return inhalt.strip()


def _datum(wert: str | None) -> str:
    if not wert:
        return ""
    try:
        return parsedate_to_datetime(wert).isoformat()
    except (TypeError, ValueError):
        return ""


def verbinde_sicher(server: str, port: int, timeout: float) -> imaplib.IMAP4_SSL:
    """IMAP über TLS mit Prüfung von Zertifikat und Servername.

    imaplib prüft das Zertifikat ohne eigenen Kontext nicht. Dann könnte sich
    jemand dazwischenschalten und das Passwort abgreifen.

    Geprüft wird gegen die Zertifikate des Betriebssystems (truststore). Unter
    Windows lädt das System fehlende Stammzertifikate selbst nach, z. B. das
    von WEB.DE (Telekom). Mit Pythons eigenem Kontext schlug das fehl.
    """
    kontext = truststore.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    return imaplib.IMAP4_SSL(server, port, ssl_context=kontext, timeout=timeout)


def lies(
    bereich: str,
    zugang: dict[str, Any],
    seit_tagen: int = 7,
    hoechstens: int = MAX_MAILS,
    heute: dt.date | None = None,
    verbinde: Callable[..., Any] | None = None,
) -> list[Mail]:
    """Liest die Mails der letzten Tage, neueste zuerst."""
    _, ordner = zerlege_bereich(bereich)
    seit_tagen = max(1, min(int(seit_tagen), MAX_TAGE))
    hoechstens = max(1, min(int(hoechstens), MAX_MAILS))

    try:
        server = str(zugang["server"])
        benutzer = str(zugang["benutzer"])
        passwort = str(zugang["passwort"])
    except (KeyError, TypeError):
        raise Fehler("Zugang unvollständig: server, benutzer und passwort sind nötig.") from None
    port = int(zugang.get("port", 993))

    if verbinde is None:
        verbinde = verbinde_sicher
    seit = (heute or dt.date.today()) - dt.timedelta(days=seit_tagen)

    try:
        imap = verbinde(server, port, timeout=30)
    except ssl.SSLCertVerificationError:
        raise Fehler("Das Zertifikat des Mailservers ist nicht gültig. Aus Sicherheitsgründen keine Verbindung.") from None
    except (OSError, imaplib.IMAP4.error):
        raise Fehler("Der Mailserver ist nicht erreichbar.") from None
    try:
        try:
            imap.login(benutzer, passwort)
        except imaplib.IMAP4.error:
            raise Fehler("Anmeldung am Postfach fehlgeschlagen.") from None

        typ, _ = imap.select(_ordnername(ordner), readonly=True)
        if typ != "OK":
            raise Fehler(f"Ordner {ordner!r} nicht gefunden.")

        typ, daten = imap.uid("SEARCH", None, "SINCE", imap_datum(seit))
        if typ != "OK":
            raise Fehler("Suche im Postfach fehlgeschlagen.")
        uids = (daten[0] or b"").split()
        neueste = list(reversed(uids))[:hoechstens]

        mails: list[Mail] = []
        for uid in neueste:
            typ, teile = imap.uid("FETCH", uid, "(BODY.PEEK[])")
            if typ != "OK":
                continue
            roh = next((t[1] for t in teile if isinstance(t, tuple) and len(t) > 1), None)
            if not roh:
                continue
            nachricht = email.message_from_bytes(roh, policy=email.policy.default)
            text = _text(nachricht)
            mails.append(
                Mail(
                    uid=uid.decode(),
                    message_id=str(nachricht.get("Message-ID", "")),
                    von=str(nachricht.get("From", "")),
                    an=str(nachricht.get("To", "")),
                    betreff=str(nachricht.get("Subject", "")),
                    datum=_datum(nachricht.get("Date")),
                    text=text[:MAX_TEXT],
                    gekuerzt=len(text) > MAX_TEXT,
                )
            )
        return mails
    finally:
        try:
            imap.logout()
        except Exception:  # noqa: BLE001 - Abmelden darf das Ergebnis nicht verderben
            pass


def als_dict(mails: list[Mail]) -> dict[str, Any]:
    return {"anzahl": len(mails), "mails": [asdict(m) for m in mails]}
