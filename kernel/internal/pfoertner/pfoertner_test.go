package pfoertner

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
)

const (
	postfachA = "postfach/info@a-test.de"
	postfachB = "postfach/info@b-test.de"
)

// testZettel sind Beipackzettel nur für Tests. Die echten liegen im
// Ordner katalog/ des Repos.
var testZettel = []string{
	`{"fassung":1,"name":"lesen","version":"1","beschreibung":"liest","recht":"lesen","bereich":"postfach/ordner","risiko":"niedrig","wirkt_nach_aussen":false}`,
	`{"fassung":1,"name":"lesen","version":"2","beschreibung":"liest neuer","recht":"lesen","bereich":"postfach/ordner","risiko":"niedrig","wirkt_nach_aussen":false}`,
	`{"fassung":1,"name":"einordnen","version":"1","beschreibung":"ordnet ein","recht":"einordnen","bereich":"postfach","risiko":"mittel","wirkt_nach_aussen":false,"datenausgang":["ki_modell"]}`,
	`{"fassung":1,"name":"pruefen","version":"1","beschreibung":"prüft","recht":"keins","risiko":"niedrig","wirkt_nach_aussen":false}`,
	`{"fassung":1,"name":"senden","version":"1","beschreibung":"sendet","recht":"senden","bereich":"postfach","risiko":"mittel","wirkt_nach_aussen":true}`,
}

type umgebung struct {
	p     *Pfoertner
	audit *auditor.Speicher
	jetzt time.Time
}

func neueUmgebung(t *testing.T) *umgebung {
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
	u := &umgebung{audit: auditor.NeuSpeicher(), jetzt: time.Date(2026, 10, 22, 20, 0, 0, 0, time.UTC)}
	u.p = Neu(StandardEinstellungen(), k, u.audit)
	u.p.jetzt = func() time.Time { return u.jetzt }

	muss(t, u.p.LegeFirmaAn(Firma{ID: "a", Ziele: []FreigegebenesZiel{
		{Ziel: Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}, Region: RegionAusserhalbEU, EinwilligungVon: "anna", EinwilligungAm: u.jetzt},
		{Ziel: Ziel{Art: "ki_modell", Adresse: "eu-modell.example"}, Region: RegionEU},
	}}))
	muss(t, u.p.LegeFirmaAn(Firma{ID: "b"}))
	muss(t, u.p.LegePersonAn(Person{ID: "anna", Firma: "a", Rolle: RolleAdmin}))
	muss(t, u.p.LegePersonAn(Person{ID: "ben", Firma: "a", Rolle: RolleAusfuehrend}))
	muss(t, u.p.LegePersonAn(Person{ID: "bea", Firma: "b", Rolle: RolleAdmin}))
	muss(t, u.p.ErteileErlaubnis(Erlaubnis{ID: "a-lesen", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"}))
	muss(t, u.p.ErteileErlaubnis(Erlaubnis{ID: "a-einordnen", Firma: "a", Recht: "einordnen", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"}))
	muss(t, u.p.ErteileErlaubnis(Erlaubnis{ID: "a-senden", Firma: "a", Recht: "senden", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"}))
	muss(t, u.p.ErteileErlaubnis(Erlaubnis{ID: "b-lesen", Firma: "b", Recht: "lesen", Bereich: postfachB, Dauer: Dauerhaft, ErteiltVon: "bea"}))
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
	u := neueUmgebung(t)
	ent := u.p.Pruefe(lesenA("t1"))
	erwarte(t, ent, Erlaubt, "")
	if ent.ErlaubnisID != "a-lesen" || ent.AuditNr == 0 {
		t.Fatalf("unerwartet: %+v", ent)
	}
}

func TestUnvollstaendigeOderFremdeAnfrage(t *testing.T) {
	u := neueUmgebung(t)
	a := lesenA("t1")
	a.Person = ""
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "unvollständig")

	a = lesenA("t1")
	a.Person = "niemand"
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "gehört nicht")

	a = lesenA("t1")
	a.Faehigkeit = "gibtsnicht"
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "nicht im Katalog")
}

func TestBereichWirdTeilFuerTeilVerglichen(t *testing.T) {
	u := neueUmgebung(t)
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
		if got := u.p.Pruefe(a).Ergebnis; got != fall.soll {
			t.Errorf("bereich %q: %q, erwartet %q", fall.bereich, got, fall.soll)
		}
	}
}

func TestEinmaligeErlaubnisWirdVerbraucht(t *testing.T) {
	u := neueUmgebung(t)
	muss(t, u.p.ErteileErlaubnis(Erlaubnis{ID: "einmal", Firma: "a", Recht: "lesen", Bereich: "postfach/einmal@a-test.de", Dauer: Einmalig, ErteiltVon: "anna"}))
	a := lesenA("t1")
	a.Bereich = "postfach/einmal@a-test.de"
	erwarte(t, u.p.Pruefe(a), Erlaubt, "")
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "fehlt die Erlaubnis")
}

func TestDauerhaftVorEinmalig(t *testing.T) {
	u := neueUmgebung(t)
	muss(t, u.p.ErteileErlaubnis(Erlaubnis{ID: "0-einmal", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Einmalig, ErteiltVon: "anna"}))
	ent := u.p.Pruefe(lesenA("t1"))
	if ent.ErlaubnisID != "a-lesen" {
		t.Fatalf("einmalige erlaubnis unnötig genutzt: %+v", ent)
	}
}

func TestAbgelaufeneUndWiderrufeneErlaubnis(t *testing.T) {
	u := neueUmgebung(t)
	muss(t, u.p.ErteileErlaubnis(Erlaubnis{ID: "kurz", Firma: "a", Recht: "lesen", Bereich: "postfach/kurz@a-test.de", Dauer: Dauerhaft, GueltigBis: u.jetzt.Add(time.Hour), ErteiltVon: "anna"}))
	a := lesenA("t1")
	a.Bereich = "postfach/kurz@a-test.de"
	erwarte(t, u.p.Pruefe(a), Erlaubt, "")
	u.jetzt = u.jetzt.Add(time.Hour)
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "fehlt die Erlaubnis")

	muss(t, u.p.Widerrufe("a", "a-lesen", "anna"))
	erwarte(t, u.p.Pruefe(lesenA("t2")), Abgelehnt, "fehlt die Erlaubnis")
}

func TestErlaubnisNurFuerGenanntePersonen(t *testing.T) {
	u := neueUmgebung(t)
	muss(t, u.p.ErteileErlaubnis(Erlaubnis{ID: "chef", Firma: "a", Recht: "lesen", Bereich: "postfach/chef@a-test.de", Dauer: Dauerhaft, Fuer: []string{"anna"}, ErteiltVon: "anna"}))
	a := lesenA("t1")
	a.Bereich = "postfach/chef@a-test.de"
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "fehlt die Erlaubnis")
	a.Person = "anna"
	erwarte(t, u.p.Pruefe(a), Erlaubt, "")
}

func TestNurLeitendeVergebenErlaubnisse(t *testing.T) {
	u := neueUmgebung(t)
	err := u.p.ErteileErlaubnis(Erlaubnis{ID: "x", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "ben"})
	if err == nil {
		t.Fatal("ausführende person durfte eine erlaubnis vergeben")
	}
	err = u.p.ErteileErlaubnis(Erlaubnis{ID: "y", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "bea"})
	if err == nil {
		t.Fatal("admin einer anderen firma durfte eine erlaubnis vergeben")
	}
	err = u.p.ErteileErlaubnis(Erlaubnis{ID: "z", Firma: "a", Recht: "keins", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"})
	if err == nil {
		t.Fatal("erlaubnis für \"keins\" angenommen")
	}
}

func TestFaehigkeitOhneRecht(t *testing.T) {
	u := neueUmgebung(t)
	erwarte(t, u.p.Pruefe(Anfrage{Firma: "a", Person: "ben", Aufgabe: "t1", Faehigkeit: "pruefen", Version: "1"}), Erlaubt, "")
}

func TestNachAussenWartetAufBestaetigung(t *testing.T) {
	u := neueUmgebung(t)
	a := Anfrage{Firma: "a", Person: "anna", Aufgabe: "t1", Faehigkeit: "senden", Version: "1", Bereich: postfachA}
	erwarte(t, u.p.Pruefe(a), Wartet, "bestätigen")
}

func einordnenA(ziel string, markierungen ...string) Anfrage {
	return Anfrage{Firma: "a", Person: "ben", Aufgabe: "t-" + ziel, Faehigkeit: "einordnen", Version: "1",
		Bereich: postfachA, Ziel: &Ziel{Art: "ki_modell", Adresse: ziel}, Markierungen: markierungen}
}

func TestDatenausgang(t *testing.T) {
	u := neueUmgebung(t)
	erwarte(t, u.p.Pruefe(einordnenA("openrouter.ai")), Erlaubt, "")
	erwarte(t, u.p.Pruefe(einordnenA("boese.example")), Abgelehnt, "nicht auf der Liste")

	// Art. 9: nicht außerhalb der EU, innerhalb der EU schon.
	erwarte(t, u.p.Pruefe(einordnenA("openrouter.ai", "art9_verdacht")), Abgelehnt, "art9_verdacht")
	erwarte(t, u.p.Pruefe(einordnenA("eu-modell.example", "art9_verdacht")), Erlaubt, "")

	// Eine Fähigkeit ohne Datenausgang im Zettel darf nichts senden.
	a := lesenA("t9")
	a.Ziel = &Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "Beipackzettel")
}

func TestZielAusserhalbEUBrauchtEinwilligung(t *testing.T) {
	u := neueUmgebung(t)
	err := u.p.LegeFirmaAn(Firma{ID: "c", Ziele: []FreigegebenesZiel{
		{Ziel: Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}, Region: RegionAusserhalbEU},
	}})
	if err == nil {
		t.Fatal("ziel außerhalb der eu ohne einwilligung angenommen")
	}
	err = u.p.LegeFirmaAn(Firma{ID: "d", Ziele: []FreigegebenesZiel{
		{Ziel: Ziel{Art: "ki_modell", Adresse: "x.example"}, Region: "irgendwo"},
	}})
	if err == nil {
		t.Fatal("unbekannte region ohne einwilligung angenommen")
	}
}

func TestStoppen(t *testing.T) {
	u := neueUmgebung(t)
	erwarte(t, u.p.Pruefe(lesenA("t1")), Erlaubt, "")
	if err := u.p.Stoppe("a", "t1", "bea"); err == nil {
		t.Fatal("person einer anderen firma durfte stoppen")
	}
	muss(t, u.p.Stoppe("a", "t1", "ben"))
	erwarte(t, u.p.Pruefe(lesenA("t1")), Abgelehnt, "gestoppt")
	erwarte(t, u.p.Pruefe(lesenA("t2")), Erlaubt, "")
}

func TestObergrenzeJeAufgabe(t *testing.T) {
	u := neueUmgebung(t)
	u.p.einst.MaxSchritte = 3
	for i := 0; i < 3; i++ {
		erwarte(t, u.p.Pruefe(lesenA("t1")), Erlaubt, "")
	}
	erwarte(t, u.p.Pruefe(lesenA("t1")), Abgelehnt, "Obergrenze")
}

func TestVersionBleibtWaehrendDerAufgabe(t *testing.T) {
	u := neueUmgebung(t)
	erwarte(t, u.p.Pruefe(lesenA("t1")), Erlaubt, "")
	a := lesenA("t1")
	a.Version = "2"
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "bleibt dabei")
	a.Aufgabe = "t2"
	erwarte(t, u.p.Pruefe(a), Erlaubt, "")
}

// Grundregel 12: Mandantentrennung.
func TestMandantentrennung(t *testing.T) {
	u := neueUmgebung(t)

	// Person aus A fragt im Namen von Firma B.
	a := lesenA("t1")
	a.Firma = "b"
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "gehört nicht")

	// Person aus A will das Postfach von B lesen.
	a = lesenA("t1")
	a.Bereich = postfachB
	erwarte(t, u.p.Pruefe(a), Abgelehnt, "fehlt die Erlaubnis")

	// Firma B nutzt ein Ziel, das nur A freigegeben hat.
	erwarte(t, u.p.Pruefe(Anfrage{Firma: "b", Person: "bea", Aufgabe: "t1", Faehigkeit: "einordnen", Version: "1",
		Bereich: postfachB, Ziel: &Ziel{Art: "ki_modell", Adresse: "openrouter.ai"}}), Abgelehnt, "")

	// Gleiche Aufgaben-ID in zwei Firmen: Stopp in A berührt B nicht.
	muss(t, u.p.Stoppe("a", "gleich", "anna"))
	erwarte(t, u.p.Pruefe(Anfrage{Firma: "b", Person: "bea", Aufgabe: "gleich", Faehigkeit: "lesen", Version: "1", Bereich: postfachB}), Erlaubt, "")

	// Widerruf über Firmengrenzen geht nicht.
	if err := u.p.Widerrufe("b", "a-lesen", "bea"); err == nil {
		t.Fatal("admin aus b konnte erlaubnis von a widerrufen")
	}
}

func TestJedeEntscheidungImAudit(t *testing.T) {
	u := neueUmgebung(t)
	vorher := len(u.audit.Eintraege())
	u.p.Pruefe(lesenA("t1"))
	a := lesenA("t1")
	a.Bereich = postfachB
	u.p.Pruefe(a)
	eintraege := u.audit.Eintraege()
	if len(eintraege) != vorher+2 {
		t.Fatalf("%d neue einträge, erwartet 2", len(eintraege)-vorher)
	}
	if err := auditor.PruefeKette(eintraege); err != nil {
		t.Fatal(err)
	}
	letzter := eintraege[len(eintraege)-1]
	if letzter.Firma != "a" || letzter.Inhalt["ergebnis"] != string(Abgelehnt) {
		t.Fatalf("eintrag falsch: %+v", letzter)
	}
}

type kaputterAuditor struct{}

func (kaputterAuditor) Schreibe(string, string, map[string]string) (auditor.Eintrag, error) {
	return auditor.Eintrag{}, errors.New("platte voll")
}

func TestOhneAuditKeineAktion(t *testing.T) {
	u := neueUmgebung(t)
	u.p.audit = kaputterAuditor{}
	if err := u.p.ErteileErlaubnis(Erlaubnis{ID: "x", Firma: "a", Recht: "lesen", Bereich: postfachA, Dauer: Einmalig, ErteiltVon: "anna"}); err == nil {
		t.Fatal("erlaubnis ohne audit-eintrag erteilt")
	}
	if u.p.erlaubnisse["x"] != nil {
		t.Fatal("erlaubnis trotz fehlendem audit gespeichert")
	}
	erwarte(t, u.p.Pruefe(lesenA("t1")), Abgelehnt, "Auditor")
	if u.p.aufgaben[schluessel("a", "t1")] != nil {
		t.Fatal("abgelehnter schritt hat die aufgabe verändert")
	}
}
