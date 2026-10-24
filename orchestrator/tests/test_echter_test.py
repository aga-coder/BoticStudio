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


def test_legt_dienste_mit_eigenen_schluesseln_an(tmp_path):
    import hashlib

    pfade = bereite_vor(tmp_path / "echt", UMGEBUNG)
    dienste = json.loads(open(pfade["dienste"]).read())["dienste"]
    assert [d["name"] for d in dienste] == ["ablauf", "dashboard"]
    schluessel = {}
    for d in dienste:
        datei = tmp_path / "echt" / f"{d['name']}.schluessel"
        assert stat.S_IMODE(datei.stat().st_mode) == 0o600
        schluessel[d["name"]] = datei.read_text()
        assert hashlib.sha256(schluessel[d["name"]].encode()).hexdigest() == d["schluessel_sha256"]
        assert d["firmen"] == ["testfirma-echt"]
    assert schluessel["ablauf"] != schluessel["dashboard"]
    for s in schluessel.values():
        assert s not in open(pfade["dienste"]).read() and s not in json.dumps(pfade)
