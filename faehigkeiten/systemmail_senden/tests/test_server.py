import pytest
from mcp.server.mcpserver.exceptions import ToolError

from systemmail_senden import server


def test_fehler_geht_als_klarer_text_hinaus():
    with pytest.raises(ToolError, match="kein Absender-Postfach"):
        server.systemmail_senden(bereich="postfach/x", zugang={}, ziel={}, vorlage="konto_bestaetigen", empfaenger="a@b.de", werte={})


def test_link_basis_kommt_vom_start_nicht_vom_aufrufer(monkeypatch):
    monkeypatch.setitem(server._einstellungen, "link_basis", "")
    with pytest.raises(ToolError, match="keine erlaubte Adresse"):
        server.systemmail_senden(
            bereich="systempost/konto@boticstudio.de",
            zugang={"server": "smtp.example.org"},
            ziel={"art": "mailserver", "adresse": "smtp.example.org"},
            vorlage="konto_bestaetigen",
            empfaenger="a@b.de",
            werte={"link": "https://boese.example/x"},
        )
