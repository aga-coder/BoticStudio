"""MCP-Server der Fähigkeit „Systemmail senden“.

Nur der Pförtner ruft diesen Server auf. Er setzt „bereich“ (Absender),
„zugang“ (aus dem Tresor) und „ziel“ (der geprüfte Mailserver). Der Aufrufer
darf nur Vorlage, Empfänger und die Werte für die Lücken mitgeben.

Unter welcher Adresse Links in den Mails liegen dürfen, legt der Start fest
(--link-basis oder BOTICSTUDIO_LINK_BASIS), nicht der Aufrufer.
"""

from __future__ import annotations

import argparse
import os
from typing import Any

from mcp.server.mcpserver import MCPServer
from mcp.server.mcpserver.exceptions import ToolError

from .senden import Fehler, sende

server = MCPServer(name="systemmail_senden", version="1.0.0")

_einstellungen = {"link_basis": os.environ.get("BOTICSTUDIO_LINK_BASIS", "")}


@server.tool(name="systemmail_senden", description="Verschickt eine Mail von BoticStudio aus einer festen Vorlage.")
def systemmail_senden(
    bereich: str, zugang: dict[str, Any], ziel: dict[str, Any], vorlage: str, empfaenger: str, werte: dict[str, Any]
) -> dict[str, Any]:
    try:
        return sende(bereich, zugang, ziel, vorlage, empfaenger, werte, link_basis=_einstellungen["link_basis"])
    except Fehler as f:
        # Nur unsere eigenen, geprüften Texte gehen hinaus.
        raise ToolError(str(f)) from None


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8485)
    parser.add_argument("--link-basis", default=_einstellungen["link_basis"], help="z. B. https://app.boticstudio.de/")
    args = parser.parse_args()
    _einstellungen["link_basis"] = args.link_basis
    server.run("streamable-http", host=args.host, port=args.port, stateless_http=True, json_response=True)


if __name__ == "__main__":
    main()
