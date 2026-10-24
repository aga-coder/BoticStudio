"""Verbindung zum Pförtner. Der Ablauf ruft Fähigkeiten nur über ihn auf.

Der Pförtner nimmt nur Dienste an, deren Schlüssel er kennt. Den Schlüssel
liest der Dienst aus der Datei in BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI (angelegt
mit `go run ./cmd/dienstschluessel` im Ordner kernel). Er steht in keinem Log
und keiner Fehlermeldung.
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from typing import Any, Protocol


class Pfoertner(Protocol):
    def fuehre(self, anfrage: dict[str, Any], eingaben: dict[str, Any]) -> dict[str, Any]: ...


class PfoertnerFehler(Exception):
    pass


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

    def fuehre(self, anfrage: dict[str, Any], eingaben: dict[str, Any]) -> dict[str, Any]:
        daten = json.dumps({"anfrage": anfrage, "eingaben": eingaben}).encode()
        kopf = {"Content-Type": "application/json"}
        if self._schluessel:
            kopf["Authorization"] = "Bearer " + self._schluessel
        req = urllib.request.Request(self.adresse + "/v1/ausfuehren", data=daten, headers=kopf, method="POST")
        try:
            with urllib.request.urlopen(req, timeout=self.zeitlimit) as antwort:  # noqa: S310 (feste Adresse)
                return json.loads(antwort.read())
        except urllib.error.HTTPError as f:
            if f.code == 401:
                raise PfoertnerFehler("Der Pförtner kennt diesen Dienst nicht (Dienstschlüssel prüfen).") from None
            if f.code == 403:
                raise PfoertnerFehler("Der Pförtner lässt diesen Dienst nicht für diese Firma anfragen.") from None
            try:
                return json.loads(f.read())
            except ValueError:
                raise PfoertnerFehler(f"Pförtner antwortet mit {f.code}.") from None
        except (urllib.error.URLError, TimeoutError, OSError):
            raise PfoertnerFehler("Pförtner nicht erreichbar.") from None
