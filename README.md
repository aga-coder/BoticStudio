# BoticStudio

Eine vertrauenswürdige KI-Arbeitsplattform für KMU. Der Kunde gibt jede Fähigkeit selbst frei, und jede Aktion lässt sich belegen.

Wir bauen in kleinen Stufen. Stufe 1 ist der erste Ablauf: Firma anmelden, Postfach verbinden, Mails in Anfragen, Beschwerden und Sonstiges einordnen und im Dashboard zeigen.

## Ordner

| Ordner | Inhalt |
| --- | --- |
| `kernel/` | Der Pförtner (Go). Prüft jeden Schritt, sonst nichts. |
| `faehigkeiten/` | Die Module der Fähigkeiten (Python, MCP). Jede läuft für sich. |
| `katalog/` | Die Beipackzettel der Fähigkeiten. Eine neue Fähigkeit bringt hier ihren Zettel mit. |
| `testdaten/` | Erfundene Testfirmen. Keine echten Kundendaten in diesem Repo. |

Orchestrator (Python, LangGraph) und Oberfläche (TypeScript, React) kommen in den nächsten Schritten dazu.
