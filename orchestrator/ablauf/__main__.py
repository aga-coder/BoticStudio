"""Startet Ablauf 1 (Postfach einordnen) für eine Firma.

    python -m ablauf --einstellungen ../testdaten/ablauf.json

Umgebung:
    BOTICSTUDIO_PFOERTNER    Adresse des Pförtners (Standard http://127.0.0.1:8470)
    BOTICSTUDIO_ARBEITSDATEN Verbindung zur Datenbank (Rolle boticstudio_ablauf).
                             Fehlt sie, werden die Ergebnisse nur gezählt.
    BOTICSTUDIO_DIENSTSCHLUESSEL_DATEI  Datei mit dem Schlüssel des Ablaufs.
                             Ohne ihn nimmt der Pförtner nichts an.

Ausgegeben werden nur Zahlen, nie Mailinhalte.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from dataclasses import asdict

from .einordnen import Einstellungen, ordne_postfach_ein
from .pfoertner import HttpPfoertner, PfoertnerFehler
from .speicher import ListenSpeicher, PostgresSpeicher


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--einstellungen", required=True, help="JSON-Datei mit firma, person, postfach, ziel, modell, kategorien")
    parser.add_argument("--postfach", help="überschreibt den Bereich aus den Einstellungen")
    args = parser.parse_args()

    with open(args.einstellungen, encoding="utf-8") as f:
        werte = json.load(f)
    if args.postfach:
        werte["postfach"] = args.postfach
    einst = Einstellungen(**werte)

    dsn = os.environ.get("BOTICSTUDIO_ARBEITSDATEN")
    speicher = PostgresSpeicher(dsn) if dsn else ListenSpeicher()
    try:
        bericht = ordne_postfach_ein(HttpPfoertner.aus_umgebung(), speicher, einst)
    except PfoertnerFehler as f:
        print(json.dumps({"abbruch": str(f)}, ensure_ascii=False))
        return 1
    finally:
        if isinstance(speicher, PostgresSpeicher):
            speicher.schliesse()
    print(json.dumps(asdict(bericht), ensure_ascii=False, indent=2))
    return 0 if not bericht.abbruch else 1


if __name__ == "__main__":
    sys.exit(main())
