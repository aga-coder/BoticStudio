"""Verbindung zum Pförtner. Der Ablauf ruft Fähigkeiten nur über ihn auf.

Der Pförtner nimmt nur Dienste an, deren Schlüssel er kennt. Den Schlüssel
liest der Dienst aus der Datei in BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI (angelegt
mit `go run ./cmd/dienstschluessel` im Ordner kernel). Er steht in keinem Log
und keiner Fehlermeldung.
"""

from __future__ import annotations

import hashlib
import json
import os
import urllib.error
import urllib.request
from typing import Any, Protocol


class Pfoertner(Protocol):
    def fuehre(self, anfrage: dict[str, Any], eingaben: dict[str, Any]) -> dict[str, Any]: ...


class FreigabePfoertner(Pfoertner, Protocol):
    """Was nur das Dashboard braucht. Der Pförtner nimmt es nur von einem Dienst an,
    bei dem "darf_freigeben" eingetragen ist."""

    def offene_freigaben(self, firma: str, person: str) -> list[dict[str, Any]]: ...

    def entscheide_freigabe(self, firma: str, freigabe: str, person: str, bestaetigt: bool) -> dict[str, Any]: ...


def fingerabdruck(text: str) -> str:
    """Der Fingerabdruck, den der Pförtner für eine Eingabe bildet, hier nur für Texte.

    Der Pförtner bildet SHA-256 über die JSON-Form der Eingabe, wie Go sie
    schreibt. Für einen Text heißt das: wie üblich, aber <, > und & sowie die
    Zeichen U+2028 und U+2029 als \\uXXXX. Ein Test im Kernel und einer hier
    halten dieselben Beispiele fest, damit beide Seiten nicht auseinanderlaufen.
    """
    roh = json.dumps(text, ensure_ascii=False)
    for zeichen in "<>&\u2028\u2029":
        roh = roh.replace(zeichen, f"\\u{ord(zeichen):04x}")
    return "sha256:" + hashlib.sha256(roh.encode("utf-8")).hexdigest()


class PfoertnerFehler(Exception):
    pass


class FreigabeAbgewiesen(PfoertnerFehler):
    """Der Pförtner hat die Entscheidung nicht angenommen. Der Text ist für die Oberfläche."""


# Die datenfreien Gründe des Pförtners, in Worten für die Oberfläche.
_GRUENDE = {
    "nur leitende personen der firma entscheiden": "Nur leitende Personen können bestätigen oder ablehnen.",
    "unbekannt": "Diese Freigabe gibt es nicht (mehr).",
    "schon entschieden": "Über diese Freigabe wurde schon entschieden.",
    "abgelaufen": "Diese Freigabe ist abgelaufen.",
}

UMGEBUNG_SCHLUESSEL = "BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI"


def lies_schluessel(pfad: str) -> str:
    try:
        with open(pfad, encoding="utf-8") as f:
            schluessel = f.read().strip()
    except OSError:
        raise PfoertnerFehler("Dienstschlüssel nicht lesbar.") from None
    if len(schluessel) < 32:
        raise PfoertnerFehler("Dienstschlüssel ungültig.")
    return schluessel


class HttpPfoertner:
    def __init__(self, adresse: str = "http://127.0.0.1:8470", zeitlimit: float = 120.0, schluessel: str = "") -> None:
        self.adresse = adresse.rstrip("/")
        self.zeitlimit = zeitlimit
        self._schluessel = schluessel

    @classmethod
    def aus_umgebung(cls, umgebung: dict[str, str] | None = None) -> HttpPfoertner:
        """Adresse aus BOTICSTUDIO_PFOERTNER, Schlüssel aus der Datei in BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI."""
        u = os.environ if umgebung is None else umgebung
        pfad = u.get(UMGEBUNG_SCHLUESSEL, "")
        if not pfad:
            raise PfoertnerFehler(f"{UMGEBUNG_SCHLUESSEL} fehlt: Ohne Schlüssel nimmt der Pförtner nichts an.")
        return cls(u.get("BOTICSTUDIO_PFOERTNER", "http://127.0.0.1:8470"), schluessel=lies_schluessel(pfad))

    def __repr__(self) -> str:
        # Der Schlüssel soll auch in einer Fehlerausgabe nie auftauchen.
        return f"HttpPfoertner({self.adresse!r})"

    def _sende(self, weg: str, wert: dict[str, Any], verboten: str) -> tuple[int, dict[str, Any]]:
        kopf = {"Content-Type": "application/json"}
        if self._schluessel:
            kopf["Authorization"] = "Bearer " + self._schluessel
        req = urllib.request.Request(self.adresse + weg, data=json.dumps(wert).encode(), headers=kopf, method="POST")
        try:
            with urllib.request.urlopen(req, timeout=self.zeitlimit) as antwort:  # noqa: S310 (feste Adresse)
                return antwort.status, json.loads(antwort.read())
        except urllib.error.HTTPError as f:
            if f.code == 401:
                raise PfoertnerFehler("Der Pförtner kennt diesen Dienst nicht (Dienstschlüssel prüfen).") from None
            if f.code == 403:
                raise PfoertnerFehler(verboten) from None
            try:
                return f.code, json.loads(f.read())
            except ValueError:
                raise PfoertnerFehler(f"Pförtner antwortet mit {f.code}.") from None
        except (urllib.error.URLError, TimeoutError, OSError, ValueError):
            raise PfoertnerFehler("Pförtner nicht erreichbar.") from None

    def fuehre(self, anfrage: dict[str, Any], eingaben: dict[str, Any]) -> dict[str, Any]:
        return self._sende(
            "/v1/ausfuehren", {"anfrage": anfrage, "eingaben": eingaben},
            "Der Pförtner lässt diesen Dienst nicht für diese Firma anfragen.",
        )[1]

    _KEINE_FREIGABEN = "Der Pförtner lässt diesen Dienst keine Freigaben weitergeben (in der Datei der Dienste fehlt darf_freigeben)."

    def offene_freigaben(self, firma: str, person: str) -> list[dict[str, Any]]:
        status, antwort = self._sende("/v1/freigaben/offen", {"firma": firma, "person": person}, self._KEINE_FREIGABEN)
        if status != 200 or not isinstance(antwort.get("freigaben"), list):
            raise PfoertnerFehler("Die Freigaben ließen sich nicht lesen.")
        return antwort["freigaben"]

    def entscheide_freigabe(self, firma: str, freigabe: str, person: str, bestaetigt: bool) -> dict[str, Any]:
        status, antwort = self._sende(
            "/v1/freigaben/entscheiden",
            {"firma": firma, "freigabe": freigabe, "person": person, "bestaetigt": bestaetigt}, self._KEINE_FREIGABEN,
        )
        if status != 200:
            raise FreigabeAbgewiesen(_GRUENDE.get(str(antwort.get("fehler", "")).rpartition(": ")[2], "Die Freigabe ließ sich nicht entscheiden."))
        return antwort
