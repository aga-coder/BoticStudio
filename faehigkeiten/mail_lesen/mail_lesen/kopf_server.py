"""MCP-Server der Fähigkeit „Kopfzeilen lesen“ (eigener Prozess, eigener Beipackzettel).

Liest nur Absender, Betreff und Datum, nie den Text. Das Dashboard zeigt
damit live, um welche Mail es geht, ohne dass etwas davon gespeichert wird.
Nur der Pförtner ruft diesen Server auf und setzt „bereich“ und „zugang“.
"""

from __future__ import annotations

import argparse
from typing import Any

from mcp.server.mcpserver import MCPServer
from mcp.server.mcpserver.exceptions import ToolError

from .lesen import MAX_KOEPFE, Fehler, als_dict, lies_koepfe

server = MCPServer(name="mail_kopf_lesen", version="1.0.0")


@server.tool(name="mail_kopf_lesen", description="Liest Absender, Betreff und Datum der Mails der letzten Tage, ohne Text und ohne etwas zu ändern.")
def mail_kopf_lesen(bereich: str, zugang: dict[str, Any], seit_tagen: int = 7, hoechstens: int = MAX_KOEPFE) -> dict[str, Any]:
    try:
        return als_dict(lies_koepfe(bereich, zugang, seit_tagen=seit_tagen, hoechstens=hoechstens))
    except Fehler as f:
        # Wie bei „Mail lesen“: nur eigene, geprüfte Texte nach außen.
        raise ToolError(str(f)) from None


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8484)
    args = parser.parse_args()
    server.run("streamable-http", host=args.host, port=args.port, stateless_http=True, json_response=True)


if __name__ == "__main__":
    main()
