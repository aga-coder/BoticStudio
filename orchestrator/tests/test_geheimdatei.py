import stat
import subprocess

import pytest

from ablauf.geheimdatei import GeheimdateiFehler, schreibe_geheim

SID = "S-1-5-21-111-222-333-1001"


class Windows:
    """Spielt whoami und icacls nach und merkt sich die Aufrufe."""

    def __init__(self, icacls_ok=True):
        self.aufrufe = []
        self.icacls_ok = icacls_ok

    def __call__(self, befehl, **_):
        self.aufrufe.append(befehl)
        if befehl[0] == "whoami":
            return subprocess.CompletedProcess(befehl, 0, f'"pc\\\\galip","{SID}"\r\n', "")
        return subprocess.CompletedProcess(befehl, 0 if self.icacls_ok else 5, "", "Zugriff verweigert")


def test_linux_0600(tmp_path):
    p = tmp_path / "g"
    schreibe_geheim(p, "geheim", ist_windows=False)
    assert stat.S_IMODE(p.stat().st_mode) == 0o600 and p.read_text() == "geheim"


def test_windows_nur_eigener_benutzer_und_erst_danach_inhalt(tmp_path):
    p = tmp_path / "g"
    inhalt_beim_icacls = []

    win = Windows()

    def ausfuehren(befehl, **kw):
        if befehl[0] == "icacls":
            inhalt_beim_icacls.append(p.read_text())
        return win(befehl, **kw)

    schreibe_geheim(p, "geheim", ist_windows=True, ausfuehren=ausfuehren)
    assert win.aufrufe[1] == ["icacls", str(p), "/inheritance:r", "/grant:r", f"*{SID}:F"]
    assert inhalt_beim_icacls == [""]  # Geheimnis kommt erst nach dem Absperren hinein
    assert p.read_text() == "geheim"


def test_windows_fehler_bricht_ab_und_hinterlaesst_nichts(tmp_path):
    p = tmp_path / "g"
    with pytest.raises(GeheimdateiFehler) as fehler:
        schreibe_geheim(p, "geheim", ist_windows=True, ausfuehren=Windows(icacls_ok=False))
    assert not p.exists()
    assert "geheim" not in str(fehler.value)
