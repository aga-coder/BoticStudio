import json
import stat

import pytest

from ablauf.echter_test import WURZEL, bereite_vor

UMGEBUNG = {"BOTICSTUDIO_TEST_IMAP_BENUTZER": "test@example.org", "BOTICSTUDIO_TEST_IMAP_PASSWORT": "app-pw-geheim"}


def test_schreibt_dateien_und_schuetzt_tresor(tmp_path, capsys):
    pfade = bereite_vor(tmp_path / "echt", UMGEBUNG)
    tresor = (tmp_path / "echt" / "tresor.json")
    assert stat.S_IMODE(tresor.stat().st_mode) == 0o600
    assert json.loads(tresor.read_text())["testfirma-echt"]["postfach/test@example.org"]["passwort"] == "app-pw-geheim"
    ablauf = json.loads((tmp_path / "echt" / "ablauf.json").read_text())
    assert ablauf["postfach"] == "postfach/test@example.org/INBOX"
    for name in ("firma", "ablauf"):
        assert "app-pw-geheim" not in open(pfade[name]).read()
    assert "app-pw-geheim" not in json.dumps(pfade)


def test_nicht_im_repo():
    with pytest.raises(SystemExit, match="nicht im Repo"):
        bereite_vor(WURZEL / "testdaten" / "echt", UMGEBUNG)


def test_ohne_zugangsdaten():
    with pytest.raises(SystemExit, match="fehlen"):
        bereite_vor(WURZEL.parent / "x", {})
