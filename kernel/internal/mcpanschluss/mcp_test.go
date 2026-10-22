package mcpanschluss

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type eingabe struct {
	Text string `json:"text"`
}

type ausgabe struct {
	Laenge int `json:"laenge"`
}

func testModul(t *testing.T) string {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "testmodul", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "zaehlen", Description: "zählt Zeichen"},
		func(_ context.Context, _ *mcp.CallToolRequest, e eingabe) (*mcp.CallToolResult, ausgabe, error) {
			if e.Text == "" {
				return nil, ausgabe{}, errors.New("text fehlt")
			}
			return nil, ausgabe{Laenge: len(e.Text)}, nil
		})
	s := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(s.Close)
	return s.URL
}

func TestRufeLiefertStrukturiertesErgebnis(t *testing.T) {
	adresse := testModul(t)
	roh, err := Anschluss{}.Rufe(context.Background(), adresse, "zaehlen", map[string]any{"text": "hallo"})
	if err != nil {
		t.Fatal(err)
	}
	var a ausgabe
	if err := json.Unmarshal(roh, &a); err != nil || a.Laenge != 5 {
		t.Fatalf("%s %v", roh, err)
	}
}

func TestRufeMeldetFehlerDesModuls(t *testing.T) {
	adresse := testModul(t)
	_, err := Anschluss{}.Rufe(context.Background(), adresse, "zaehlen", map[string]any{"text": ""})
	if err == nil || !strings.HasSuffix(err.Error(), "text fehlt") {
		t.Fatalf("fehler %v", err)
	}
}

func TestRufeOhneModul(t *testing.T) {
	_, err := Anschluss{}.Rufe(context.Background(), "http://127.0.0.1:1/mcp", "zaehlen", nil)
	if err == nil {
		t.Fatal("kein fehler ohne modul")
	}
}
