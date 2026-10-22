// Package mcpanschluss ruft Fähigkeiten-Module über MCP (Streamable HTTP) auf.
//
// Nur der Pförtner benutzt diesen Anschluss. Der Orchestrator spricht nie
// direkt mit einem Modul.
package mcpanschluss

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Anschluss ruft ein Werkzeug eines MCP-Moduls auf.
type Anschluss struct {
	HTTP *http.Client
}

// Rufe verbindet sich mit dem Modul unter adresse, ruft das Werkzeug auf
// und liefert das Ergebnis als JSON. Meldet das Modul einen Fehler, kommt
// er als error zurück.
func (a Anschluss) Rufe(ctx context.Context, adresse, werkzeug string, args map[string]any) (json.RawMessage, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "boticstudio-pfoertner", Version: "0.2.0"}, nil)
	sitzung, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             adresse,
		HTTPClient:           a.HTTP,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, err
	}
	defer sitzung.Close()

	res, err := sitzung.CallTool(ctx, &mcp.CallToolParams{Name: werkzeug, Arguments: args})
	if err != nil {
		return nil, err
	}
	text := texte(res.Content)
	if res.IsError {
		// MCP-Server setzen oft "Error executing tool <name>: " davor.
		text = strings.TrimPrefix(text, "Error executing tool "+werkzeug+": ")
		if text == "" {
			text = "unbekannter fehler im modul"
		}
		return nil, errors.New(text)
	}
	if res.StructuredContent != nil {
		return json.Marshal(res.StructuredContent)
	}
	if json.Valid([]byte(text)) {
		return json.RawMessage(text), nil
	}
	return json.Marshal(text)
}

func texte(inhalt []mcp.Content) string {
	var teile []string
	for _, c := range inhalt {
		if t, ok := c.(*mcp.TextContent); ok {
			teile = append(teile, t.Text)
		}
	}
	return strings.Join(teile, "\n")
}
