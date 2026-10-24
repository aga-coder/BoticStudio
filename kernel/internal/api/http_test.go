package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// Zwei Dienste: einer für Firma a, einer nur für Firma b.
var (
	schluesselA = strings.Repeat("A", 43)
	schluesselB = strings.Repeat("B", 43)
)

func hashVon(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func testServer(t *testing.T) *httptest.Server {
	return testServerMitLog(t, io.Discard)
}

func testServerMitLog(t *testing.T, logZiel io.Writer) *httptest.Server {
	t.Helper()
	k := katalog.Neu()
	z, err := beipackzettel.Lies([]byte(`{"fassung":1,"name":"lesen","version":"1","beschreibung":"liest","recht":"lesen","bereich":"postfach","risiko":"niedrig","wirkt_nach_aussen":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Fuege(z); err != nil {
		t.Fatal(err)
	}
	p := pfoertner.Neu(pfoertner.StandardEinstellungen(), k, pfoertner.NeuerArbeitsspeicher())
	for _, err := range []error{
		p.LegeFirmaAn(context.Background(), pfoertner.Firma{ID: "a"}),
		p.LegePersonAn(context.Background(), pfoertner.Person{ID: "anna", Firma: "a", Rolle: pfoertner.RolleAdmin}),
		p.ErteileErlaubnis(context.Background(), pfoertner.Erlaubnis{ID: "e", Firma: "a", Recht: "lesen", Bereich: "postfach/x", Dauer: pfoertner.Dauerhaft, ErteiltVon: "anna"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	f := &ausfuehrung.Ausfuehrer{Pfoertner: p, Katalog: k, Tresor: tresor.NeuSpeicher()}
	d, err := dienste.Neu(dienste.Datei{Dienste: []dienste.Eintrag{
		{Name: "ablauf", SchluesselSHA256: hashVon(schluesselA), Firmen: []string{"a"}},
		{Name: "fremd", SchluesselSHA256: hashVon(schluesselB), Firmen: []string{"b"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(Handler(p, f, d, slog.New(slog.NewTextHandler(logZiel, nil))))
	t.Cleanup(s.Close)
	return s
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	return sende(t, url, body, "Bearer "+schluesselA)
}

func sende(t *testing.T, url, body, anmeldung string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if anmeldung != "" {
		req.Header.Set("Authorization", anmeldung)
	}
	res, err := http.DefaultClient.Do(req)
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
		req, _ := http.NewRequest(http.MethodPost, s.URL+weg, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+schluesselA)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s antwortet %d", weg, res.StatusCode)
		}
	}
}

func TestAusfuehrenPrueftZuerst(t *testing.T) {
	s := testServer(t)
	// Ohne Erlaubnis für den Bereich: abgelehnt, nichts ausgeführt.
	status, antwort := post(t, s.URL+"/v1/ausfuehren",
		`{"anfrage":{"firma":"a","person":"anna","aufgabe":"t1","faehigkeit":"lesen","version":"1","bereich":"postfach/y"}}`)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if ent, _ := antwort["entscheidung"].(map[string]any); ent["ergebnis"] != "abgelehnt" || antwort["ergebnis"] != nil {
		t.Fatalf("%v", antwort)
	}
	// Reservierte Eingabe: falsch gebaut.
	status, _ = post(t, s.URL+"/v1/ausfuehren",
		`{"anfrage":{"firma":"a","person":"anna","aufgabe":"t1","faehigkeit":"lesen","version":"1","bereich":"postfach/x"},"eingaben":{"zugang":"x"}}`)
	if status != http.StatusBadRequest {
		t.Fatalf("reservierte eingabe: status %d", status)
	}
}

func TestNurBekannteDienste(t *testing.T) {
	var log bytes.Buffer
	s := testServerMitLog(t, &log)
	anfrage := `{"firma":"a","person":"anna","aufgabe":"t1","faehigkeit":"lesen","version":"1","bereich":"postfach/x"}`
	for _, anmeldung := range []string{"", schluesselA, "Bearer ", "Bearer kurz", "Bearer " + schluesselA + "x", "Basic " + schluesselA, "bearer " + schluesselA} {
		for _, weg := range []string{"/v1/pruefen", "/v1/ausfuehren", "/v1/stoppen", "/v1/unbekannt"} {
			status, antwort := sende(t, s.URL+weg, anfrage, anmeldung)
			if status != http.StatusUnauthorized || antwort["fehler"] != "dienst nicht angemeldet" {
				t.Errorf("%q %s: %d %v", anmeldung, weg, status, antwort)
			}
		}
	}
	// Gesund bleibt offen.
	res, err := http.Get(s.URL + "/gesund")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("gesund: %d", res.StatusCode)
	}
	if strings.Contains(log.String(), schluesselA[:10]) {
		t.Fatal("schlüssel steht im log")
	}
}

func TestDienstNurFuerEigeneFirmen(t *testing.T) {
	var log bytes.Buffer
	s := testServerMitLog(t, &log)
	fremd := "Bearer " + schluesselB
	faelle := map[string]string{
		"/v1/pruefen":    `{"firma":"a","person":"anna","aufgabe":"t1","faehigkeit":"lesen","version":"1","bereich":"postfach/x"}`,
		"/v1/ausfuehren": `{"anfrage":{"firma":"a","person":"anna","aufgabe":"t1","faehigkeit":"lesen","version":"1","bereich":"postfach/x"}}`,
		"/v1/stoppen":    `{"firma":"a","aufgabe":"t1","person":"anna"}`,
	}
	for weg, body := range faelle {
		if status, _ := sende(t, s.URL+weg, body, fremd); status != http.StatusForbidden {
			t.Errorf("%s: fremder dienst bekam %d", weg, status)
		}
	}
	// Die Aufgabe wurde nicht gestoppt: Der richtige Dienst darf weiter.
	if _, antwort := post(t, s.URL+"/v1/pruefen", faelle["/v1/pruefen"]); antwort["ergebnis"] != "erlaubt" {
		t.Fatalf("%v", antwort)
	}
	if !strings.Contains(log.String(), "dienst=ablauf") || strings.Contains(log.String(), schluesselB[:10]) {
		t.Fatalf("log: %s", log.String())
	}
}
