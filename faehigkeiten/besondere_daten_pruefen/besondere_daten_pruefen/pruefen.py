"""Erste Filterstufe für besondere Daten nach Art. 9 DSGVO.

Eine Wortliste sucht nach Hinweisen auf Gesundheit, Religion, politische
Meinung, Gewerkschaft, ethnische Herkunft, Sexualleben und genetische oder
biometrische Daten. Findet sie etwas, markiert sie die Mail mit
„art9_verdacht“. Der Pförtner lässt eine so markierte Mail an kein Ziel
außerhalb der EU.

Grenze: Die Wortliste ist keine Garantie. Was sie nicht erkennt, geht an das
Modell. Sie markiert lieber einmal zu viel als einmal zu wenig.

Diese Fähigkeit läuft innerhalb von BoticStudio (EU) und gibt keine Daten
hinaus. Sie meldet nur Bereiche, nie die gefundenen Wörter oder Textstellen.
"""

from __future__ import annotations

import re
import unicodedata
from typing import Any

MARKIERUNG = "art9_verdacht"

# Wortanfänge je Bereich. Ein Eintrag passt am Anfang eines Wortes, z. B.
# „krank“ auf „Krankmeldung“. Mitten im Wort wird nicht gesucht, darum
# erkennt „krank“ nicht „Wochenkrank“. Bei mehreren Wörtern darf jedes Wort
# eine Endung haben („politischen Überzeugung“). Ein „$“ am Ende heißt: nur
# das ganze Wort („verdi$“ passt nicht auf „Verdienst“).
WORTLISTE: dict[str, list[str]] = {
    "gesundheit": [
        "krank", "erkrank", "arbeitsunfaehig", "au-bescheinigung", "attest", "arzt", "aerzt",
        "diagnos", "befund", "therapie", "therapeut", "medikament", "klinik", "krankenhaus",
        "reha$", "rehabilitation", "schwanger", "behinder", "schwerbehindert", "pflegegrad", "psych", "depression",
        "burnout", "krebs", "tumor", "hiv$", "aids$", "allergi", "impf", "operation", "unfall",
        "verletz", "suchterkrank", "entzug", "chemotherapie", "corona-positiv", "infektion",
        "gute besserung", "mutterschutz",
        "sick", "illness", "diagnosis", "pregnan", "disabilit", "medical", "hospital",
    ],
    "religion": [
        "religio", "konfession", "kirchensteuer", "kirchenaustritt", "muslim", "islam", "juedisch",
        "jude$", "juden$", "katholisch", "evangelisch", "moschee", "synagoge", "ramadan", "christlich",
        "religious", "church",
    ],
    "politik": [
        "politische meinung", "politische ueberzeugung", "parteimitglied", "parteibuch",
        "parteizugehoerigkeit", "political opinion",
    ],
    "gewerkschaft": [
        "gewerkschaft", "verdi$", "ver.di$", "ig metall", "trade union", "labour union", "labor union",
    ],
    "herkunft": [
        "ethnisch", "rassis", "hautfarbe", "migrationshintergrund", "abstammung", "ethnic", "racial",
    ],
    "sexualleben": [
        "sexuell", "sexualit", "homosexu", "bisexu", "transgender", "transsexu", "lesbisch",
        "schwul", "queer$", "geschlechtsangleich", "sexual",
    ],
    "genetik_biometrie": [
        "genetisch", "gentest", "dna$", "erbkrank", "fingerabdruck", "gesichtserkennung",
        "biometri", "iris-scan", "genetic", "biometric",
    ],
}

_ERSATZ = str.maketrans({"ä": "ae", "ö": "oe", "ü": "ue", "ß": "ss"})


def normalisiere(text: str) -> str:
    """Kleinschreibung, Umlaute ausgeschrieben, Sonderformen vereinheitlicht."""
    text = unicodedata.normalize("NFKC", text).lower().translate(_ERSATZ)
    # Restliche Akzente entfernen (é -> e).
    text = "".join(z for z in unicodedata.normalize("NFKD", text) if not unicodedata.combining(z))
    return re.sub(r"\s+", " ", text)


def _muster(eintrag: str) -> re.Pattern[str]:
    ganz = eintrag.endswith("$")
    teile = [re.escape(t) for t in normalisiere(eintrag.rstrip("$")).split(" ")]
    return re.compile(r"(?<![a-z0-9])" + r"[a-z]*\s+".join(teile) + (r"(?![a-z0-9])" if ganz else ""))


_MUSTER = {bereich: [_muster(e) for e in eintraege] for bereich, eintraege in WORTLISTE.items()}


def text_der_mail(mail: Any) -> str:
    """Betreff und Text einer Mail (wie sie „Mail lesen“ liefert) oder ein reiner Text."""
    if isinstance(mail, str):
        return mail
    if isinstance(mail, dict):
        return "\n".join(str(mail.get(feld) or "") for feld in ("betreff", "text"))
    raise ValueError("mail muss ein Text oder ein Objekt mit betreff und text sein")


def pruefe(mail: Any) -> dict[str, Any]:
    text = normalisiere(text_der_mail(mail))
    hinweise = sorted(b for b, muster in _MUSTER.items() if any(m.search(text) for m in muster))
    return {
        "markierungen": [MARKIERUNG] if hinweise else [],
        "hinweise": hinweise,
        "grenze": "Wortliste, erste Filterstufe. Was sie nicht erkennt, geht an das Modell.",
    }
