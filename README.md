# BoticStudio

Eine vertrauenswürdige KI-Arbeitsplattform für KMU. Der Kunde gibt jede Fähigkeit selbst frei, und jede Aktion lässt sich belegen.

Wir bauen in kleinen Stufen. Stufe 1 ist der erste Ablauf: Firma anmelden, Postfach verbinden, Mails in Anfragen, Beschwerden und Sonstiges einordnen und im Dashboard zeigen.

## Ordner

| Ordner | Inhalt |
| --- | --- |
| `kernel/` | Der Pförtner (Go). Prüft jeden Schritt, sonst nichts. |
| `orchestrator/` | Der Orchestrator (Python, LangGraph) und der Dienst hinter dem Dashboard. Führt feste Abläufe aus, ruft jeden Schritt über den Pförtner auf und hält an, wenn ein Mensch bestätigen muss. |
| `dashboard/` | Die Oberfläche (TypeScript, React): eingeordnete Mails und der Knopf „Jetzt einordnen“. |
| `faehigkeiten/` | Die Module der Fähigkeiten (Python, MCP). Jede läuft für sich. |
| `katalog/` | Die Beipackzettel der Fähigkeiten. Eine neue Fähigkeit bringt hier ihren Zettel mit. |
| `testdaten/` | Erfundene Testfirmen. Keine echten Kundendaten in diesem Repo. |

Die Liste wartender Freigaben im Dashboard, die Planung durch ein KI-Modell und das Dashboard mit frei anordenbaren Bausteinen kommen in den nächsten Schritten dazu.
