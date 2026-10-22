package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	k := katalog.Neu()
	z, err := beipackzettel.Lies([]byte(`{"fassung":1,"name":"lesen","version":"1","beschreibung":"liest","recht":"lesen","bereich":"postfach","risiko":"niedrig","wirkt_nach_aussen":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Fuege(z); err != nil {
		t.Fatal(err)
	}
	p := pfoertner.Neu(pfoertner.StandardEinstellungen(), k, auditor.NeuSpeicher())
	for _, err := range []error{
		p.LegeFirmaAn(pfoertner.Firma{ID: "a"}),
		p.LegePersonAn(pfoertner.Person{ID: "anna", Firma: "a", Rolle: pfoertner.RolleAdmin}),
		p.ErteileErlaubnis(pfoertner.Erlaubnis{ID: "e", Firma: "a", Recht: "lesen", Bereich: "postfach/x", Dauer: pfoertner.Dauerhaft, ErteiltVon: "anna"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	s := httptest.NewServer(Handler(p, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(s.Close)
	return s
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var antwort map[string]any
	if err := json.NewDecoder(res.Body).Decode(&antwort); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, antwort
}

func TestPruefenUndStoppenUeberHTTP(t *testing.T) {
	s := testServer(t)
	anfrage := `{"firma":"a","person":"anna","aufgabe":"t1","faehigkeit":"lesen","version":"1","bereich":"postfach/x"}`

	status, antwort := post(t, s.URL+"/v1/pruefen", anfrage)
	if status != http.StatusOK || antwort["ergebnis"] != "erlaubt" {
		t.Fatalf("%d %v", status, antwort)
	}

	status, _ = post(t, s.URL+"/v1/stoppen", `{"firma":"a","aufgabe":"t1","person":"anna"}`)
	if status != http.StatusOK {
		t.Fatalf("stopp: %d", status)
	}
	_, antwort = post(t, s.URL+"/v1/pruefen", anfrage)
	if antwort["ergebnis"] != "abgelehnt" {
		t.Fatalf("nach stopp: %v", antwort)
	}
}

func TestKaputteAnfragen(t *testing.T) {
	s := testServer(t)
	for _, body := range []string{`kein json`, `{"firma":"a","unbekannt":1}`, `{}{}`} {
		if status, _ := post(t, s.URL+"/v1/pruefen", body); status != http.StatusBadRequest {
			t.Errorf("%q: status %d, erwartet 400", body, status)
		}
	}
	if status, _ := post(t, s.URL+"/v1/stoppen", `{"firma":"a","aufgabe":"t1","person":"fremd"}`); status != http.StatusForbidden {
		t.Errorf("stopp durch fremde person: %d", status)
	}
}

func TestKeinWegZumErlaubnisVergeben(t *testing.T) {
	s := testServer(t)
	for _, weg := range []string{"/v1/erlaubnis", "/v1/erlaubnisse", "/v1/erteilen"} {
		res, err := http.Post(s.URL+weg, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s antwortet %d", weg, res.StatusCode)
		}
	}
}
