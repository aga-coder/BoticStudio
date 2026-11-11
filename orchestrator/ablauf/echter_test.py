"""Bereitet einen Durchlauf mit einem echten Test-Postfach vor.

Liest die Zugangsdaten aus der Umgebung und schreibt in einen lokalen Ordner:
  tresor.json  Zugang zum Postfach (nur für den Besitzer lesbar: 0600, unter Windows per Zugriffsliste)
  firma.json   Testfirma mit Erlaubnissen für genau dieses Postfach
  ablauf.json  Einstellungen für den Ablauf
  dienste.json Dienste, die den Pförtner aufrufen dürfen (nur SHA-256 der Schlüssel)
  ablauf.schluessel, dashboard.schluessel  je ein eigener Schlüssel (0600)

Umgebung:
  BOTICSTUDIO_TEST_IMAP_BENUTZER  Adresse des Test-Postfachs
  BOTICSTUDIO_TEST_IMAP_PASSWORT  App-Passwort
  BOTICSTUDIO_TEST_IMAP_SERVER    optional, Standard imap.gmail.com

Das Passwort wird nie ausgegeben. Der Ordner darf nicht im Repo liegen.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import secrets
import sys
from pathlib import Path

from ablauf.geheimdatei import schreibe_geheim
from ablauf.stand import neuer_schluessel

FIRMA = "testfirma-echt"
WURZEL = Path(__file__).resolve().parents[2]


DIENSTE = ("ablauf", "dashboard")


def _schreibe_geheim(pfad: Path, inhalt: str) -> None:
    # Unter Windows wirkt 0600 nicht; schreibe_geheim setzt dort die Zugriffsliste.
    schreibe_geheim(pfad, inhalt)


def _lege_dienste_an(ordner: Path) -> dict[str, str]:
    """Je Dienst ein neuer Schlüssel. Der Pförtner bekommt nur den SHA-256."""
    eintraege, pfade = [], {}
    for name in DIENSTE:
        schluessel = secrets.token_urlsafe(32)
        pfad = ordner / f"{name}.schluessel"
        _schreibe_geheim(pfad, schluessel)
        eintrag = {"name": name, "schluessel_sha256": hashlib.sha256(schluessel.encode()).hexdigest(), "firmen": [FIRMA]}
        if name == "dashboard":
            # Nur das Dashboard meldet Menschen an, also darf nur es Freigaben weitergeben.
            eintrag["darf_freigeben"] = True
        eintraege.append(eintrag)
        pfade[f"schluessel_{name}"] = str(pfad)
    (ordner / "dienste.json").write_text(json.dumps({"dienste": eintraege}, indent=2), encoding="utf-8")
    # Schlüssel, mit dem der Orchestrator die Stände wartender Läufe verschlüsselt.
    _schreibe_geheim(ordner / "ablaufstand.schluessel", neuer_schluessel())
    pfade["schluessel_ablaufstand"] = str(ordner / "ablaufstand.schluessel")
    return {"dienste": str(ordner / "dienste.json"), **pfade}


def bereite_vor(ordner: Path, umgebung: dict[str, str]) -> dict[str, str]:
    benutzer = umgebung.get("BOTICSTUDIO_TEST_IMAP_BENUTZER", "").strip()
    passwort = umgebung.get("BOTICSTUDIO_TEST_IMAP_PASSWORT", "")
    if not benutzer or not passwort:
        raise SystemExit("BOTICSTUDIO_TEST_IMAP_BENUTZER und BOTICSTUDIO_TEST_IMAP_PASSWORT fehlen.")
    ordner = ordner.resolve()
    if ordner == WURZEL or WURZEL in ordner.parents:
        raise SystemExit("Der Ordner darf nicht im Repo liegen.")
    ordner.mkdir(parents=True, exist_ok=True)

    postfach = f"postfach/{benutzer}"
    tresor = ordner / "tresor.json"
    inhalt = json.dumps({FIRMA: {postfach: {
        "server": umgebung.get("BOTICSTUDIO_TEST_IMAP_SERVER", "imap.gmail.com"),
        "port": 993, "benutzer": benutzer, "passwort": passwort,
    }}})
    _schreibe_geheim(tresor, inhalt)

    firma = {
        "firmen": [{"id": FIRMA, "ziele": [{"art": "ki_modell", "adresse": "openrouter.ai", "region": "ausserhalb_eu",
                                            "einwilligung_von": "admin", "einwilligung_am": "2026-10-22T20:00:00Z"}]}],
        "personen": [{"id": "admin", "firma": FIRMA, "rolle": "admin"}, {"id": "ablauf", "firma": FIRMA, "rolle": "ausfuehrend"}],
        "erlaubnisse": [
            {"id": "lesen", "firma": FIRMA, "recht": "mail.lesen", "bereich": postfach, "dauer": "dauerhaft", "erteilt_von": "admin"},
            {"id": "einordnen", "firma": FIRMA, "recht": "mail.einordnen", "bereich": postfach, "dauer": "dauerhaft", "erteilt_von": "admin"},
        ],
    }
    (ordner / "firma.json").write_text(json.dumps(firma, indent=2, ensure_ascii=False), encoding="utf-8")

    ablauf = json.loads((WURZEL / "testdaten" / "ablauf.json").read_text(encoding="utf-8"))
    ablauf.update(firma=FIRMA, person="ablauf", postfach=postfach + "/INBOX")
    (ordner / "ablauf.json").write_text(json.dumps(ablauf, indent=2, ensure_ascii=False), encoding="utf-8")
    return {"tresor": str(tresor), "firma": str(ordner / "firma.json"), "ablauf": str(ordner / "ablauf.json"), **_lege_dienste_an(ordner)}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--ordner", required=True)
    args = parser.parse_args()
    print(json.dumps(bereite_vor(Path(args.ordner), dict(os.environ)), indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
