"""Bereitet einen Durchlauf mit einem echten Test-Postfach vor.

Liest die Zugangsdaten aus der Umgebung und schreibt in einen lokalen Ordner:
  tresor.json  Zugang zum Postfach (nur für den Besitzer lesbar, 0600)
  firma.json   Testfirma mit Erlaubnissen für genau dieses Postfach
  ablauf.json  Einstellungen für den Ablauf

Umgebung:
  BOTICSTUDIO_TEST_IMAP_BENUTZER  Adresse des Test-Postfachs
  BOTICSTUDIO_TEST_IMAP_PASSWORT  App-Passwort
  BOTICSTUDIO_TEST_IMAP_SERVER    optional, Standard imap.gmail.com

Das Passwort wird nie ausgegeben. Der Ordner darf nicht im Repo liegen.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

FIRMA = "testfirma-echt"
WURZEL = Path(__file__).resolve().parents[2]


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
    fd = os.open(tresor, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write(inhalt)
    os.chmod(tresor, 0o600)

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
    return {"tresor": str(tresor), "firma": str(ordner / "firma.json"), "ablauf": str(ordner / "ablauf.json")}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--ordner", required=True)
    args = parser.parse_args()
    print(json.dumps(bereite_vor(Path(args.ordner), dict(os.environ)), indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
