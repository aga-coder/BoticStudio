"""Dashboard, erste Stufe: zeigt die eingeordneten Mails einer Firma und
startet das Einordnen auf Knopfdruck.

    python -m ablauf.dashboard --einstellungen ../testdaten/ablauf.json

Dann im Browser http://127.0.0.1:8490 öffnen. Die Oberfläche (dashboard/)
muss vorher mit `npm run build` gebaut sein.

Umgebung wie beim Ablauf-Starter (BOTICSTUDIO_PFOERTNER, BOTICSTUDIO_ARBEITSDATEN).

Grenzen dieser Stufe, bewusst:
- Noch keine Anmeldung. Deshalb hört der Dienst nur auf 127.0.0.1, nimmt nur
  Anfragen mit passendem Host an (gegen DNS-Rebinding) und verlangt für den
  Knopf eine eigene Kopfzeile, die eine fremde Webseite nicht mitschicken kann.
- Eine Firma je Dienst, aus den Einstellungen.

Die Antworten enthalten nie Betreff, Absender oder Text einer Mail und nie
Zugangsdaten: Sie kommen nur aus den Arbeitsdaten, und dort steht nur ein
Verweis auf die Mail.
"""

from __future__ import annotations

import argparse
import json
import mimetypes
import os
import sys
import threading
from dataclasses import asdict
from datetime import datetime, timezone
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from .einordnen import Einstellungen, ordne_postfach_ein
from .pfoertner import HttpPfoertner, Pfoertner, PfoertnerFehler
from .speicher import Einordnung, ListenSpeicher, PostgresSpeicher, Speicher

ADRESSE = "127.0.0.1"
KOPF_AKTION = "X-BoticStudio-Aktion"
STANDARD_OBERFLAECHE = Path(__file__).resolve().parents[2] / "dashboard" / "dist"

SICHERHEIT = {
    "Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'",
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
    "Referrer-Policy": "no-referrer",
}


def _jetzt() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def _kategorie_namen(kategorien: list[Any]) -> list[str]:
    return [k["name"] if isinstance(k, dict) else str(k) for k in kategorien]


def als_zeile(e: Einordnung) -> dict[str, Any]:
    """Was die Oberfläche zu einer Mail bekommt. Bewusst eine feste Auswahl."""
    return {
        "bereich": e.bereich,
        "uid": e.uid,
        "stand": e.stand,
        "kategorie": e.kategorie,
        "begruendung": e.begruendung,
        "unsicher": e.unsicher,
        "art9_verdacht": bool(e.markierungen),
        "modell": e.modell,
        "nachweis": e.audit_nr,
        "eingeordnet_am": e.eingeordnet_am.isoformat(timespec="seconds") if e.eingeordnet_am else None,
    }


class Dashboard:
    """Hält den Zustand: läuft gerade ein Einordnen, wie ging das letzte aus."""

    def __init__(self, pf: Pfoertner, speicher: Speicher, einst: Einstellungen) -> None:
        self.pf, self.speicher, self.einst = pf, speicher, einst
        self._sperre = threading.Lock()
        self._laeuft = False
        self._letzter: dict[str, Any] | None = None
        self._faden: threading.Thread | None = None

    def zustand(self) -> dict[str, Any]:
        with self._sperre:
            return {
                "firma": self.einst.firma,
                "postfach": self.einst.postfach,
                "kategorien": _kategorie_namen(self.einst.kategorien),
                "laeuft": self._laeuft,
                "letzter_lauf": self._letzter,
            }

    def einordnungen(self) -> list[dict[str, Any]]:
        return [als_zeile(e) for e in self.speicher.liste(self.einst.firma)]

    def starte(self) -> bool:
        """Startet das Einordnen im Hintergrund. False, wenn es schon läuft."""
        with self._sperre:
            if self._laeuft:
                return False
            self._laeuft = True
            self._letzter = {"beginn": _jetzt(), "ende": None, "bericht": None, "abbruch": ""}
        self._faden = threading.Thread(target=self._lauf, daemon=True)
        self._faden.start()
        return True

    def warte(self, sekunden: float = 30) -> None:
        if self._faden:
            self._faden.join(sekunden)

    def _lauf(self) -> None:
        bericht: dict[str, Any] | None = None
        abbruch = ""
        try:
            b = ordne_postfach_ein(self.pf, self.speicher, self.einst)
            bericht = {k: v for k, v in asdict(b).items() if k != "abbruch"}
            abbruch = b.abbruch
        except PfoertnerFehler as f:
            abbruch = str(f)
        except Exception as f:  # noqa: BLE001
            # Kein Text der Ausnahme nach außen: Er könnte Inhalte enthalten.
            print(f"Einordnen abgebrochen: {type(f).__name__}", file=sys.stderr)
            abbruch = "Interner Fehler beim Einordnen."
        with self._sperre:
            self._laeuft = False
            self._letzter = {**(self._letzter or {}), "ende": _jetzt(), "bericht": bericht, "abbruch": abbruch}


def baue_handler(db: Dashboard, port: int, oberflaeche: Path | None) -> type[BaseHTTPRequestHandler]:
    erlaubte_hosts = {f"127.0.0.1:{port}", f"localhost:{port}"}
    wurzel = oberflaeche.resolve() if oberflaeche else None

    class Handler(BaseHTTPRequestHandler):
        server_version = "BoticStudio"
        sys_version = ""

        def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
            # Nur Anfragezeile und Status, keine Kopfzeilen.
            sys.stderr.write(format % args + "\n")

        def _antworte(self, status: int, koerper: bytes, art: str, mehr: dict[str, str] | None = None) -> None:
            self.send_response(status)
            self.send_header("Content-Type", art)
            self.send_header("Content-Length", str(len(koerper)))
            for k, v in {**SICHERHEIT, **(mehr or {})}.items():
                self.send_header(k, v)
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(koerper)

        def _json(self, status: int, wert: Any) -> None:
            self._antworte(status, json.dumps(wert, ensure_ascii=False).encode(), "application/json; charset=utf-8", {"Cache-Control": "no-store"})

        def _host_ok(self) -> bool:
            if self.headers.get("Host", "") not in erlaubte_hosts:
                self._json(HTTPStatus.FORBIDDEN, {"fehler": "Unbekannter Host."})
                return False
            return True

        def do_GET(self) -> None:  # noqa: N802
            if not self._host_ok():
                return
            pfad = urlsplit(self.path).path
            if pfad == "/api/zustand":
                self._json(HTTPStatus.OK, db.zustand())
            elif pfad == "/api/einordnungen":
                try:
                    self._json(HTTPStatus.OK, {"einordnungen": db.einordnungen()})
                except RuntimeError as f:
                    self._json(HTTPStatus.SERVICE_UNAVAILABLE, {"fehler": str(f)})
            elif pfad.startswith("/api/"):
                self._json(HTTPStatus.NOT_FOUND, {"fehler": "Unbekannt."})
            else:
                self._datei(pfad)

        def do_POST(self) -> None:  # noqa: N802
            if not self._host_ok():
                return
            herkunft = self.headers.get("Origin")
            if herkunft is not None and urlsplit(herkunft).netloc not in erlaubte_hosts:
                self._json(HTTPStatus.FORBIDDEN, {"fehler": "Fremde Herkunft."})
                return
            # Eine fremde Webseite kann diese Kopfzeile nicht ohne Rückfrage (CORS) setzen,
            # und auf die Rückfrage antwortet der Dienst nicht.
            if self.headers.get(KOPF_AKTION) != "1":
                self._json(HTTPStatus.FORBIDDEN, {"fehler": "Kopfzeile fehlt."})
                return
            if urlsplit(self.path).path != "/api/einordnen":
                self._json(HTTPStatus.NOT_FOUND, {"fehler": "Unbekannt."})
                return
            if not db.starte():
                self._json(HTTPStatus.CONFLICT, {"fehler": "Das Einordnen läuft schon."})
                return
            self._json(HTTPStatus.ACCEPTED, db.zustand())

        def _datei(self, pfad: str) -> None:
            if wurzel is None or not (wurzel / "index.html").is_file():
                self._antworte(
                    HTTPStatus.NOT_FOUND,
                    "Die Oberfläche ist noch nicht gebaut: im Ordner dashboard `npm ci && npm run build`.".encode(),
                    "text/plain; charset=utf-8",
                )
                return
            ziel = (wurzel / pfad.lstrip("/")).resolve()
            if not ziel.is_relative_to(wurzel) or not ziel.is_file():
                ziel = wurzel / "index.html"
            art = mimetypes.guess_type(ziel.name)[0] or "application/octet-stream"
            if art.startswith("text/") or art in ("application/javascript", "image/svg+xml"):
                art += "; charset=utf-8"
            self._antworte(HTTPStatus.OK, ziel.read_bytes(), art)

    return Handler


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--einstellungen", required=True, help="JSON-Datei wie beim Ablauf-Starter")
    parser.add_argument("--port", type=int, default=8490)
    parser.add_argument("--oberflaeche", type=Path, default=STANDARD_OBERFLAECHE, help="gebaute Oberfläche (dashboard/dist)")
    args = parser.parse_args()

    with open(args.einstellungen, encoding="utf-8") as f:
        einst = Einstellungen(**json.load(f))
    dsn = os.environ.get("BOTICSTUDIO_ARBEITSDATEN")
    speicher: Speicher = PostgresSpeicher(dsn) if dsn else ListenSpeicher()
    if not dsn:
        print("Ohne BOTICSTUDIO_ARBEITSDATEN: Ergebnisse nur im Arbeitsspeicher.", file=sys.stderr)
    db = Dashboard(HttpPfoertner(os.environ.get("BOTICSTUDIO_PFOERTNER", "http://127.0.0.1:8470")), speicher, einst)
    server = ThreadingHTTPServer((ADRESSE, args.port), baue_handler(db, args.port, args.oberflaeche))
    print(f"Dashboard: http://{ADRESSE}:{args.port}", file=sys.stderr)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
        if isinstance(speicher, PostgresSpeicher):
            speicher.schliesse()
    return 0


if __name__ == "__main__":
    sys.exit(main())
