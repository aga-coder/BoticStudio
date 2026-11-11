"""Der Orchestrator: führt feste Abläufe Schritt für Schritt aus (LangGraph).

Was er tut:
- Jeder Schritt geht an den Pförtner. Der Orchestrator entscheidet nichts
  selbst und kann sich keine Zustimmung geben.
- Sagt der Pförtner "wartet", hält der Lauf an und merkt sich seinen Stand
  (siehe stand.py). Nach der Entscheidung eines Menschen macht `setze_fort`
  genau dort weiter. Er legt dem Pförtner denselben Schritt mit denselben
  Daten noch einmal vor. Ob er jetzt laufen darf, sagt allein der Pförtner.
- Ist ein Lauf fertig, wird sein Stand gelöscht.

Was er noch nicht tut: Er sucht sich keine Fähigkeiten selbst aus dem Katalog
und plant nichts mit einem KI-Modell. Abläufe sind fest beschrieben.

Regel für Abläufe: In den Zustand eines Laufs gehört nur, was zum
Weitermachen nötig ist. Mailtexte bleiben innerhalb eines Schritts und kommen
nie in den Zustand. Zugangsdaten sieht der Orchestrator ohnehin nie.
"""

from __future__ import annotations

import re
import uuid
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any, TypedDict

from langgraph.checkpoint.base import BaseCheckpointSaver
from langgraph.graph import StateGraph
from langgraph.types import interrupt

from .pfoertner import Pfoertner
from .stand import ArbeitsspeicherStand


class Zustand(TypedDict, total=False):
    """Was ein Lauf sich merkt. Wird (verschlüsselt) gespeichert, also knapp halten."""

    firma: str
    person: str
    lauf: str
    eingabe: dict[str, Any]
    ergebnis: dict[str, Any]


@dataclass
class Schritt:
    """Antwort auf einen Schritt, nachdem der Pförtner entschieden hat."""

    stand: str  # erlaubt | abgelehnt | fehler
    ergebnis: dict[str, Any] = field(default_factory=dict)
    grund: str = ""
    audit_nr: int | None = None
    freigabe_id: str = ""

    @property
    def erlaubt(self) -> bool:
        return self.stand == "erlaubt"


def schritt(
    pf: Pfoertner,
    z: Zustand,
    faehigkeit: tuple[str, str],
    eingaben: dict[str, Any],
    *,
    bereich: str | None = None,
    ziel: dict[str, str] | None = None,
    anzeige: dict[str, Any] | None = None,
) -> Schritt:
    """Legt dem Pförtner einen Schritt vor. Bei "wartet" hält der Lauf hier an.

    `anzeige` ist, was ein Mensch zum Entscheiden sehen soll (z. B. der Text,
    der hinausgehen würde). Es wird mit dem Stand gespeichert.

    Beim Fortsetzen beginnt der Schritt von vorn: Der Pförtner bekommt
    dieselbe Anfrage noch einmal und antwortet jetzt mit erlaubt (bestätigt),
    abgelehnt oder wieder wartet (noch nicht entschieden). Ein Schritt eines
    Ablaufs darf darum vor diesem Aufruf nichts tun, was nicht wiederholt
    werden darf.
    """
    anfrage: dict[str, Any] = {
        "firma": z["firma"], "person": z["person"], "aufgabe": z["lauf"],
        "faehigkeit": faehigkeit[0], "version": faehigkeit[1],
    }
    if bereich:
        anfrage["bereich"] = bereich
    if ziel:
        anfrage["ziel"] = ziel

    antwort = pf.fuehre(anfrage, eingaben)
    ent = antwort.get("entscheidung", {})
    if ent.get("ergebnis") == "wartet":
        if not ent.get("freigabe_id"):
            return Schritt("abgelehnt", grund="Der Schritt braucht eine Bestätigung, aber der Pförtner hat keine Freigabe angelegt.")
        # Hält den Lauf hier an. Der Orchestrator gibt beim Fortsetzen nie eine
        # Antwort mit, also kommt diese Zeile nie zurück: Fortsetzen heißt nur,
        # dass der Schritt von vorn beginnt und den Pförtner neu fragt.
        interrupt({
            "freigabe_id": ent["freigabe_id"], "faehigkeit": faehigkeit[0], "bereich": bereich or "",
            "grund": str(ent.get("grund", "")), "anzeige": anzeige or {},
        })
        return Schritt("fehler", grund="Der Schritt wartet auf eine Bestätigung.")

    grund = str(antwort.get("fehler") or ent.get("grund") or "")
    gemeinsam = {"audit_nr": ent.get("audit_nr"), "freigabe_id": str(ent.get("freigabe_id", ""))}
    if ent.get("ergebnis") != "erlaubt":
        return Schritt("abgelehnt", grund=grund or "Abgelehnt.", **gemeinsam)
    if antwort.get("fehler"):
        return Schritt("fehler", grund=grund, **gemeinsam)
    return Schritt("erlaubt", ergebnis=antwort.get("ergebnis") or {}, **gemeinsam)


@dataclass
class Lauf:
    id: str
    ablauf: str
    stand: str  # fertig | wartet
    ergebnis: dict[str, Any] = field(default_factory=dict)
    # Je wartendem Schritt: freigabe_id, faehigkeit, bereich, grund, anzeige.
    wartet_auf: list[dict[str, Any]] = field(default_factory=list)


class LaufUnbekannt(Exception):
    pass


_NAME = re.compile(r"^[a-z][a-z0-9_]{0,40}$")
_LAUF = re.compile(r"^([a-z][a-z0-9_]{0,40})-[0-9a-f]{32}$")


class Orchestrator:
    """Kennt die Abläufe und führt Läufe aus.

    `ablaeufe`: Name -> Funktion, die den Ablauf als StateGraph über `Zustand` baut.
    `stand`: Firma -> Speicher für die Stände dieser Firma. Ohne Angabe nur im
    Arbeitsspeicher (nach einem Neustart sind wartende Läufe dann weg).
    """

    def __init__(
        self,
        ablaeufe: dict[str, Callable[[], StateGraph]],
        stand: Callable[[str], BaseCheckpointSaver] | None = None,
    ) -> None:
        for name in ablaeufe:
            if not _NAME.match(name):
                raise ValueError(f"Ungültiger Name für einen Ablauf: {name!r}")
        self._ablaeufe = dict(ablaeufe)
        if stand is None:
            speicher = ArbeitsspeicherStand()
            stand = lambda _firma: speicher  # noqa: E731
        self._stand = stand

    def _graph(self, ablauf: str, firma: str):
        return self._ablaeufe[ablauf]().compile(checkpointer=self._stand(firma))

    def starte(self, ablauf: str, firma: str, person: str, eingabe: dict[str, Any] | None = None) -> Lauf:
        if ablauf not in self._ablaeufe:
            raise KeyError(f"Ablauf {ablauf!r} gibt es nicht.")
        if not firma or not person:
            raise ValueError("Ein Lauf braucht Firma und Person.")
        lauf = f"{ablauf}-{uuid.uuid4().hex}"
        graph = self._graph(ablauf, firma)
        anfang: Zustand = {"firma": firma, "person": person, "lauf": lauf, "eingabe": eingabe or {}, "ergebnis": {}}
        try:
            ende = graph.invoke(anfang, _cfg(lauf))
        except BaseException:
            # Ein Lauf, der gar nicht erst in Gang kam, hinterlässt keinen Stand.
            self._stand(firma).delete_thread(lauf)
            raise
        return self._abschluss(ablauf, firma, lauf, ende)

    def setze_fort(self, firma: str, lauf: str) -> Lauf:
        """Macht einen wartenden Lauf weiter, nachdem ein Mensch entschieden hat.

        Ist noch nichts entschieden, wartet der Lauf danach einfach weiter.
        """
        treffer = _LAUF.match(lauf)
        if not treffer or treffer.group(1) not in self._ablaeufe:
            raise LaufUnbekannt(lauf)
        ablauf = treffer.group(1)
        graph = self._graph(ablauf, firma)
        bild = graph.get_state(_cfg(lauf))
        # Die Firma steht auch im Zustand: Ein Lauf lässt sich nur in seiner Firma fortsetzen.
        if not bild.next or bild.values.get("firma") != firma:
            raise LaufUnbekannt(lauf)
        return self._abschluss(ablauf, firma, lauf, graph.invoke(None, _cfg(lauf)))

    def stand(self, firma: str, lauf: str) -> Lauf:
        """Wo ein wartender Lauf steht, ohne ihn weiterzuführen."""
        treffer = _LAUF.match(lauf)
        if not treffer or treffer.group(1) not in self._ablaeufe:
            raise LaufUnbekannt(lauf)
        bild = self._graph(treffer.group(1), firma).get_state(_cfg(lauf))
        if not bild.next or bild.values.get("firma") != firma:
            raise LaufUnbekannt(lauf)
        return Lauf(lauf, treffer.group(1), "wartet", dict(bild.values.get("ergebnis") or {}), [dict(u.value) for u in bild.interrupts])

    def _abschluss(self, ablauf: str, firma: str, lauf: str, ende: dict[str, Any]) -> Lauf:
        wartet = [dict(u.value) for u in ende.get("__interrupt__", [])]
        ergebnis = dict(ende.get("ergebnis") or {})
        if wartet:
            return Lauf(lauf, ablauf, "wartet", ergebnis, wartet)
        self._stand(firma).delete_thread(lauf)
        return Lauf(lauf, ablauf, "fertig", ergebnis)


def _cfg(lauf: str) -> dict[str, Any]:
    return {"configurable": {"thread_id": lauf}}
