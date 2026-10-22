"""MCP-Server der Fähigkeit „Besondere Daten prüfen“.

Nur der Pförtner ruft diesen Server auf. Das Ergebnis („markierungen“)
merkt sich der Pförtner zu genau dieser Mail.
"""

from __future__ import annotations

import argparse
from typing import Any

from mcp.server.mcpserver import MCPServer
from mcp.server.mcpserver.exceptions import ToolError

from .pruefen import pruefe

server = MCPServer(name="besondere_daten_pruefen", version="1.0.0")


@server.tool(
    name="besondere_daten_pruefen",
    description="Sucht mit einer Wortliste nach Hinweisen auf besondere Daten (Art. 9 DSGVO) und markiert die Mail.",
)
def besondere_daten_pruefen(mail: dict[str, Any] | str) -> dict[str, Any]:
    try:
        return pruefe(mail)
    except ValueError as f:
        raise ToolError(str(f)) from None


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8482)
    args = parser.parse_args()
    server.run("streamable-http", host=args.host, port=args.port, stateless_http=True, json_response=True)


if __name__ == "__main__":
    main()
