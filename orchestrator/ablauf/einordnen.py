"""Ablauf 1: Mails eines Postfachs lesen, auf besondere Daten prüfen, einordnen.

Der Ablauf entscheidet nichts selbst: Jeder Schritt geht an den Pförtner.
Auch eine Mail mit Art.-9-Verdacht wird ihm zum Einordnen vorgelegt. Er
lehnt sie ab, und die Ablehnung steht in seinem Audit. Das ist der Beweis,
dass die Sperre im Pförtner sitzt und nicht im Ablauf.

Der Ablauf gibt keine Mailinhalte aus, nur Zahlen.
"""

from __future__ import annotations

import uuid
from dataclasses import dataclass, field
from typing import Any

from .pfoertner import Pfoertner
from .speicher import Einordnung, Speicher

LESEN = ("mail_lesen", "1.0.0")
PRUEFEN = ("besondere_daten_pruefen", "1.0.0")
EINORDNEN = ("mail_einordnen", "1.0.0")

HINWEIS_ART9 = "Verdacht auf besondere Daten (Art. 9). Die Mail ging an kein Modell außerhalb der EU. Bitte von Hand einordnen."


@dataclass
class Einstellungen:
    firma: str
    person: str
    postfach: str  # Bereich, z. B. "postfach/info@firma.de/INBOX"
    ziel: dict[str, str]
    modell: str
    kategorien: list[Any]
    seit_tagen: int = 7
    hoechstens: int = 100


@dataclass
class Bericht:
    aufgabe: str
    gelesen: int = 0
    eingeordnet: int = 0
    nicht_eingeordnet: int = 0
    fehler: int = 0
    kategorien: dict[str, int] = field(default_factory=dict)
    abbruch: str = ""


def _anfrage(e: Einstellungen, aufgabe: str, faehigkeit: tuple[str, str], **mehr: Any) -> dict[str, Any]:
    return {"firma": e.firma, "person": e.person, "aufgabe": aufgabe, "faehigkeit": faehigkeit[0], "version": faehigkeit[1], **mehr}


def _erlaubt(antwort: dict[str, Any]) -> bool:
    return antwort.get("entscheidung", {}).get("ergebnis") == "erlaubt" and not antwort.get("fehler")


def _grund(antwort: dict[str, Any]) -> str:
    return antwort.get("fehler") or antwort.get("entscheidung", {}).get("grund") or "Unbekannter Grund."


def ordne_postfach_ein(pf: Pfoertner, speicher: Speicher, e: Einstellungen) -> Bericht:
    aufgabe = "einordnen-" + uuid.uuid4().hex[:12]
    bericht = Bericht(aufgabe=aufgabe)

    gelesen = pf.fuehre(
        _anfrage(e, aufgabe, LESEN, bereich=e.postfach),
        {"seit_tagen": e.seit_tagen, "hoechstens": e.hoechstens},
    )
    if not _erlaubt(gelesen):
        bericht.abbruch = "Lesen nicht möglich: " + _grund(gelesen)
        return bericht
    mails = gelesen.get("ergebnis", {}).get("mails", [])
    bericht.gelesen = len(mails)

    for mail in mails:
        eintrag = Einordnung(
            firma=e.firma, bereich=e.postfach, uid=str(mail.get("uid", "")),
            message_id=str(mail.get("message_id", "")), stand="fehler", aufgabe=aufgabe,
        )
        geprueft = pf.fuehre(_anfrage(e, aufgabe, PRUEFEN), {"mail": mail})
        if not _erlaubt(geprueft):
            eintrag.begruendung = "Prüfung nicht möglich: " + _grund(geprueft)
        else:
            eintrag.markierungen = list(geprueft.get("ergebnis", {}).get("markierungen", []))
            # Dieselbe Mail, unverändert: Der Pförtner erkennt sie am Fingerabdruck.
            antwort = pf.fuehre(
                _anfrage(e, aufgabe, EINORDNEN, bereich=e.postfach, ziel=e.ziel),
                {"mail": mail, "kategorien": e.kategorien, "modell": e.modell},
            )
            eintrag.audit_nr = antwort.get("entscheidung", {}).get("audit_nr")
            ergebnis_pf = antwort.get("entscheidung", {}).get("ergebnis")
            if _erlaubt(antwort):
                ergebnis = antwort.get("ergebnis", {})
                eintrag.modell = str(ergebnis.get("modell") or e.modell)
                eintrag.begruendung = str(ergebnis.get("begruendung") or "")
                eintrag.unsicher = bool(ergebnis.get("unsicher", True))
                if ergebnis.get("kategorie"):
                    eintrag.stand, eintrag.kategorie = "eingeordnet", str(ergebnis["kategorie"])
                else:
                    eintrag.stand = "nicht_eingeordnet"
            elif ergebnis_pf in ("abgelehnt", "wartet"):
                eintrag.stand = "nicht_eingeordnet"
                eintrag.begruendung = HINWEIS_ART9 if eintrag.markierungen else _grund(antwort)
            else:
                eintrag.begruendung = _grund(antwort)

        speicher.merke(eintrag)
        if eintrag.stand == "eingeordnet":
            bericht.eingeordnet += 1
            bericht.kategorien[eintrag.kategorie] = bericht.kategorien.get(eintrag.kategorie, 0) + 1
        elif eintrag.stand == "nicht_eingeordnet":
            bericht.nicht_eingeordnet += 1
        else:
            bericht.fehler += 1
    return bericht
