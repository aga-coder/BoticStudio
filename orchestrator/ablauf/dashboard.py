"""Dashboard, erste Stufe: zeigt die eingeordneten Mails einer Firma und
startet das Einordnen auf Knopfdruck.

    python -m ablauf.dashboard --einstellungen ../testdaten/ablauf.json

Dann im Browser http://127.0.0.1:8490 öffnen. Die Oberfläche (dashboard/)
muss vorher mit `npm run build` gebaut sein.

Umgebung wie beim Ablauf-Starter (BOTICSTUDIO_PFOERTNER, BOTICSTUDIO_ARBEITSDATEN,
BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI mit dem eigenen Schlüssel des Dashboards).

Anmeldung: Mit BOTICSTUDIO_ARBEITSDATEN braucht jede Anfrage an /api eine
Sitzung (Konten anlegen mit `python -m ablauf.konto`). Dem Pförtner nennt das
Dashboard dann die angemeldete Person. Ohne Datenbank läuft es im Testmodus
ohne Anmeldung, mit der Person aus den Einstellungen.

Grenzen dieser Stufe, bewusst:
- Der Dienst hört nur auf 127.0.0.1, nimmt nur Anfragen mit passendem Host an
  (gegen DNS-Rebinding) und verlangt für alles, was etwas auslöst, eine eigene
  Kopfzeile, die eine fremde Webseite nicht mitschicken kann.
- Eine Firma je Dienst, aus den Einstellungen.

Gespeichert ist nur ein Verweis auf jede Mail. Betreff, Absender und Datum
holt /api/koepfe bei jedem Abruf live über den Pförtner (Fähigkeit
„mail_kopf_lesen“, ohne Text) und gibt sie nur weiter, nie in einen Speicher
oder ein Log. Den Text einer Mail und Zugangsdaten liefert der Dienst nie.
"""

from __future__ import annotations

import argparse
import dataclasses
import json
import mimetypes
import os
import sys
import threading
import uuid
from datetime import datetime, timezone
from http import HTTPStatus
from http.cookies import CookieError, SimpleCookie
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from collections.abc import Callable
from typing import Any
from urllib.parse import urlsplit

from .ablaeufe import MAIL_EINORDNEN, eingabe_aus, mail_einordnen
from .einordnen import Einstellungen, _anfrage, _erlaubt, _grund
from .orchestrator import LaufUnbekannt, Orchestrator
from .stand import UMGEBUNG_SCHLUESSEL as UMGEBUNG_STAND
from .stand import PostgresStaende, StandFehler, lies_schluessel as lies_stand_schluessel
from .konten import Anmeldung, KontoFehler, PostgresKonten, Sitzung
from .pfoertner import FreigabeAbgewiesen, HttpPfoertner, Pfoertner, PfoertnerFehler, fingerabdruck
from .speicher import Einordnung, ListenSpeicher, PostgresSpeicher, Speicher

ADRESSE = "127.0.0.1"
KOPF_AKTION = "X-BoticStudio-Aktion"
KOPF_LESEN = ("mail_kopf_lesen", "1.0.0")
COOKIE = "boticstudio_sitzung"
MAX_KOERPER = 4096
# scrypt braucht je Prüfung rund 128 MiB: höchstens zwei Anmeldungen gleichzeitig.
ANMELDE_PLAETZE = threading.BoundedSemaphore(2)
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


MAX_ANZEIGE = 20_000


def _freigabe_zeile(f: dict[str, Any], lauf: Any, wartet: dict[str, Any] | None) -> dict[str, Any]:
    """Was die Oberfläche zu einer Freigabe bekommt. Bewusst eine feste Auswahl."""
    daten = f.get("daten") or {}
    anzeige = (wartet or {}).get("anzeige") or {}
    felder = []
    for name in sorted(anzeige):
        wert = anzeige[name]
        # Nur Texte lassen sich bisher nachrechnen. Alles andere gilt als nicht geprüft.
        stimmt = isinstance(wert, str) and name in daten and fingerabdruck(wert) == daten[name]
        text = wert if isinstance(wert, str) else json.dumps(wert, ensure_ascii=False)
        felder.append({"name": name, "wert": text[:MAX_ANZEIGE], "gekuerzt": len(text) > MAX_ANZEIGE, "stimmt": stimmt})
    gezeigt = {x["name"] for x in felder if x["stimmt"]}
    return {
        "id": str(f.get("id", "")),
        "faehigkeit": str(f.get("faehigkeit", "")),
        "bereich": str(f.get("bereich", "")),
        "ziel": str(f.get("ziel", "")),
        "angefragt_fuer": str(f.get("person", "")),
        "angefragt_am": f.get("angefragt_am"),
        "gueltig_bis": f.get("gueltig_bis"),
        "lauf": lauf.id if lauf else None,
        "anzeige": felder,
        # Geprüft: Es gibt den Lauf, und jede Eingabe des Schritts wird gezeigt und stimmt.
        "geprueft": bool(lauf) and set(daten) <= gezeigt and all(x["stimmt"] for x in felder),
    }


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

    def __init__(
        self,
        pf: Pfoertner,
        speicher: Speicher,
        einst: Einstellungen,
        anmeldung: Anmeldung | None = None,
        *,
        stand: Callable[[str], Any] | None = None,
        weitere_ablaeufe: dict[str, Any] | None = None,
    ) -> None:
        self.pf, self.speicher, self.einst = pf, speicher, einst
        # stand: wo wartende Läufe ihren Stand halten (siehe stand.py). Ohne Angabe nur im Arbeitsspeicher.
        self.orchestrator = Orchestrator({MAIL_EINORDNEN: mail_einordnen(pf, speicher), **(weitere_ablaeufe or {})}, stand)
        self._fortsetzung: dict[str, Any] | None = None
        self._faeden: list[threading.Thread] = []
        # None: Testmodus ohne Anmeldung (nur ohne Datenbank).
        self.anmeldung = anmeldung
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
                "letzte_fortsetzung": self._fortsetzung,
            }

    def einordnungen(self) -> list[dict[str, Any]]:
        return [als_zeile(e) for e in self.speicher.liste(self.einst.firma)]

    def _fuer(self, person: str | None) -> Einstellungen:
        """Die Einstellungen mit der angemeldeten Person. Sie steht dann im Audit des Pförtners."""
        return dataclasses.replace(self.einst, person=person) if person else self.einst

    def koepfe(self, person: str | None = None) -> dict[str, Any]:
        """Absender, Betreff und Datum, live aus dem Postfach über den Pförtner.

        Nichts davon wird gespeichert. Jeder Abruf steht im Audit des Pförtners,
        und er prüft, ob die Person dieses Postfach lesen darf.
        """
        aufgabe = "ansehen-" + uuid.uuid4().hex[:12]
        ergebnis: dict[str, Any] = {"bereich": self.einst.postfach, "koepfe": {}, "fehler": ""}
        try:
            antwort = self.pf.fuehre(
                _anfrage(self._fuer(person), aufgabe, KOPF_LESEN, bereich=self.einst.postfach),
                {"seit_tagen": 31, "hoechstens": 500},
            )
        except PfoertnerFehler as f:
            return {**ergebnis, "fehler": str(f)}
        if not _erlaubt(antwort):
            return {**ergebnis, "fehler": _grund(antwort)}
        for m in antwort.get("ergebnis", {}).get("mails", []):
            ergebnis["koepfe"][str(m.get("uid", ""))] = {
                "von": str(m.get("von", "")),
                "betreff": str(m.get("betreff", "")),
                "datum": str(m.get("datum", "")),
            }
        return ergebnis

    def freigaben(self, person: str | None = None) -> dict[str, Any]:
        """Was auf einen Menschen wartet.

        Die Liste kommt vom Pförtner. Was der Mensch dazu sehen soll, kommt aus
        dem Stand des wartenden Laufs. Das Dashboard rechnet für jedes gezeigte
        Feld den Fingerabdruck nach und vergleicht ihn mit dem, den der Pförtner
        zu dieser Freigabe gespeichert hat. Nur wenn jede Eingabe des Schritts
        gezeigt wird und stimmt, gilt die Freigabe als geprüft und lässt sich
        bestätigen. So kann niemand etwas anderes zeigen, als ausgeführt wird.
        """
        try:
            offen = self.pf.offene_freigaben(self.einst.firma, person or self.einst.person)
            wartend = {w.get("freigabe_id"): (lauf, w) for lauf in self.orchestrator.wartende(self.einst.firma) for w in lauf.wartet_auf}
        except (PfoertnerFehler, RuntimeError) as f:
            return {"freigaben": [], "fehler": str(f)}
        return {"freigaben": [_freigabe_zeile(f, *wartend.get(f.get("id"), (None, None))) for f in offen], "fehler": ""}

    def entscheide(self, freigabe: str, bestaetigt: bool, person: str | None = None) -> dict[str, Any]:
        """Gibt die Entscheidung eines Menschen an den Pförtner und stößt den Lauf wieder an.

        Bestätigen geht nur für eine geprüfte Freigabe (siehe `freigaben`).
        Ablehnen geht immer. Ob die Person entscheiden darf, prüft der Pförtner.
        """
        liste = self.freigaben(person)
        if liste["fehler"]:
            raise PfoertnerFehler(liste["fehler"])
        zeile = next((f for f in liste["freigaben"] if f["id"] == freigabe), None)
        if zeile is None:
            raise FreigabeAbgewiesen("Diese Freigabe gibt es nicht (mehr).")
        if bestaetigt and not zeile["geprueft"]:
            raise FreigabeAbgewiesen("Diese Freigabe lässt sich nicht bestätigen: Das Dashboard kann nicht belegen, was genau ausgeführt würde.")
        self.pf.entscheide_freigabe(self.einst.firma, freigabe, person or self.einst.person, bestaetigt)
        if zeile["lauf"]:
            faden = threading.Thread(target=self._setze_fort, args=(zeile["lauf"],), daemon=True)
            self._faeden = [f for f in self._faeden if f.is_alive()] + [faden]
            faden.start()
        return self.freigaben(person)

    def _setze_fort(self, lauf: str) -> None:
        eintrag: dict[str, Any] = {"lauf": lauf, "ende": None, "stand": "", "abbruch": ""}
        try:
            eintrag["stand"] = self.orchestrator.setze_fort(self.einst.firma, lauf).stand
        except PfoertnerFehler as f:
            eintrag["abbruch"] = str(f)
        except LaufUnbekannt:
            eintrag["abbruch"] = "Der Lauf ist nicht mehr bekannt."
        except Exception as f:  # noqa: BLE001
            # Kein Text der Ausnahme nach außen: Er könnte Inhalte enthalten.
            print(f"Fortsetzen abgebrochen: {type(f).__name__}", file=sys.stderr)
            eintrag["abbruch"] = "Interner Fehler beim Fortsetzen."
        eintrag["ende"] = _jetzt()
        with self._sperre:
            self._fortsetzung = eintrag

    def starte(self, person: str | None = None) -> bool:
        """Startet das Einordnen im Hintergrund. False, wenn es schon läuft."""
        with self._sperre:
            if self._laeuft:
                return False
            self._laeuft = True
            self._letzter = {"beginn": _jetzt(), "ende": None, "bericht": None, "abbruch": "", "gestartet_von": person or self.einst.person}
        self._faden = threading.Thread(target=self._lauf, args=(self._fuer(person),), daemon=True)
        self._faden.start()
        return True

    def warte(self, sekunden: float = 30) -> None:
        for faden in [self._faden, *self._faeden]:
            if faden:
                faden.join(sekunden)

    def _lauf(self, einst: Einstellungen) -> None:
        bericht: dict[str, Any] | None = None
        abbruch = ""
        try:
            b = self.orchestrator.starte(MAIL_EINORDNEN, einst.firma, einst.person, eingabe_aus(einst)).ergebnis
            bericht = {k: v for k, v in b.items() if k != "abbruch"}
            abbruch = b.get("abbruch", "")
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

        def _aus_dem_dashboard(self) -> bool:
            herkunft = self.headers.get("Origin")
            if herkunft is not None and urlsplit(herkunft).netloc not in erlaubte_hosts:
                self._json(HTTPStatus.FORBIDDEN, {"fehler": "Fremde Herkunft."})
                return False
            # Eine fremde Webseite kann diese Kopfzeile nicht ohne Rückfrage (CORS) setzen,
            # und auf die Rückfrage antwortet der Dienst nicht.
            if self.headers.get(KOPF_AKTION) != "1":
                self._json(HTTPStatus.FORBIDDEN, {"fehler": "Kopfzeile fehlt."})
                return False
            return True

        def _cookie(self) -> str:
            try:
                keks = SimpleCookie(self.headers.get("Cookie", ""))
            except CookieError:
                return ""
            return keks[COOKIE].value if COOKIE in keks else ""

        def _sitzung(self) -> Sitzung | None:
            if db.anmeldung is None:
                return None
            try:
                s = db.anmeldung.pruefe_sitzung(self._cookie())
            except RuntimeError:
                return None
            # Ein Dienst je Firma: eine Sitzung einer anderen Firma gilt hier nicht.
            return s if s and s.firma == db.einst.firma else None

        def _angemeldet(self) -> tuple[bool, str | None]:
            """(darf weiter, Person). Im Testmodus ohne Anmeldung immer erlaubt."""
            if db.anmeldung is None:
                return True, None
            s = self._sitzung()
            if s is None:
                self._json(HTTPStatus.UNAUTHORIZED, {"fehler": "Bitte anmelden.", "anmelden": True})
                return False, None
            return True, s.person

        def _ich(self) -> dict[str, Any]:
            if db.anmeldung is None:
                return {"anmeldung_noetig": False, "angemeldet": False, "person": db.einst.person, "firma": db.einst.firma}
            s = self._sitzung()
            return {"anmeldung_noetig": True, "angemeldet": s is not None,
                    "person": s.person if s else None, "firma": s.firma if s else None}

        def _koerper(self) -> dict[str, Any] | None:
            try:
                laenge = int(self.headers.get("Content-Length", "0"))
            except ValueError:
                laenge = -1
            if laenge < 0 or laenge > MAX_KOERPER:
                self._json(HTTPStatus.REQUEST_ENTITY_TOO_LARGE, {"fehler": "Anfrage zu groß."})
                return None
            try:
                wert = json.loads(self.rfile.read(laenge) or b"{}")
            except ValueError:
                wert = None
            if not isinstance(wert, dict):
                self._json(HTTPStatus.BAD_REQUEST, {"fehler": "Ungültige Anfrage."})
                return None
            return wert

        def _setze_cookie(self, wert: str, max_alter: int) -> dict[str, str]:
            return {"Set-Cookie": f"{COOKIE}={wert}; HttpOnly; SameSite=Strict; Path=/; Max-Age={max_alter}"}

        def _anmelden(self) -> None:
            if db.anmeldung is None:
                self._json(HTTPStatus.NOT_FOUND, {"fehler": "Anmeldung ist in diesem Modus aus."})
                return
            daten = self._koerper()
            if daten is None:
                return
            email, passwort = str(daten.get("email", "")), str(daten.get("passwort", ""))
            if not ANMELDE_PLAETZE.acquire(timeout=10):
                self._json(HTTPStatus.SERVICE_UNAVAILABLE, {"fehler": "Gerade zu viele Anmeldungen. Bitte gleich nochmal."})
                return
            try:
                sitzung, wert = db.anmeldung.melde_an(email, passwort)
            except KontoFehler as f:
                print("Anmeldung fehlgeschlagen.", file=sys.stderr)
                self._json(HTTPStatus.UNAUTHORIZED, {"fehler": str(f)})
                return
            except RuntimeError as f:
                self._json(HTTPStatus.SERVICE_UNAVAILABLE, {"fehler": str(f)})
                return
            finally:
                ANMELDE_PLAETZE.release()
            if sitzung.firma != db.einst.firma:
                db.anmeldung.melde_ab(wert)
                self._json(HTTPStatus.FORBIDDEN, {"fehler": "Dieses Konto gehört nicht zu dieser Firma."})
                return
            print(f"Angemeldet: {sitzung.firma}/{sitzung.person}", file=sys.stderr)
            dauer = int((sitzung.laeuft_ab - sitzung.zuletzt).total_seconds())
            self._antworte(
                HTTPStatus.OK,
                json.dumps({"anmeldung_noetig": True, "angemeldet": True, "person": sitzung.person, "firma": sitzung.firma}).encode(),
                "application/json; charset=utf-8",
                {"Cache-Control": "no-store", **self._setze_cookie(wert, dauer)},
            )

        def _abmelden(self) -> None:
            if db.anmeldung is not None:
                try:
                    db.anmeldung.melde_ab(self._cookie())
                except RuntimeError:
                    pass
            self._antworte(
                HTTPStatus.OK, b'{"angemeldet": false}', "application/json; charset=utf-8",
                {"Cache-Control": "no-store", **self._setze_cookie("", 0)},
            )

        def do_GET(self) -> None:  # noqa: N802
            if not self._host_ok():
                return
            pfad = urlsplit(self.path).path
            if pfad == "/api/ich":
                self._json(HTTPStatus.OK, self._ich())
                return
            if pfad.startswith("/api/"):
                weiter, person = self._angemeldet()
                if not weiter:
                    return
            if pfad == "/api/zustand":
                self._json(HTTPStatus.OK, db.zustand())
            elif pfad == "/api/einordnungen":
                try:
                    self._json(HTTPStatus.OK, {"einordnungen": db.einordnungen()})
                except RuntimeError as f:
                    self._json(HTTPStatus.SERVICE_UNAVAILABLE, {"fehler": str(f)})
            elif pfad == "/api/koepfe":
                # Löst einen Abruf am Postfach aus, deshalb wie der Knopf nur aus dem Dashboard.
                if self._aus_dem_dashboard():
                    self._json(HTTPStatus.OK, db.koepfe(person))
            elif pfad == "/api/freigaben":
                self._json(HTTPStatus.OK, db.freigaben(person))
            elif pfad.startswith("/api/"):
                self._json(HTTPStatus.NOT_FOUND, {"fehler": "Unbekannt."})
            else:
                self._datei(pfad)

        def do_POST(self) -> None:  # noqa: N802
            if not self._host_ok():
                return
            if not self._aus_dem_dashboard():
                return
            pfad = urlsplit(self.path).path
            if pfad == "/api/anmelden":
                self._anmelden()
                return
            if pfad == "/api/abmelden":
                self._abmelden()
                return
            weiter, person = self._angemeldet()
            if not weiter:
                return
            if pfad == "/api/freigaben/entscheiden":
                self._entscheiden(person)
                return
            if pfad != "/api/einordnen":
                self._json(HTTPStatus.NOT_FOUND, {"fehler": "Unbekannt."})
                return
            if not db.starte(person):
                self._json(HTTPStatus.CONFLICT, {"fehler": "Das Einordnen läuft schon."})
                return
            self._json(HTTPStatus.ACCEPTED, db.zustand())

        def _entscheiden(self, person: str | None) -> None:
            daten = self._koerper()
            if daten is None:
                return
            freigabe, bestaetigt = daten.get("freigabe"), daten.get("bestaetigt")
            if not isinstance(freigabe, str) or not freigabe or not isinstance(bestaetigt, bool):
                self._json(HTTPStatus.BAD_REQUEST, {"fehler": "Ungültige Anfrage."})
                return
            try:
                antwort = db.entscheide(freigabe, bestaetigt, person)
            except FreigabeAbgewiesen as f:
                self._json(HTTPStatus.CONFLICT, {"fehler": str(f)})
                return
            except PfoertnerFehler as f:
                self._json(HTTPStatus.SERVICE_UNAVAILABLE, {"fehler": str(f)})
                return
            print(f"Freigabe {'bestätigt' if bestaetigt else 'abgelehnt'}: {db.einst.firma}/{person or db.einst.person}", file=sys.stderr)
            self._json(HTTPStatus.OK, antwort)

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
    try:
        pf = HttpPfoertner.aus_umgebung()
    except PfoertnerFehler as f:
        print(f, file=sys.stderr)
        return 2
    dsn = os.environ.get("BOTICSTUDIO_ARBEITSDATEN")
    speicher: Speicher = PostgresSpeicher(dsn) if dsn else ListenSpeicher()
    konten = PostgresKonten(dsn) if dsn else None
    if not dsn:
        print("Ohne BOTICSTUDIO_ARBEITSDATEN: Testmodus ohne Anmeldung, Ergebnisse nur im Arbeitsspeicher.", file=sys.stderr)
    # Mit Datenbank und Schlüssel überstehen wartende Läufe einen Neustart.
    staende = None
    if dsn and os.environ.get(UMGEBUNG_STAND):
        try:
            staende = PostgresStaende(dsn, lies_stand_schluessel(os.environ[UMGEBUNG_STAND]))
        except StandFehler as f:
            print(f, file=sys.stderr)
            return 2
    else:
        print(f"Ohne {UMGEBUNG_STAND}: Wartende Läufe stehen nur im Arbeitsspeicher und sind nach einem Neustart weg.", file=sys.stderr)
    db = Dashboard(pf, speicher, einst, Anmeldung(konten) if konten else None, stand=staende.fuer if staende else None)
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
        if konten:
            konten.schliesse()
        if staende:
            staende.schliesse()
    return 0


if __name__ == "__main__":
    sys.exit(main())
