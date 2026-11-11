package pfoertner_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	. "github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
)

// sendenA ist ein Schritt, der nach außen wirkt, mit festen Daten.
func sendenA(t *testing.T, aufgabe string, inhalt any) Anfrage {
	return Anfrage{Firma: "a", Person: "ben", Aufgabe: aufgabe, Faehigkeit: "senden", Version: "1", Bereich: postfachA,
		Daten: map[string]string{"text": fingerabdruck(t, inhalt)}}
}

func wartend(t *testing.T, u *umgebung, a Anfrage) string {
	t.Helper()
	ent := u.p.Pruefe(ctx, a)
	erwarte(t, ent, Wartet, "bestätigen")
	if ent.FreigabeID == "" {
		t.Fatal("wartet ohne freigabe")
	}
	return ent.FreigabeID
}

// Der ganze Weg: wartet, ein Mensch bestätigt, genau dieser Schritt läuft einmal.
func TestFreigabeBestaetigtGenauEinenSchritt(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := sendenA(t, "t1", "Guten Tag")
		id := wartend(t, u, a)
		// Wiederholtes Anfragen legt keine zweite Freigabe an.
		if nochmal := wartend(t, u, a); nochmal != id {
			t.Fatalf("zweite freigabe %s statt %s", nochmal, id)
		}
		offen, err := u.p.OffeneFreigaben(ctx, "a", "anna")
		muss(t, err)
		if len(offen) != 1 || offen[0].ID != id || offen[0].Person != "ben" || offen[0].Daten["text"] != a.Daten["text"] {
			t.Fatalf("offen: %+v", offen)
		}

		frei, err := u.p.EntscheideFreigabe(ctx, "a", id, "anna", true)
		muss(t, err)
		if frei.Stand != FreigabeBestaetigt || frei.EntschiedenVon != "anna" {
			t.Fatalf("%+v", frei)
		}
		ent := u.p.Pruefe(ctx, a)
		erwarte(t, ent, Erlaubt, "bestätigt")
		if ent.FreigabeID != id {
			t.Fatalf("erlaubt mit freigabe %q", ent.FreigabeID)
		}
		// Einmal heißt einmal: Derselbe Schritt wartet danach wieder, mit neuer Freigabe.
		if neu := wartend(t, u, a); neu == id {
			t.Fatal("verbrauchte freigabe wurde wiederverwendet")
		}
	})
}

// Eine Freigabe gilt nur für die Daten, die der Mensch bestätigt hat.
func TestFreigabeGiltNichtFuerAndereDaten(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		id := wartend(t, u, sendenA(t, "t1", "Guten Tag"))
		_, err := u.p.EntscheideFreigabe(ctx, "a", id, "anna", true)
		muss(t, err)

		for name, anders := range map[string]Anfrage{
			"anderer text":    sendenA(t, "t1", "Überweisen Sie 5000 Euro"),
			"andere aufgabe":  sendenA(t, "t2", "Guten Tag"),
			"anderer bereich": func() Anfrage { a := sendenA(t, "t1", "Guten Tag"); a.Bereich = postfachA + "/Entwurf"; return a }(),
			"andere person":   func() Anfrage { a := sendenA(t, "t1", "Guten Tag"); a.Person = "anna"; return a }(),
		} {
			if neu := wartend(t, u, anders); neu == id {
				t.Errorf("%s: bestätigte freigabe passt", name)
			}
		}
		// Die bestätigte Freigabe ist dabei nicht verbraucht worden.
		erwarte(t, u.p.Pruefe(ctx, sendenA(t, "t1", "Guten Tag")), Erlaubt, "")
	})
}

func TestFreigabeAbgelehnt(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := sendenA(t, "t1", "Guten Tag")
		id := wartend(t, u, a)
		_, err := u.p.EntscheideFreigabe(ctx, "a", id, "anna", false)
		muss(t, err)
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "abgelehnt")
		// Eine Entscheidung steht.
		if _, err := u.p.EntscheideFreigabe(ctx, "a", id, "anna", true); !errors.Is(err, ErrFreigabe) {
			t.Fatalf("nachträglich bestätigt: %v", err)
		}
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "abgelehnt")
	})
}

// Nur leitende Menschen der eigenen Firma entscheiden.
func TestFreigabeNurDurchLeitendeDerFirma(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := sendenA(t, "t1", "Guten Tag")
		id := wartend(t, u, a)
		for _, fall := range []struct{ firma, person string }{
			{"a", "ben"},     // ausführend
			{"a", "niemand"}, // unbekannt
			{"a", "bea"},     // leitend, aber in Firma b
			{"b", "bea"},     // Firma b sieht die Freigabe von a nicht
			{"a", ""},        // ohne Person
			{"gibtsnicht", "anna"},
		} {
			if _, err := u.p.EntscheideFreigabe(ctx, fall.firma, id, fall.person, true); err == nil {
				t.Errorf("%+v durfte entscheiden", fall)
			}
		}
		if liste, _ := u.p.OffeneFreigaben(ctx, "b", "bea"); len(liste) != 0 {
			t.Fatalf("firma b sieht freigaben von a: %+v", liste)
		}
		if _, err := u.p.OffeneFreigaben(ctx, "a", "bea"); err == nil {
			t.Fatal("fremde person liest freigaben")
		}
		wartend(t, u, a)
	})
}

func TestFreigabeLaeuftAb(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		// Offen: Nach der Wartezeit kann niemand mehr entscheiden.
		a := sendenA(t, "t1", "Guten Tag")
		id := wartend(t, u, a)
		u.jetzt = u.jetzt.Add(7*24*time.Hour + time.Second)
		if _, err := u.p.EntscheideFreigabe(ctx, "a", id, "anna", true); !errors.Is(err, ErrFreigabe) {
			t.Fatalf("abgelaufene freigabe entschieden: %v", err)
		}
		if liste, _ := u.p.OffeneFreigaben(ctx, "a", "anna"); len(liste) != 0 {
			t.Fatalf("abgelaufene freigabe gelistet: %+v", liste)
		}
		neu := wartend(t, u, a)
		if neu == id {
			t.Fatal("abgelaufene freigabe wiederverwendet")
		}

		// Bestätigt: Die Bestätigung gilt 24 Stunden.
		_, err := u.p.EntscheideFreigabe(ctx, "a", neu, "anna", true)
		muss(t, err)
		u.jetzt = u.jetzt.Add(24*time.Hour + time.Second)
		if dritte := wartend(t, u, a); dritte == neu {
			t.Fatal("abgelaufene bestätigung wiederverwendet")
		}
	})
}

// Die Bestätigung ersetzt keine andere Prüfung: Wird die Erlaubnis
// widerrufen oder die Aufgabe gestoppt, läuft der Schritt trotzdem nicht.
func TestFreigabeErsetztKeineAnderePruefung(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := sendenA(t, "t1", "Guten Tag")
		_, err := u.p.EntscheideFreigabe(ctx, "a", wartend(t, u, a), "anna", true)
		muss(t, err)
		muss(t, u.p.Widerrufe(ctx, "a", "a-senden", "anna"))
		erwarte(t, u.p.Pruefe(ctx, a), Abgelehnt, "Erlaubnis")

		b := sendenA(t, "t2", "Guten Tag")
		muss(t, u.p.ErteileErlaubnis(ctx, Erlaubnis{ID: "a-senden2", Firma: "a", Recht: "senden", Bereich: postfachA, Dauer: Dauerhaft, ErteiltVon: "anna"}))
		_, err = u.p.EntscheideFreigabe(ctx, "a", wartend(t, u, b), "anna", true)
		muss(t, err)
		muss(t, u.p.Stoppe(ctx, "a", "t2", "ben"))
		erwarte(t, u.p.Pruefe(ctx, b), Abgelehnt, "gestoppt")
	})
}

// Ohne die echten Eingaben (reine Vorab-Prüfung) entsteht keine Freigabe.
func TestKeineFreigabeOhneDaten(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := sendenA(t, "t1", "Guten Tag")
		a.Daten = nil
		ent := u.p.Pruefe(ctx, a)
		erwarte(t, ent, Wartet, "")
		if ent.FreigabeID != "" {
			t.Fatal("freigabe ohne daten")
		}
		if liste, _ := u.p.OffeneFreigaben(ctx, "a", "anna"); len(liste) != 0 {
			t.Fatalf("%+v", liste)
		}
	})
}

// Jeder Schritt der Freigabe steht im Audit, ohne Dateninhalt.
func TestFreigabeImAudit(t *testing.T) {
	jedeUmgebung(t, func(t *testing.T, u *umgebung) {
		a := sendenA(t, "t1", "Sehr geheimer Text")
		id := wartend(t, u, a)
		_, err := u.p.EntscheideFreigabe(ctx, "a", id, "anna", true)
		muss(t, err)
		erwarte(t, u.p.Pruefe(ctx, a), Erlaubt, "")

		var folge []string
		for _, e := range u.audit("a") {
			if e.Inhalt["freigabe"] != id {
				continue
			}
			folge = append(folge, e.Art+":"+e.Inhalt["ergebnis"]+e.Inhalt["von"])
			for _, wert := range e.Inhalt {
				if strings.Contains(wert, "geheim") {
					t.Fatalf("dateninhalt im audit: %v", e.Inhalt)
				}
			}
		}
		soll := "pruefung:wartet freigabe_bestaetigt:anna pruefung:erlaubt"
		if strings.Join(folge, " ") != soll {
			t.Fatalf("audit %v, erwartet %s", folge, soll)
		}
	})
}

// Kann das Audit nicht schreiben, wird nichts entschieden.
func TestFreigabeOhneAuditNichtEntschieden(t *testing.T) {
	s := NeuerArbeitsspeicher()
	u := neueUmgebung(t, s, nil)
	a := sendenA(t, "t1", "Guten Tag")
	id := wartend(t, u, a)
	s.AuditFehler = errors.New("platte voll")
	if _, err := u.p.EntscheideFreigabe(ctx, "a", id, "anna", true); err == nil {
		t.Fatal("ohne audit entschieden")
	}
	s.AuditFehler = nil
	if got := wartend(t, u, a); got != id {
		t.Fatal("freigabe hat sich ohne audit geändert")
	}
}
