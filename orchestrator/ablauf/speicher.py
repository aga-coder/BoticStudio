"""Speicher für die Ergebnisse (Arbeitsdaten).

Gespeichert wird nur ein Verweis auf die Mail, nie Betreff, Absender oder
Text. In PostgreSQL trennt Row-Level Security die Firmen: Jeder Vorgang
setzt seine Firma, die Datenbank zeigt und nimmt nur deren Zeilen.
"""

from __future__ import annotations

import threading
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Protocol


@dataclass
class Einordnung:
    firma: str
    bereich: str
    uid: str
    message_id: str
    stand: str  # eingeordnet | nicht_eingeordnet | fehler
    aufgabe: str
    kategorie: str | None = None
    begruendung: str = ""
    unsicher: bool = True
    markierungen: list[str] = field(default_factory=list)
    modell: str = ""
    audit_nr: int | None = None
    eingeordnet_am: datetime | None = None


class Speicher(Protocol):
    def merke(self, e: Einordnung) -> None: ...

    def liste(self, firma: str, hoechstens: int = 500) -> list[Einordnung]: ...


class ListenSpeicher:
    """Für Tests."""

    def __init__(self) -> None:
        self.eintraege: list[Einordnung] = []
        self._sperre = threading.Lock()

    def merke(self, e: Einordnung) -> None:
        with self._sperre:
            e.eingeordnet_am = datetime.now(timezone.utc)
            self.eintraege = [x for x in self.eintraege if (x.firma, x.bereich, x.uid) != (e.firma, e.bereich, e.uid)]
            self.eintraege.append(e)

    def liste(self, firma: str, hoechstens: int = 500) -> list[Einordnung]:
        with self._sperre:
            eigene = [x for x in self.eintraege if x.firma == firma]
        return list(reversed(eigene))[:hoechstens]


_SPALTEN = "bereich, uid, message_id, stand, kategorie, begruendung, unsicher, markierungen, modell, aufgabe, audit_nr, eingeordnet_am"


class PostgresSpeicher:
    """Eine Verbindung, von mehreren Threads genutzt (Dashboard und Ablauf): eine Sperre hält sie auseinander."""

    def __init__(self, dsn: str) -> None:
        import psycopg

        self._sperre = threading.Lock()
        try:
            self._verbindung = psycopg.connect(dsn, autocommit=False)
        except psycopg.Error:
            # Der Text der Ausnahme kann die Verbindungsangabe enthalten.
            raise RuntimeError("Arbeitsdaten-Datenbank nicht erreichbar.") from None

    def liste(self, firma: str, hoechstens: int = 500) -> list[Einordnung]:
        import psycopg

        try:
            with self._sperre, self._verbindung.transaction(), self._verbindung.cursor() as cur:
                cur.execute("SELECT set_config('boticstudio.firma', %s, true)", (firma,))
                # Row-Level Security zeigt ohnehin nur die Zeilen dieser Firma.
                cur.execute(
                    f"SELECT {_SPALTEN} FROM arbeit.einordnungen ORDER BY eingeordnet_am DESC, uid DESC LIMIT %s",
                    (hoechstens,),
                )
                zeilen = cur.fetchall()
        except psycopg.Error:
            raise RuntimeError("Arbeitsdaten konnten nicht gelesen werden.") from None
        return [
            Einordnung(
                firma=firma, bereich=z[0], uid=z[1], message_id=z[2], stand=z[3], kategorie=z[4], begruendung=z[5],
                unsicher=z[6], markierungen=list(z[7]), modell=z[8], aufgabe=z[9], audit_nr=z[10], eingeordnet_am=z[11],
            )
            for z in zeilen
        ]

    def merke(self, e: Einordnung) -> None:
        with self._sperre, self._verbindung.transaction(), self._verbindung.cursor() as cur:
            cur.execute("SELECT set_config('boticstudio.firma', %s, true)", (e.firma,))
            cur.execute(
                """INSERT INTO arbeit.einordnungen
                   (firma, bereich, uid, message_id, stand, kategorie, begruendung, unsicher, markierungen, modell, aufgabe, audit_nr, eingeordnet_am)
                   VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, now())
                   ON CONFLICT (firma, bereich, uid) DO UPDATE SET
                     message_id = EXCLUDED.message_id, stand = EXCLUDED.stand, kategorie = EXCLUDED.kategorie,
                     begruendung = EXCLUDED.begruendung, unsicher = EXCLUDED.unsicher, markierungen = EXCLUDED.markierungen,
                     modell = EXCLUDED.modell, aufgabe = EXCLUDED.aufgabe, audit_nr = EXCLUDED.audit_nr,
                     eingeordnet_am = EXCLUDED.eingeordnet_am""",
                (
                    e.firma, e.bereich, e.uid, e.message_id, e.stand, e.kategorie, e.begruendung[:500],
                    e.unsicher, e.markierungen, e.modell, e.aufgabe, e.audit_nr,
                ),
            )

    def schliesse(self) -> None:
        self._verbindung.close()
