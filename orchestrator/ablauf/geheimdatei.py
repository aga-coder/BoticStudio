"""Geheimnisdateien so schreiben, dass nur der eigene Benutzer sie lesen kann.

Unter Linux/macOS reicht der Modus 0600. Unter Windows wirkt dieser Modus nicht:
Dort entscheiden die Zugriffslisten (ACL). Deshalb wird die Datei unter Windows
zuerst leer angelegt, dann mit icacls auf den eigenen Benutzer beschränkt
(geerbte Rechte entfernt) und erst danach beschrieben. Klappt das nicht, bricht
der Vorgang ab und die leere Datei wird entfernt: lieber kein Geheimnis als ein
offenes.
"""

from __future__ import annotations

import csv
import io
import os
import subprocess
from pathlib import Path


class GeheimdateiFehler(RuntimeError):
    pass


def _eigene_sid(ausfuehren=subprocess.run) -> str:
    erg = ausfuehren(["whoami", "/user", "/fo", "csv", "/nh"], capture_output=True, text=True, check=False)
    if erg.returncode != 0:
        raise GeheimdateiFehler("Eigener Windows-Benutzer nicht ermittelbar (whoami).")
    zeilen = list(csv.reader(io.StringIO(erg.stdout.strip())))
    if not zeilen or len(zeilen[0]) < 2 or not zeilen[0][1].startswith("S-1-"):
        raise GeheimdateiFehler("Eigener Windows-Benutzer nicht ermittelbar (whoami).")
    return zeilen[0][1]


def beschraenke_windows(pfad: Path, ausfuehren=subprocess.run) -> None:
    """Nur der eigene Benutzer bekommt Vollzugriff, alle geerbten Rechte fallen weg."""
    sid = _eigene_sid(ausfuehren)
    erg = ausfuehren(
        ["icacls", str(pfad), "/inheritance:r", "/grant:r", f"*{sid}:F"],
        capture_output=True, text=True, check=False,
    )
    if erg.returncode != 0:
        raise GeheimdateiFehler(f"Zugriffsrechte für {pfad.name} ließen sich nicht setzen (icacls).")


def schreibe_geheim(pfad: Path, inhalt: str, *, ist_windows: bool | None = None, ausfuehren=subprocess.run) -> None:
    if ist_windows is None:
        ist_windows = os.name == "nt"
    pfad = Path(pfad)
    fd = os.open(pfad, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.close(fd)
    if ist_windows:
        try:
            beschraenke_windows(pfad, ausfuehren)
        except GeheimdateiFehler:
            pfad.unlink(missing_ok=True)
            raise
    else:
        os.chmod(pfad, 0o600)
    with open(pfad, "w", encoding="utf-8") as f:
        f.write(inhalt)
