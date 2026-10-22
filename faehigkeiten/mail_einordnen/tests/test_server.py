import pytest
from mcp.server.mcpserver.exceptions import ToolError

from mail_einordnen import server


def test_fehler_als_klarer_text(monkeypatch):
    monkeypatch.setenv("OPENROUTER_API_KEY", "x")
    with pytest.raises(ToolError, match="kennt das Modul nicht"):
        server.mail_einordnen(bereich="postfach/x", ziel={"art": "ki_modell", "adresse": "boese.example"},
                              mail={"betreff": "", "text": ""}, kategorien=["A"], modell="m")
