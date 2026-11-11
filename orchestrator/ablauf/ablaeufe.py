"""Die festen Abläufe, die der Orchestrator kennt."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import asdict

from langgraph.graph import END, START, StateGraph

from .einordnen import Einstellungen, ordne_postfach_ein
from .orchestrator import Zustand
from .pfoertner import Pfoertner
from .speicher import Speicher

MAIL_EINORDNEN = "mail_einordnen"


def mail_einordnen(pf: Pfoertner, speicher: Speicher) -> Callable[[], StateGraph]:
    """Ablauf 1: Postfach lesen, auf besondere Daten prüfen, einordnen.

    Ein einziger Schritt im Graphen, mit Absicht: Die Mails bleiben darin und
    kommen nie in den gespeicherten Zustand. Dieser Ablauf hat keine Stelle,
    an der ein Mensch bestätigen muss.
    """

    def einordnen(z: Zustand) -> Zustand:
        einst = Einstellungen(firma=z["firma"], person=z["person"], **z["eingabe"])
        return {"ergebnis": asdict(ordne_postfach_ein(pf, speicher, einst, aufgabe=z["lauf"]))}

    def baue() -> StateGraph:
        g = StateGraph(Zustand)
        g.add_node("einordnen", einordnen)
        g.add_edge(START, "einordnen")
        g.add_edge("einordnen", END)
        return g

    return baue


def eingabe_aus(einst: Einstellungen) -> dict:
    """Die Einstellungen ohne Firma und Person (die nennt der Lauf selbst)."""
    werte = asdict(einst)
    werte.pop("firma")
    werte.pop("person")
    return werte
