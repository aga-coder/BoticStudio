import pytest
from mcp.server.mcpserver.exceptions import ToolError

from mail_lesen import kopf_server, server


def test_fehler_geht_als_klarer_text_hinaus():
    with pytest.raises(ToolError, match="kein Postfach"):
        server.mail_lesen(bereich="kalender/x", zugang={})


def test_kopfzeilen_fehler_als_klarer_text():
    with pytest.raises(ToolError, match="kein Postfach"):
        kopf_server.mail_kopf_lesen(bereich="kalender/x", zugang={})
