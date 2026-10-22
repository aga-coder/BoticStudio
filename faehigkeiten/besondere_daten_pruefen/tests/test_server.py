import pytest
from mcp.server.mcpserver.exceptions import ToolError

from besondere_daten_pruefen import server


def test_ergebnis_hat_markierungen():
    assert server.besondere_daten_pruefen(mail={"betreff": "Hallo", "text": "Ich bin krank."})["markierungen"] == ["art9_verdacht"]


def test_fehler_als_klarer_text():
    with pytest.raises(ToolError, match="betreff und text"):
        server.besondere_daten_pruefen(mail=42)
