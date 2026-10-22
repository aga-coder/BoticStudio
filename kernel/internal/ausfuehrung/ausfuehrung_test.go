package ausfuehrung

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
	"github.com/aga-coder/boticstudio/kernel/internal/tresor"
)

const passwort = "streng-geheim-123"

// falschesModul merkt sich den Aufruf und antwortet wie eingestellt.
type falschesModul struct {
	aufrufe  int
	werkzeug string
	args     map[string]any
	antwort  string
	fehler   error
}

func (m *falschesModul) Rufe(_ context.Context, _, werkzeug string, args map[string]any) (json.RawMessage, error) {
	m.aufrufe++
	m.werkzeug, m.args = werkzeug, args
	if m.fehler != nil {
		return nil, m.fehler
	}
	return json.RawMessage(m.antwort), nil
}

type umgebung struct {
	f     *Ausfuehrer
	modul *falschesModul
	audit *auditor.Speicher
}

func neueUmgebung(t *testing.T) *umgebung {
	t.Helper()
	k := katalog.Neu()
	for _, roh := range []string{
		`{"fassung":2,"name":"lesen","version":"1","beschreibung":"liest","recht":"lesen","bereich":"postfach/ordner",
		  "risiko":"niedrig","wirkt_nach_aussen":false,"eingaben":["seit_tagen"],"zugang":"postfach"}`,
		`{"fassung":1,"name":"zaehlen","version":"1","beschreibung":"zählt","recht":"keins","risiko":"niedrig","wirkt_nach_aussen":false,"eingaben":["text"]}`,
	} {
		z, err := beipackzettel.Lies([]byte(roh))
		if err != nil {
			t.Fatal(err)
		}
		if err := k.Fuege(z); err != nil {
			t.Fatal(err)
		}
	}
	audit := auditor.NeuSpeicher()
	p := pfoertner.Neu(pfoertner.StandardEinstellungen(), k, audit)
	for _, err := range []error{
		p.LegeFirmaAn(pfoertner.Firma{ID: "a"}),
		p.LegeFirmaAn(pfoertner.Firma{ID: "b"}),
		p.LegePersonAn(pfoertner.Person{ID: "anna", Firma: "a", Rolle: pfoertner.RolleAdmin}),
		p.LegePersonAn(pfoertner.Person{ID: "bea", Firma: "b", Rolle: pfoertner.RolleAdmin}),
		p.ErteileErlaubnis(pfoertner.Erlaubnis{ID: "e", Firma: "a", Recht: "lesen", Bereich: "postfach/info@a.de", Dauer: pfoertner.Dauerhaft, ErteiltVon: "anna"}),
		p.ErteileErlaubnis(pfoertner.Erlaubnis{ID: "f", Firma: "a", Recht: "lesen", Bereich: "postfach/leer@a.de", Dauer: pfoertner.Dauerhaft, ErteiltVon: "anna"}),
		p.ErteileErlaubnis(pfoertner.Erlaubnis{ID: "g", Firma: "b", Recht: "lesen", Bereich: "postfach/info@a.de", Dauer: pfoertner.Dauerhaft, ErteiltVon: "bea"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	tr := tresor.NeuSpeicher()
	tr.Lege("a", "postfach/info@a.de", tresor.NeuGeheimnis(`{"server":"imap.a.de","benutzer":"info@a.de","passwort":"`+passwort+`"}`))
	m := &falschesModul{antwort: `{"mails":[]}`}
	return &umgebung{
		f: &Ausfuehrer{Pfoertner: p, Katalog: k, Tresor: tr, Audit: audit, Aufrufer: m,
			Module: map[string]string{"lesen@1": "http://lesen", "zaehlen@1": "http://zaehlen"}},
		modul: m, audit: audit,
	}
}

func lesen(firma, person, bereich string) Auftrag {
	return Auftrag{
		Anfrage:  pfoertner.Anfrage{Firma: firma, Person: person, Aufgabe: "t1", Faehigkeit: "lesen", Version: "1", Bereich: bereich},
		Eingaben: map[string]any{"seit_tagen": 7},
	}
}

func TestErlaubtFuehrtAusMitZugang(t *testing.T) {
	u := neueUmgebung(t)
	antwort, err := u.f.Fuehre(context.Background(), lesen("a", "anna", "postfach/info@a.de/INBOX"))
	if err != nil || antwort.Fehler != "" || string(antwort.Ergebnis) != `{"mails":[]}` {
		t.Fatalf("%v %+v", err, antwort)
	}
	if u.modul.werkzeug != "lesen" || u.modul.args["bereich"] != "postfach/info@a.de/INBOX" || u.modul.args["seit_tagen"] != 7 {
		t.Fatalf("falsche argumente: %v", u.modul.args)
	}
	zugang, _ := u.modul.args["zugang"].(map[string]any)
	if zugang["passwort"] != passwort {
		t.Fatal("zugang nicht ans modul gegeben")
	}
	// Im Audit steht kein Passwort.
	for _, e := range u.audit.Eintraege() {
		roh, _ := json.Marshal(e)
		if strings.Contains(string(roh), passwort) {
			t.Fatalf("passwort im audit: %s", roh)
		}
	}
	if err := auditor.PruefeKette(u.audit.Eintraege()); err != nil {
		t.Fatal(err)
	}
}

func TestAbgelehntRuftKeinModul(t *testing.T) {
	u := neueUmgebung(t)
	antwort, err := u.f.Fuehre(context.Background(), lesen("a", "anna", "postfach/fremd@x.de"))
	if err != nil || antwort.Entscheidung.Ergebnis != pfoertner.Abgelehnt || u.modul.aufrufe != 0 {
		t.Fatalf("%v %+v, %d aufrufe", err, antwort, u.modul.aufrufe)
	}
}

func TestEingabenNurLautBeipackzettel(t *testing.T) {
	u := neueUmgebung(t)
	for _, eingaben := range []map[string]any{
		{"zugang": map[string]any{"passwort": "x"}},
		{"bereich": "postfach/fremd@x.de"},
		{"unbekannt": 1},
	} {
		a := lesen("a", "anna", "postfach/info@a.de")
		a.Eingaben = eingaben
		if _, err := u.f.Fuehre(context.Background(), a); !errors.Is(err, ErrUngueltig) {
			t.Errorf("%v: fehler %v", eingaben, err)
		}
	}
	if u.modul.aufrufe != 0 {
		t.Fatal("modul trotz ungültiger eingaben aufgerufen")
	}
}

func TestOhneZugangKeinAufruf(t *testing.T) {
	u := neueUmgebung(t)
	antwort, _ := u.f.Fuehre(context.Background(), lesen("a", "anna", "postfach/leer@a.de"))
	if !strings.Contains(antwort.Fehler, "Zugangsdaten fehlen") || u.modul.aufrufe != 0 {
		t.Fatalf("%+v", antwort)
	}
}

// Firma B hat zwar eine Erlaubnis für denselben Pfad, aber der Zugang im
// Tresor gehört Firma A.
func TestZugangNurAusEigenerFirma(t *testing.T) {
	u := neueUmgebung(t)
	antwort, _ := u.f.Fuehre(context.Background(), lesen("b", "bea", "postfach/info@a.de"))
	if antwort.Entscheidung.Ergebnis != pfoertner.Erlaubt || !strings.Contains(antwort.Fehler, "Zugangsdaten fehlen") || u.modul.aufrufe != 0 {
		t.Fatalf("%+v", antwort)
	}
}

func TestGeheimnisImErgebnisWirdZurueckgehalten(t *testing.T) {
	u := neueUmgebung(t)
	u.modul.antwort = `{"text":"login mit ` + passwort + `"}`
	antwort, _ := u.f.Fuehre(context.Background(), lesen("a", "anna", "postfach/info@a.de"))
	if antwort.Ergebnis != nil || !strings.Contains(antwort.Fehler, "zurückgehalten") {
		t.Fatalf("%+v", antwort)
	}
}

func TestFehlerDesModulsWirdGeschwaerzt(t *testing.T) {
	u := neueUmgebung(t)
	u.modul.fehler = errors.New("anmeldung mit " + passwort + " fehlgeschlagen")
	antwort, _ := u.f.Fuehre(context.Background(), lesen("a", "anna", "postfach/info@a.de"))
	if strings.Contains(antwort.Fehler, passwort) || !strings.Contains(antwort.Fehler, tresor.Verdeckt) {
		t.Fatalf("fehler nicht geschwärzt: %q", antwort.Fehler)
	}
	for _, e := range u.audit.Eintraege() {
		roh, _ := json.Marshal(e)
		if strings.Contains(string(roh), passwort) {
			t.Fatalf("passwort im audit: %s", roh)
		}
	}
}

func TestOhneRechtKeinBereichUndKeinZugang(t *testing.T) {
	u := neueUmgebung(t)
	antwort, err := u.f.Fuehre(context.Background(), Auftrag{
		Anfrage:  pfoertner.Anfrage{Firma: "a", Person: "anna", Aufgabe: "t1", Faehigkeit: "zaehlen", Version: "1"},
		Eingaben: map[string]any{"text": "hallo"},
	})
	if err != nil || antwort.Fehler != "" {
		t.Fatalf("%v %+v", err, antwort)
	}
	if _, da := u.modul.args["zugang"]; da {
		t.Fatal("fähigkeit ohne zugang bekam zugangsdaten")
	}
	if _, da := u.modul.args["bereich"]; da {
		t.Fatal("fähigkeit ohne recht bekam einen bereich")
	}
}

func TestModulNichtEingerichtet(t *testing.T) {
	u := neueUmgebung(t)
	u.f.Module = map[string]string{}
	antwort, _ := u.f.Fuehre(context.Background(), lesen("a", "anna", "postfach/info@a.de"))
	if !strings.Contains(antwort.Fehler, "nicht eingerichtet") {
		t.Fatalf("%+v", antwort)
	}
}
