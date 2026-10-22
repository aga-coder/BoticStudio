"""MCP-Server der Fähigkeit „Mail einordnen“.

Nur der Pförtner ruft diesen Server auf. Er setzt „bereich“ und „ziel“ und
ruft ihn nur, wenn die Mail vorgeprüft ist und an dieses Ziel darf.
"""

from __future__ import annotations

import argparse
from typing import Any

from mcp.server.mcpserver import MCPServer
from mcp.server.mcpserver.exceptions import ToolError

from .einordnen import Fehler, ordne_ein

server = MCPServer(name="mail_einordnen", version="1.0.0")


@server.tool(name="mail_einordnen", description="Ordnet eine Mail einer der Kategorien der Firma zu.")
def mail_einordnen(
    bereich: str, ziel: dict[str, Any], mail: dict[str, Any], kategorien: list[Any], modell: str
) -> dict[str, Any]:
    try:
        return ordne_ein(mail, kategorien, ziel, modell)
    except Fehler as f:
        raise ToolError(str(f)) from None


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8483)
    args = parser.parse_args()
    server.run("streamable-http", host=args.host, port=args.port, stateless_http=True, json_response=True)


if __name__ == "__main__":
    main()
