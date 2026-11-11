package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aga-coder/boticstudio/kernel/internal/ausfuehrung"
	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/dienste"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
	"github.com/aga-coder/boticstudio/kernel/internal/tresor"
)

var schluesselDashboard = strings.Repeat("D", 43)

type zaehlendesModul struct{ aufrufe int }

func (m *zaehlendesModul) Rufe(context.Context, string, string, map[string]any) (json.RawMessage, error) {
	m.aufrufe++
	return json.RawMessage(`{"gesendet":true}`), nil
}

// freigabeServer hat eine Fähigkeit, die nach außen wirkt, einen Dienst
// "ablauf" ohne und einen Dienst "dashboard" mit dem Recht, Freigaben weiterzugeben.
func freigabeServer(t *testing.T) (*httptest.Server, *zaehlendesModul) {
	t.Helper()
	k := katalog.Neu()
	z, err := beipackzettel.Lies([]byte(`{"fassung":1,"name":"melden","version":"1","beschreibung":"meldet","recht":"melden","bereich":"kanal","risiko":"mittel","wirkt_nach_aussen":true,"eingaben":["text"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Fuege(z); err != nil {
		t.Fatal(err)
	}
	p := pfoertner.Neu(pfoertner.StandardEinstellungen(), k, pfoertner.NeuerArbeitsspeicher())
	ctx := context.Background()
	for _, err := range []error{
		p.LegeFirmaAn(ctx, pfoertner.Firma{ID: "a"}),
		p.LegePersonAn(ctx, pfoertner.Person{ID: "anna", Firma: "a", Rolle: pfoertner.RolleLeitend}),
		p.LegePersonAn(ctx, pfoertner.Person{ID: "ben", Firma: "a", Rolle: pfoertner.RolleAusfuehrend}),
		p.ErteileErlaubnis(ctx, pfoertner.Erlaubnis{ID: "e", Firma: "a", Recht: "melden", Bereich: "kanal/x", Dauer: pfoertner.Dauerhaft, ErteiltVon: "anna"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	m := &zaehlendesModul{}
	f := &ausfuehrung.Ausfuehrer{Pfoertner: p, Katalog: k, Tresor: tresor.NeuSpeicher(), Aufrufer: m, Module: map[string]string{"melden@1": "http://melden"}}
	d, err := dienste.Neu(dienste.Datei{Dienste: []dienste.Eintrag{
		{Name: "ablauf", SchluesselSHA256: hashVon(schluesselA), Firmen: []string{"a"}},
		{Name: "fremd", SchluesselSHA256: hashVon(schluesselB), Firmen: []string{"b"}, DarfFreigeben: true},
		{Name: "dashboard", SchluesselSHA256: hashVon(schluesselDashboard), Firmen: []string{"a"}, DarfFreigeben: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(Handler(p, f, d, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(s.Close)
	return s, m
}

func melden(text string) string {
	return `{"anfrage":{"firma":"a","person":"ben","aufgabe":"t1","faehigkeit":"melden","version":"1","bereich":"kanal/x"},"eingaben":{"text":"` + text + `"}}`
}

func entscheidung(t *testing.T, antwort map[string]any) (string, string) {
	t.Helper()
	ent, _ := antwort["entscheidung"].(map[string]any)
	ergebnis, _ := ent["ergebnis"].(string)
	id, _ := ent["freigabe_id"].(string)
	return ergebnis, id
}

func TestFreigabeUeberHTTP(t *testing.T) {
	s, modul := freigabeServer(t)
	dashboard := "Bearer " + schluesselDashboard

	_, antwort := post(t, s.URL+"/v1/ausfuehren", melden("hallo"))
	ergebnis, id := entscheidung(t, antwort)
	if ergebnis != "wartet" || id == "" || modul.aufrufe != 0 {
		t.Fatalf("%v, aufrufe %d", antwort, modul.aufrufe)
	}

	// Der Orchestrator kann mit seinem Schlüssel weder lesen noch entscheiden.
	entscheiden := `{"firma":"a","freigabe":"` + id + `","person":"anna","bestaetigt":true}`
	for weg, body := range map[string]string{"/v1/freigaben/offen": `{"firma":"a","person":"anna"}`, "/v1/freigaben/entscheiden": entscheiden} {
		if status, _ := post(t, s.URL+weg, body); status != http.StatusForbidden {
			t.Errorf("ablauf-schlüssel auf %s: %d", weg, status)
		}
		// Ein Dienst mit dem Recht, aber für eine andere Firma, auch nicht.
		if status, _ := sende(t, s.URL+weg, body, "Bearer "+schluesselB); status != http.StatusForbidden {
			t.Errorf("fremder dienst auf %s: %d", weg, status)
		}
	}
	if _, antwort := post(t, s.URL+"/v1/ausfuehren", melden("hallo")); modul.aufrufe != 0 {
		t.Fatalf("ohne bestätigung ausgeführt: %v", antwort)
	}

	status, antwort := sende(t, s.URL+"/v1/freigaben/offen", `{"firma":"a","person":"anna"}`, dashboard)
	liste, _ := antwort["freigaben"].([]any)
	if status != http.StatusOK || len(liste) != 1 || liste[0].(map[string]any)["id"] != id {
		t.Fatalf("%d %v", status, antwort)
	}

	// Eine ausführende Person kann nicht bestätigen, auch nicht über das Dashboard.
	status, _ = sende(t, s.URL+"/v1/freigaben/entscheiden", strings.Replace(entscheiden, "anna", "ben", 1), dashboard)
	if status != http.StatusConflict {
		t.Fatalf("ausführende person: %d", status)
	}

	status, antwort = sende(t, s.URL+"/v1/freigaben/entscheiden", entscheiden, dashboard)
	if status != http.StatusOK || antwort["stand"] != "bestaetigt" {
		t.Fatalf("%d %v", status, antwort)
	}

	// Ein anderer Text läuft mit dieser Bestätigung nicht.
	_, antwort = post(t, s.URL+"/v1/ausfuehren", melden("etwas anderes"))
	if ergebnis, _ := entscheidung(t, antwort); ergebnis != "wartet" || modul.aufrufe != 0 {
		t.Fatalf("anderer text: %v", antwort)
	}
	// Der bestätigte läuft, genau einmal.
	_, antwort = post(t, s.URL+"/v1/ausfuehren", melden("hallo"))
	if ergebnis, _ := entscheidung(t, antwort); ergebnis != "erlaubt" || modul.aufrufe != 1 || antwort["ergebnis"] == nil {
		t.Fatalf("nach bestätigung: %v, aufrufe %d", antwort, modul.aufrufe)
	}
	_, antwort = post(t, s.URL+"/v1/ausfuehren", melden("hallo"))
	if ergebnis, _ := entscheidung(t, antwort); ergebnis != "wartet" || modul.aufrufe != 1 {
		t.Fatalf("zweites mal: %v, aufrufe %d", antwort, modul.aufrufe)
	}
}
