"""Verbindung zum Pförtner. Der Ablauf ruft Fähigkeiten nur über ihn auf."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from typing import Any, Protocol


class Pfoertner(Protocol):
    def fuehre(self, anfrage: dict[str, Any], eingaben: dict[str, Any]) -> dict[str, Any]: ...


class PfoertnerFehler(Exception):
    pass


class HttpPfoertner:
    def __init__(self, adresse: str = "http://127.0.0.1:8470", zeitlimit: float = 120.0) -> None:
        self.adresse = adresse.rstrip("/")
        self.zeitlimit = zeitlimit

    def fuehre(self, anfrage: dict[str, Any], eingaben: dict[str, Any]) -> dict[str, Any]:
        daten = json.dumps({"anfrage": anfrage, "eingaben": eingaben}).encode()
        req = urllib.request.Request(
            self.adresse + "/v1/ausfuehren", data=daten, headers={"Content-Type": "application/json"}, method="POST"
        )
        try:
            with urllib.request.urlopen(req, timeout=self.zeitlimit) as antwort:  # noqa: S310 (feste Adresse)
                return json.loads(antwort.read())
        except urllib.error.HTTPError as f:
            try:
                return json.loads(f.read())
            except ValueError:
                raise PfoertnerFehler(f"Pförtner antwortet mit {f.code}.") from None
        except (urllib.error.URLError, TimeoutError, OSError):
            raise PfoertnerFehler("Pförtner nicht erreichbar.") from None
