"""Systemmails von BoticStudio verschicken, z. B. den Bestätigungslink.

Drei Schutzregeln:

1. Nur feste Vorlagen. Der Aufrufer nennt Vorlage, Empfänger und die Werte
   für die Lücken. Freien Text kann er nicht senden. So lässt sich die
   Fähigkeit nicht für Werbung oder gefälschte Nachrichten missbrauchen.
2. Ein Link in der Mail muss mit der Adresse beginnen, die beim Start des
   Moduls festgelegt wurde (--link-basis). Der Aufrufer kann also nicht auf
   eine fremde Seite verweisen. Ohne festgelegte Adresse wird nichts gesendet.
3. Gesendet wird nur verschlüsselt, mit geprüftem Zertifikat, und nur an den
   Mailserver, den der Pförtner als Ziel geprüft hat.

Absender, Zugangsdaten und Ziel kommen vom Pförtner. Die Zugangsdaten werden
nirgends gespeichert oder ausgegeben. Fehlermeldungen sind feste Texte.

Der Versandweg ist austauschbar: Heute gibt es „smtp“ (ein normales
Postfach). Ein Versanddienst kommt später als weiterer Weg dazu, ohne dass
sich für Aufrufer oder Pförtner etwas ändert.
"""

from __future__ import annotations

import re
import smtplib
import ssl
from dataclasses import dataclass
from email.headerregistry import Address
from email.message import EmailMessage
from email.utils import format_datetime, localtime, make_msgid
from typing import Any, Callable
from urllib.parse import urlsplit

import truststore

ZEITLIMIT = 20.0
MAX_ADRESSE = 254
MAX_LINK = 600
ABSENDERNAME = "BoticStudio"
ZIEL_ART = "mailserver"


class Fehler(Exception):
    """Ein Fehler, dessen Text gefahrlos an den Nutzer gehen darf."""


@dataclass(frozen=True)
class Vorlage:
    betreff: str
    text: str
    # Lücken, die der Aufrufer füllen muss. Andere Werte werden abgelehnt.
    luecken: tuple[str, ...]


VORLAGEN: dict[str, Vorlage] = {
    "konto_bestaetigen": Vorlage(
        betreff="Bitte bestätigen Sie Ihre E-Mail-Adresse",
        luecken=("link",),
        text=(
            "Guten Tag,\n\n"
            "mit dieser E-Mail-Adresse wurde ein Konto bei BoticStudio angefragt.\n"
            "Bitte bestätigen Sie die Adresse über diesen Link:\n\n"
            "{link}\n\n"
            "Der Link gilt 24 Stunden und nur einmal.\n\n"
            "Wenn Sie kein Konto angefragt haben, müssen Sie nichts tun. "
            "Ohne Bestätigung wird kein Konto angelegt.\n\n"
            "Diese Nachricht wurde automatisch verschickt.\n"
            "BoticStudio\n"
        ),
    ),
}

_ADRESSE = re.compile(r"^[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?\.[A-Za-z]{2,}$")


def pruefe_adresse(wert: Any, was: str) -> str:
    """Genau eine einfache Adresse. Zeilenumbrüche, Namen und Listen fallen durch."""
    if not isinstance(wert, str) or len(wert) > MAX_ADRESSE or not _ADRESSE.match(wert) or ".." in wert:
        raise Fehler(f"{was} ist keine gültige E-Mail-Adresse.")
    return wert


def zerlege_bereich(bereich: str) -> str:
    """"systempost/<adresse>" → Absenderadresse."""
    teile = bereich.strip("/").split("/")
    if len(teile) != 2 or teile[0] != "systempost":
        raise Fehler("Der Bereich ist kein Absender-Postfach von BoticStudio.")
    return pruefe_adresse(teile[1], "Der Absender")


def pruefe_link(link: Any, link_basis: str) -> str:
    """Der Link muss https sein (lokal auch http) und unter der festgelegten Adresse liegen."""
    if not link_basis:
        raise Fehler("Für Links ist keine erlaubte Adresse festgelegt. Es wird nichts gesendet.")
    if not isinstance(link, str) or len(link) > MAX_LINK or re.search(r"\s", link):
        raise Fehler("Der Link ist ungültig.")
    basis, ziel = urlsplit(link_basis), urlsplit(link)
    lokal = basis.hostname in ("localhost", "127.0.0.1")
    if basis.scheme != "https" and not (basis.scheme == "http" and lokal):
        raise Fehler("Die erlaubte Adresse für Links muss mit https beginnen.")
    pfad = basis.path if basis.path.endswith("/") else basis.path + "/"
    gleich = (ziel.scheme, ziel.hostname, ziel.port) == (basis.scheme, basis.hostname, basis.port)
    fremd = ziel.username or ziel.password or ".." in ziel.path.split("/")
    if not gleich or fremd or not (ziel.path + "/").startswith(pfad):
        raise Fehler("Der Link führt nicht zu BoticStudio. Es wird nichts gesendet.")
    return link


def baue_mail(vorlage: str, absender: str, empfaenger: Any, werte: Any, link_basis: str) -> EmailMessage:
    v = VORLAGEN.get(vorlage) if isinstance(vorlage, str) else None
    if v is None:
        raise Fehler("Diese Vorlage gibt es nicht.")
    empfaenger = pruefe_adresse(empfaenger, "Der Empfänger")
    if not isinstance(werte, dict) or set(werte) != set(v.luecken):
        raise Fehler("Die Werte passen nicht zur Vorlage.")
    gefuellt = dict(werte)
    if "link" in gefuellt:
        gefuellt["link"] = pruefe_link(gefuellt["link"], link_basis)

    name, _, domain = absender.partition("@")
    mail = EmailMessage()
    mail["From"] = Address(ABSENDERNAME, name, domain)
    mail["To"] = empfaenger
    mail["Subject"] = v.betreff
    mail["Date"] = format_datetime(localtime())
    mail["Message-ID"] = make_msgid(domain=domain)
    # Sagt Mailprogrammen: automatisch erzeugt, bitte keine Abwesenheitsnotiz zurück.
    mail["Auto-Submitted"] = "auto-generated"
    mail.set_content(v.text.format(**gefuellt))
    return mail


def _tls_kontext() -> ssl.SSLContext:
    # Zertifikate des Betriebssystems, wie bei „Mail lesen“ (wichtig unter Windows).
    return truststore.SSLContext(ssl.PROTOCOL_TLS_CLIENT)


def verbinde_smtp(server: str, port: int, timeout: float) -> smtplib.SMTP:
    """Immer verschlüsselt: Port 465 von Anfang an, sonst STARTTLS. Nie im Klartext."""
    if port == 465:
        return smtplib.SMTP_SSL(server, port, context=_tls_kontext(), timeout=timeout)
    verbindung = smtplib.SMTP(server, port, timeout=timeout)
    try:
        verbindung.starttls(context=_tls_kontext())
    except Exception:
        verbindung.close()
        raise
    return verbindung


def _sende_smtp(mail: EmailMessage, absender: str, zugang: dict[str, Any], verbinde: Callable[..., Any]) -> None:
    try:
        server, port = str(zugang["server"]), int(zugang["port"])
        benutzer, passwort = str(zugang["benutzer"]), str(zugang["passwort"])
    except (KeyError, TypeError, ValueError):
        raise Fehler("Der Zugang zum Absender-Postfach ist unvollständig.") from None
    try:
        verbindung = verbinde(server, port, ZEITLIMIT)
    except ssl.SSLError:
        raise Fehler("Die verschlüsselte Verbindung zum Mailserver ließ sich nicht sicher aufbauen.") from None
    except (OSError, smtplib.SMTPException):
        raise Fehler("Der Mailserver ist nicht erreichbar.") from None
    try:
        try:
            verbindung.login(benutzer, passwort)
        except smtplib.SMTPAuthenticationError:
            raise Fehler("Der Mailserver hat die Anmeldung abgelehnt.") from None
        try:
            abgelehnt = verbindung.send_message(mail, from_addr=absender)
        except smtplib.SMTPRecipientsRefused:
            raise Fehler("Der Mailserver hat die Empfängeradresse abgelehnt.") from None
        if abgelehnt:
            raise Fehler("Der Mailserver hat die Empfängeradresse abgelehnt.")
    except Fehler:
        raise
    except (OSError, smtplib.SMTPException):
        # Der Text des Servers kann Adressen oder Kennungen enthalten, darum ein fester Text.
        raise Fehler("Der Mailserver hat die Mail nicht angenommen.") from None
    finally:
        try:
            verbindung.quit()
        except (OSError, smtplib.SMTPException):
            pass


# Versandwege. Ein neuer Weg (z. B. ein Versanddienst) kommt hier dazu.
WEGE: dict[str, Callable[..., None]] = {"smtp": _sende_smtp}


def sende(
    bereich: str,
    zugang: dict[str, Any],
    ziel: dict[str, Any],
    vorlage: str,
    empfaenger: str,
    werte: dict[str, Any],
    *,
    link_basis: str,
    verbinde: Callable[..., Any] | None = None,
) -> dict[str, Any]:
    absender = zerlege_bereich(bereich)
    if not isinstance(zugang, dict):
        raise Fehler("Der Zugang zum Absender-Postfach fehlt.")
    weg = WEGE.get(str(zugang.get("weg", "smtp")))
    if weg is None:
        raise Fehler("Diesen Versandweg gibt es nicht.")
    # Nur an den Server, den der Pförtner als Ziel geprüft hat.
    if not isinstance(ziel, dict) or ziel.get("art") != ZIEL_ART or not ziel.get("adresse"):
        raise Fehler("Das Ziel ist kein Mailserver.")
    if str(zugang.get("server", "")).lower() != str(ziel["adresse"]).lower():
        raise Fehler("Der Mailserver im Zugang ist nicht das geprüfte Ziel. Es wird nichts gesendet.")

    mail = baue_mail(vorlage, absender, empfaenger, werte, link_basis)
    weg(mail, absender, zugang, verbinde or verbinde_smtp)
    return {"gesendet": True, "message_id": str(mail["Message-ID"])}
