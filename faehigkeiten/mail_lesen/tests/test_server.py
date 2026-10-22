import pytest
from mcp.server.mcpserver.exceptions import ToolError

from mail_lesen import server


def test_fehler_geht_als_klarer_text_hinaus():
    with pytest.raises(ToolError, match="kein Postfach"):
        server.mail_lesen(bereich="kalender/x", zugang={})
