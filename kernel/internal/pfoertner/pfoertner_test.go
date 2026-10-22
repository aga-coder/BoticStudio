package pfoertner_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/datenbank"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	. "github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
	"github.com/aga-coder/boticstudio/kernel/internal/testdb"
)

const (
	postfachA = "postfach/info@a-test.de"
	postfachB = "postfach/info@b-test.de"
)

var ctx = context.Background()

// testZettel sind Beipackzettel nur für Tests. Die echten liegen im
// Ordner katalog/ des Repos.
var testZettel = []string{
	`{"fassung":1,"name":"lesen","version":"1","beschreibung":"liest","recht":"lesen","bereich":"postfach/ordner","risiko":"niedrig","wirkt_nach_aussen":false}`,
	`{"fassung":1,"name":"lesen","version":"2","beschreibung":"liest neuer","recht":"lesen","bereich":"postfach/ordner","risiko":"niedrig","wirkt_nach_aussen":false}`,
	`{"fassung":1,"name":"einordnen","version":"1","beschreibung":"ordnet ein","recht":"einordnen","bereich":"postfach","risiko":"mittel","wirkt_nach_aussen":false,"datenausgang":["ki_modell"]}`,
	`{"fassung":1,"name":"pruefen","version":"1","beschreibung":"prüft","recht":"keins","risiko":"niedrig","wirkt_nach_aussen":false}`,
	`{"fassung":3,"name":"vorpruefen","version":"1","beschreibung":"prüft","recht":"keins","risiko":"niedrig","wirkt_nach_aussen":false,"eingaben":["mail"],"ausgaben":["markierungen"],"prueft":["mail"]}`,
	`{"fassung":3,"name":"zuordnen","version":"1","beschreibung":"ordnet zu","recht":"einordnen","bereich":"postfach","risiko":"mittel","wirkt_nach_aussen":false,"datenausgang":["ki_modell"],"eingaben":["mail","kategorien"],"braucht_vorpruefung":["mail"]}`,
	`{"fassung":1,"name":"zuordnen_alt","version":"1","beschreibung":"ordnet zu","recht":"einordnen","bereich":"postfach","risiko":"mittel","wirkt_nach_aussen":false,"datenausgang":["ki_modell"],"eingaben":["mail","kategorien"]}`,
	`{"fassung":1,"name":"senden","version":"1","beschreibung":"sendet","recht":"senden","bereich":"postfach","risiko":"mittel","wirkt_nach_aussen":true}`,
}

type umgebung struct {
	p        *Pfoertner
	speicher Speicher
	audit    func(firma string) []auditor.Eintrag
	jetzt    time.Time
}

// jedeUmgebung führt einen Test mit jedem Speicher aus: im Arbeitsspeicher
// und in PostgreSQL (wenn eine Testdatenbank eingerichtet ist).
func jedeUmgebung(t *testing.T, test func(t *testing.T, u *umgebung), einstellen ...func(*Einstellungen)) {
	t.Run("arbeitsspeicher", func(t *testing.T) {
		s := NeuerArbeitsspeicher()
		test(t, neueUmgebung(t, s, func(f string) []auditor.Eintrag { return s.Audit(f) }, einstellen...))
	})
	t.Run("postgres", func(t *testing.T) {
		db := testdb.Neu(t)
		s := datenbank.Neu(db.Pfoertner)
		test(t, neueUmgebung(t, s, func(f string) []auditor.Eintrag {
			e, err := s.AuditEintraege(ctx, f)
			if err != nil {
				t.Fatal(err)
			}
			return e
		}, einstellen...))
	})
}

func testKatalog(t *testing.T) *katalog.Katalog {
	t.Helper()
	k := katalog.Neu()
	for _, roh := range testZettel {
		z, err := beipackzettel.Lies([]byte(roh))
		if err != nil {
			t.Fatal(err)
		}
		if err := k.Fuege(z); err != nil {
			t.Fatal(err)
		}
	}
	return k
}

func neueUmgebung(t *testing.T, s Speicher, audit func(string) []auditor.Eintrag, einstellen ...func(*Einstellungen)) *umgebung {
	t.Helper()
	u := &umgebung{speicher: s, audit: audit, jetzt: time.Date(2026, 10, 22, 20, 0, 0, 0, time.UTC)}
	einst := StandardEinstellungen()
	einst.Uhr = func() time.Time { return u.jetzt }
	for _, f := range einstellen {
		f(&einst)
	}
	u.p = Neu(einst, testKatalog(t), s)

	muss(t, u.p.LegeFirmaAn(ctx, Firma{ID: "a", Ziele: []FreigegebenesZiel{
		{Ziel: Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}, Region: RegionAusserhalbEU, EinwilligungVon: "anna", EinwilligungAm: u.jetzt},
		{Ziel: Ziel{Art: "ki_modell", Adresse: "eu-modell.example"}, Region: RegionEU},
	}}))
	muss(t, u.p.LegeFirmaAn(ctx, Firma{ID: "b"}))
	muss(t, u.p.LegePersonAn(ctx, Person{ID: "anna", Firma: "a", Rolle: RolleAdmin}))
	muss(t, u.p.LegePersonAn(ctx, Person{ID: "ben", Firma: "a", Rolle: RolleAusfuehrend}))
	muss(t, u.p.LegePersonAn(ctx, Person{ID: "bea", Firma: "b", Rolle: RolleAdmin}))
	muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "a-lesen", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"}))
	muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "a-einordnen", Firma: "a", Recht: "einordnen", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"}))
	muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "a-senden", Firma: "a", Recht: "senden", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"}))
	muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "b-lesen", Firma: "b", Recht: "lesen", Bereich: postfachB, Dauer: Dauerhaft, ErteiltVon: "bea"}))
	return u
}

func muss(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func lesenA(aufgabe string) Anfrage {
	return Anfrage{Firma: "a", Person: "ben", Aufgabe: aufgabe, Faehigkeit: "lesen", Version: "1", Bereich: postfachA + "/INBOX"}
}

func erwarte(t *testing.T, ent Entscheidung, soll Ergebnis, grundTeil string) {
	t.Helper()
	if ent.Ergebnis != soll {
		t.Fatalf("ergebnis %q (%s), erwartet %q", ent.Ergebnis, ent.Grund, soll)
	}
	if grundTeil != "" && !strings.Contains(ent.Grund, grundTeil) {
		t.Fatalf("grund %q enthält nicht %q", ent.Grund, grundTeil)
	}
}

func TestErlaubtMitPassenderErlaubnis(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		ent := u.p.Pruefe(ctx, lesenA("t1"))
		erwarte(t, ent, Erlaubt, "")
		if ent.ErlaubnisID != "a-lesen" || ent.AuditNr == 0 {
			t.Fatalf("unerwartet: %+v", ent)
		}
	})
}

func TestUnvollstaendigeOderFremdeAnfrage(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := lesenA("t1")
		a.Person = ""
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "unvollständig")

		a = lesenA("t1")
		a.Person = "niemand"
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "gehört nicht")

		a = lesenA("t1")
		a.Faehigkeit = "gibtsnicht"
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "nicht im Katalog")
	})
}

func TestBereichWirdTeilFuerTeilVerglichen(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		for _, fall := range []struct {
			bereich string
			soll    Ergebnis
		}{
			{postfachA, Erlaubt},
			{postfachA + "/INBOX", Erlaubt},
			{postfachA + "/INBOX/Unterordner", Erlaubt},
			{"postfach/info@a-test.de.boese", Abgelehnt},
			{"postfach", Abgelehnt},
			{postfachA + "/../info@b-test.de", Abgelehnt},
			{"", Abgelehnt},
		} {
			a := lesenA("t-" + fall.bereich)
			a.Bereich = fall.bereich
			if got := u.p.Pruefe(ctx, a).Ergebnis; got != fall.soll {
				t.Errorf("bereich %q: %q, erwartet %q", fall.bereich, got, fall.soll)
			}
		}
	})
}

func TestEinmaligeErlaubnisWirdVerbraucht(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "einmal", Firma: "a", Recht: "lesen", Bereich: "postfach/einmal@a-test.de", Dauer: Einmalig, ErteiltVon: "anna"}))
		a := lesenA("t1")
		a.Bereich = "postfach/einmal@a-test.de"
		erwarte(t, u.p.Pruefe(ctx, a), Erlaubt, "")
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "fehlt die Erlaubnis")
	})
}

func TestDauerhaftVorEinmalig(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "0-einmal", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Einmalig, ErteiltVon: "anna"}))
		ent := u.p.Pruefe(ctx, lesenA("t1"))
		if ent.ErlaubnisID != "a-lesen" {
			t.Fatalf("einmalige erlaubnis unnötig genutzt: %+v", ent)
		}
	})
}

func TestAbgelaufeneUndWiderrufeneErlaubnis(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "kurz", Firma: "a", Recht: "lesen", Bereich: "postfach/kurz@a-test.de", Dauer: Dauerhaft, GueltigBis: u.jetzt.Add(time.Hour), ErteiltVon: "anna"}))
		a := lesenA("t1")
		a.Bereich = "postfach/kurz@a-test.de"
		erwarte(t, u.p.Pruefe(ctx, a), Erlaubt, "")
		u.jetzt = u.jetzt.Add(time.Hour)
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "fehlt die Erlaubnis")

		muss(t, u.p.Widerrufe(ctx, "a", "a-lesen", "anna"))
		erwarte(t, u.p.Pruefe(ctx, lesenA("t2")), Abgelehnt, "fehlt die Erlaubnis")
	})
}

func TestErlaubnisNurFuerGenanntePersonen(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "chef", Firma: "a", Recht: "lesen", Bereich: "postfach/chef@a-test.de", Dauer: Dauerhaft, Fuer: []string{"anna"}, ErteiltVon: "anna"}))
		a := lesenA("t1")
		a.Bereich = "postfach/chef@a-test.de"
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "fehlt die Erlaubnis")
		a.Person = "anna"
		erwarte(t, u.p.Pruefe(ctx, a), Erlaubt, "")
	})
}

func TestNurLeitendeVergebenErlaubnisse(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		err := u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "x", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "ben"})
		if err == nil {
			t.Fatal("ausführende person durfte eine erlaubnis vergeben")
		}
		err = u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "y", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "bea"})
		if err == nil {
			t.Fatal("admin einer anderen firma durfte eine erlaubnis vergeben")
		}
		err = u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "z", Firma: "a", Recht: "keins", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"})
		if err == nil {
			t.Fatal("erlaubnis für \"keins\" angenommen")
		}
	})
}

func TestFaehigkeitOhneRecht(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		erwarte(t, u.p.Pruefe(ctx, Anfrage{Firma: "a", Person: "ben", Aufgabe: "t1", Faehigkeit: "pruefen", Version: "1"}), Erlaubt, "")
	})
}

func TestNachAussenWartetAufBestaetigung(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := Anfrage{Firma: "a", Person: "anna", Aufgabe: "t1", Faehigkeit: "senden", Version: "1", Bereich: postfachA}
		erwarte(t, u.p.Pruefe(ctx, a), Wartet, "bestätigen")
	})
}

func einordnenA(ziel string, markierungen ...string) Anfrage {
	return Anfrage{Firma: "a", Person: "ben", Aufgabe: "t-" + ziel, Faehigkeit: "einordnen", Version: "1",
		Bereich: postfachA, Ziel: &Ziel{Art: "ki_modell", Adresse: ziel}, Markierungen: markierungen}
}

func TestDatenausgang(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		erwarte(t, u.p.Pruefe(ctx, einordnenA("openrouter.ai")), Erlaubt, "")
		erwarte(t, u.p.Pruefe(ctx, einordnenA("boese.example")), Abgelehnt, "nicht auf der Liste")

		// Art. 9: nicht außerhalb der EU, innerhalb der EU schon.
		erwarte(t, u.p.Pruefe(ctx, einordnenA("openrouter.ai", "art9_verdacht")), Abgelehnt, "art9_verdacht")
		erwarte(t, u.p.Pruefe(ctx, einordnenA("eu-modell.example", "art9_verdacht")), Erlaubt, "")

		// Eine Fähigkeit ohne Datenausgang im Zettel darf nichts senden.
		a := lesenA("t9")
		a.Ziel = &Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "Beipackzettel")
	})
}

func TestZielAusserhalbEUBrauchtEinwilligung(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		err := u.p.LegeFirmaAn(ctx, Firma{ID: "c", Ziele: []FreigegebenesZiel{
			{Ziel: Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}, Region: RegionAusserhalbEU},
		}})
		if err == nil {
			t.Fatal("ziel außerhalb der eu ohne einwilligung angenommen")
		}
		err = u.p.LegeFirmaAn(ctx, Firma{ID: "d", Ziele: []FreigegebenesZiel{
			{Ziel: Ziel{Art: "ki_modell", Adresse: "x.example"}, Region: "irgendwo"},
		}})
		if err == nil {
			t.Fatal("unbekannte region ohne einwilligung angenommen")
		}
	})
}

func TestStoppen(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		erwarte(t, u.p.Pruefe(ctx, lesenA("t1")), Erlaubt, "")
		if err := u.p.Stoppe(ctx, "a", "t1", "bea"); err == nil {
			t.Fatal("person einer anderen firma durfte stoppen")
		}
		muss(t, u.p.Stoppe(ctx, "a", "t1", "ben"))
		erwarte(t, u.p.Pruefe(ctx, lesenA("t1")), Abgelehnt, "gestoppt")
		erwarte(t, u.p.Pruefe(ctx, lesenA("t2")), Erlaubt, "")
	})
}

func TestObergrenzeJeAufgabe(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		for i := 0; i < 3; i++ {
			erwarte(t, u.p.Pruefe(ctx, lesenA("t1")), Erlaubt, "")
		}
		erwarte(t, u.p.Pruefe(ctx, lesenA("t1")), Abgelehnt, "Obergrenze")
	}, func(e *Einstellungen) { e.MaxSchritte = 3 })
}

func TestVersionBleibtWaehrendDerAufgabe(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		erwarte(t, u.p.Pruefe(ctx, lesenA("t1")), Erlaubt, "")
		a := lesenA("t1")
		a.Version = "2"
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "bleibt dabei")
		a.Aufgabe = "t2"
		erwarte(t, u.p.Pruefe(ctx, a), Erlaubt, "")
	})
}

// Grundregel 12: Mandantentrennung.
func TestMandantentrennung(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {

		// Person aus A fragt im Namen von Firma B.
		a := lesenA("t1")
		a.Firma = "b"
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "gehört nicht")

		// Person aus A will das Postfach von B lesen.
		a = lesenA("t1")
		a.Bereich = postfachB
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "fehlt die Erlaubnis")

		// Firma B nutzt ein Ziel, das nur A freigegeben hat.
		erwarte(t, u.p.Pruefe(ctx, Anfrage{Firma: "b", Person: "bea", Aufgabe: "t1", Faehigkeit: "einordnen", Version: "1",
			Bereich: postfachB, Ziel: &Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}}), Abgelehnt, "")

		// Gleiche Aufgaben-ID in zwei Firmen: Stopp in A berührt B nicht.
		muss(t, u.p.Stoppe(ctx, "a", "gleich", "anna"))
		erwarte(t, u.p.Pruefe(ctx, Anfrage{Firma: "b", Person: "bea", Aufgabe: "gleich", Faehigkeit: "lesen", Version: "1", Bereich: postfachB}), Erlaubt, "")

		// Widerruf über Firmengrenzen geht nicht.
		if err := u.p.Widerrufe(ctx, "b", "a-lesen", "bea"); err == nil {
			t.Fatal("admin aus b konnte erlaubnis von a widerrufen")
		}
	})
}

func TestJedeEntscheidungImAudit(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		vorher := len(u.audit("a"))
		u.p.Pruefe(ctx, lesenA("t1"))
		a := lesenA("t1")
		a.Bereich = postfachB
		u.p.Pruefe(ctx, a)
		eintraege := u.audit("a")
		if len(eintraege) != vorher+2 {
			t.Fatalf("%d neue einträge, erwartet 2", len(eintraege)-vorher)
		}
		if err := auditor.PruefeKette(eintraege); err != nil {
			t.Fatal(err)
		}
		if err := auditor.PruefeKette(u.audit("b")); err != nil {
			t.Fatal(err)
		}
		letzter := eintraege[len(eintraege)-1]
		if letzter.Firma != "a" || letzter.Inhalt["ergebnis"] != string(Abgelehnt) {
			t.Fatalf("eintrag falsch: %+v", letzter)
		}
		for _, e := range u.audit("b") {
			if e.Firma != "b" {
				t.Fatalf("eintrag von %q in der kette von b", e.Firma)
			}
		}
	})
}

// kaputtesAudit lässt jedes Schreiben ins Audit scheitern.
type kaputtesAudit struct{ Speicher }

func (k kaputtesAudit) Vorgang(c context.Context, firma string, fn func(Vorgang) error) error {
	return k.Speicher.Vorgang(c, firma, func(v Vorgang) error { return fn(kaputterVorgang{v}) })
}

type kaputterVorgang struct{ Vorgang }

func (kaputterVorgang) Schreibe(string, map[string]string) (auditor.Eintrag, error) {
	return auditor.Eintrag{}, errors.New("platte voll")
}

func TestOhneAuditKeineAktion(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "einmal", Firma: "a", Recht: "lesen", Bereich: "postfach/einmal@a-test.de", Dauer: Einmalig, ErteiltVon: "anna"}))
		vorher := len(u.audit("a"))

		kaputt := Neu(Einstellungen{MaxSchritte: 1}, testKatalog(t), kaputtesAudit{u.speicher})
		if err := kaputt.ErteileErlaubnis(ctx, Erlaubnis{ID: "x", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Einmalig, ErteiltVon: "anna"}); err == nil {
			t.Fatal("erlaubnis ohne audit-eintrag erteilt")
		}
		a := lesenA("t1")
		a.Bereich = "postfach/einmal@a-test.de"
		erwarte(t, kaputt.Pruefe(ctx, a), Abgelehnt, "Auditor")
		if len(u.audit("a")) != vorher {
			t.Fatal("audit trotz fehler verändert")
		}

		// Nichts wurde verbraucht oder gezählt: Mit funktionierendem Audit geht
		// der Schritt genau einmal durch, auch bei Obergrenze 1.
		heil := Neu(Einstellungen{MaxSchritte: 1}, testKatalog(t), u.speicher)
		erwarte(t, heil.Pruefe(ctx, a), Erlaubt, "")
		erwarte(t, heil.Pruefe(ctx, a), Abgelehnt, "")
		if err := heil.ErteileErlaubnis(ctx, Erlaubnis{ID: "x", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Einmalig, ErteiltVon: "anna"}); err != nil {
			t.Fatal("erlaubnis x wurde trotz fehler gespeichert:", err)
		}
	})
}

func fingerabdruck(t *testing.T, wert any) string {
	t.Helper()
	fp, err := Fingerabdruck(wert)
	muss(t, err)
	return fp
}

// vorpruefen lässt die prüfende Fähigkeit einen Inhalt prüfen und merkt das Ergebnis.
func vorpruefen(t *testing.T, u *umgebung, inhalt any, markierungen ...string) {
	t.Helper()
	a := Anfrage{Firma: "a", Person: "ben", Aufgabe: "t-pruefen", Faehigkeit: "vorpruefen", Version: "1",
		Daten: map[string]string{"mail": fingerabdruck(t, inhalt)}}
	erwarte(t, u.p.Pruefe(ctx, a), Erlaubt, "")
	muss(t, u.p.MerkeVorpruefung(ctx, a, markierungen))
}

func zuordnenA(t *testing.T, faehigkeit, ziel string, inhalt any) Anfrage {
	return Anfrage{Firma: "a", Person: "ben", Aufgabe: "t-" + faehigkeit + ziel, Faehigkeit: faehigkeit, Version: "1",
		Bereich: postfachA, Ziel: &Ziel{Art: "ki_modell", Adresse: ziel},
		Daten: map[string]string{"mail": fingerabdruck(t, inhalt), "kategorien": fingerabdruck(t, "Anfrage, Beschwerde")}}
}

// Wer Daten hinausgibt, muss sagen wohin. Sonst prüft der Pförtner nichts.
func TestDatenausgangOhneZiel(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := zuordnenA(t, "zuordnen", "openrouter.ai", "hallo")
		a.Ziel = nil
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "fehlt das Ziel")
	})
}

// Art. 9, erste Filterstufe: Ungeprüftes geht nirgendwohin. Was die Prüfung
// markiert, geht nicht außerhalb der EU. Ohne Treffer geht es zum Modell.
func TestVorpruefungVorDemDatenausgang(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		harmlos := map[string]any{"betreff": "Angebot", "text": "Bitte um ein Angebot."}
		heikel := map[string]any{"betreff": "Krankmeldung", "text": "Ich bin krank."}

		erwarte(t, u.p.Pruefe(ctx, zuordnenA(t, "zuordnen", "openrouter.ai", harmlos)), Abgelehnt, "nicht vorgeprüft")

		vorpruefen(t, u, harmlos)
		vorpruefen(t, u, heikel, "art9_verdacht")
		erwarte(t, u.p.Pruefe(ctx, zuordnenA(t, "zuordnen", "openrouter.ai", harmlos)), Erlaubt, "")
		erwarte(t, u.p.Pruefe(ctx, zuordnenA(t, "zuordnen", "openrouter.ai", heikel)), Abgelehnt, "art9_verdacht")
		erwarte(t, u.p.Pruefe(ctx, zuordnenA(t, "zuordnen", "eu-modell.example", heikel)), Erlaubt, "")

		// Schon ein Zeichen anders ist ein anderer Inhalt und wieder ungeprüft.
		geaendert := map[string]any{"betreff": "Angebot", "text": "Bitte um ein Angebot!"}
		erwarte(t, u.p.Pruefe(ctx, zuordnenA(t, "zuordnen", "openrouter.ai", geaendert)), Abgelehnt, "nicht vorgeprüft")

		// Eine spätere Prüfung ohne Treffer nimmt die Markierung nicht weg.
		vorpruefen(t, u, heikel)
		erwarte(t, u.p.Pruefe(ctx, zuordnenA(t, "zuordnen", "openrouter.ai", heikel)), Abgelehnt, "art9_verdacht")

		// Die Markierung steht im Audit der Prüfung.
		eintraege := u.audit("a")
		letzte := eintraege[len(eintraege)-1]
		if letzte.Inhalt["markierungen"] != "art9_verdacht" || letzte.Inhalt["ergebnis"] != "abgelehnt" {
			t.Fatalf("audit ohne markierung: %v", letzte.Inhalt)
		}
	})
}

// Regel B: Ein alter Zettel mit Datenausgang ohne "braucht_vorpruefung"
// braucht für jede Eingabe eine Vorprüfung, auch für die Kategorien.
func TestAlterZettelBrauchtVorpruefungFuerAlles(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		mail := "Guten Tag"
		vorpruefen(t, u, mail)
		erwarte(t, u.p.Pruefe(ctx, zuordnenA(t, "zuordnen_alt", "openrouter.ai", mail)), Abgelehnt, `"kategorien" ist nicht vorgeprüft`)
		erwarte(t, u.p.Pruefe(ctx, zuordnenA(t, "zuordnen", "openrouter.ai", mail)), Erlaubt, "")
	})
}

// Ohne die echten Daten (reine Prüfanfrage) lässt sich die Vorprüfung nicht
// belegen, also wird abgelehnt.
func TestVorpruefungNurBeimAusfuehren(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := zuordnenA(t, "zuordnen", "openrouter.ai", "x")
		a.Daten = nil
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "erst beim Ausführen")
	})
}

// Nur eine Fähigkeit, die laut Zettel prüft, kann Prüfergebnisse hinterlegen.
// Und Prüfergebnisse gelten nur in der eigenen Firma.
func TestVorpruefungNurDurchPrueferUndJeFirma(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := zuordnenA(t, "zuordnen", "openrouter.ai", "x")
		if err := u.p.MerkeVorpruefung(ctx, a, nil); err == nil {
			t.Fatal("nicht-prüfende fähigkeit konnte ein prüfergebnis hinterlegen")
		}
		muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "b-einordnen", Firma: "b", Recht: "einordnen", Bereich: postfachB, Dauer: Dauerhaft, ErteiltVon: "bea"}))
		vorpruefen(t, u, "gleicher text")
		b := zuordnenA(t, "zuordnen", "openrouter.ai", "gleicher text")
		b.Firma, b.Person, b.Bereich = "b", "bea", postfachB
		b.Ziel = &Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}
		erwarte(t, u.p.Pruefe(ctx, b), Abgelehnt, "nicht vorgeprüft")
	})
}
