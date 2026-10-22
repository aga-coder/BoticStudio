"""MCP-Server der Fähigkeit „Mail lesen“.

Nur der Pförtner ruft diesen Server auf. Er setzt „bereich“ und „zugang“,
der Orchestrator darf nur die Eingaben aus dem Beipackzettel mitgeben.
"""

from __future__ import annotations

import argparse
from typing import Any

from mcp.server.mcpserver import MCPServer
from mcp.server.mcpserver.exceptions import ToolError

from .lesen import MAX_MAILS, Fehler, als_dict, lies

server = MCPServer(name="mail_lesen", version="1.0.0")


@server.tool(name="mail_lesen", description="Liest die Mails der letzten Tage aus einem Postfach-Ordner, ohne etwas zu ändern.")
def mail_lesen(bereich: str, zugang: dict[str, Any], seit_tagen: int = 7, hoechstens: int = MAX_MAILS) -> dict[str, Any]:
    try:
        return als_dict(lies(bereich, zugang, seit_tagen=seit_tagen, hoechstens=hoechstens))
    except Fehler as f:
        # Nur unsere eigenen, geprüften Texte gehen hinaus. Andere Ausnahmen
        # meldet der MCP-Server nur allgemein, ohne ihren Text.
        raise ToolError(str(f)) from None


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8481)
    args = parser.parse_args()
    server.run("streamable-http", host=args.host, port=args.port, stateless_http=True, json_response=True)


if __name__ == "__main__":
    main()
