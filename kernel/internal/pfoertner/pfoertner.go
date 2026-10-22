// Package pfoertner ist der Kern von BoticStudio: Er prüft jeden Schritt.
//
// Regel A: Der Pförtner kennt keine einzelne Fähigkeit. Er vergleicht nur
// den Beipackzettel aus dem Katalog mit den Erlaubnissen und der Zielliste
// der Firma. Er plant nichts, wählt kein Modell und keine Fähigkeit.
//
// Gespeichert wird über einen Speicher (Arbeitsspeicher oder PostgreSQL).
// Jede Prüfung ist ein Vorgang: Entscheidung, Audit-Eintrag und die
// Änderungen daraus werden gemeinsam gespeichert oder gar nicht.
package pfoertner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
)

// Einstellungen gelten für den ganzen Pförtner.
type Einstellungen struct {
	// MaxSchritte je Aufgabe, Schutz gegen Endlosschleifen.
	MaxSchritte int
	// Sperren gelten für alle Firmen.
	Sperren []Sperre
	// Uhr liefert die aktuelle Zeit. Leer heißt: Systemzeit.
	Uhr func() time.Time
}

// StandardEinstellungen sind die Werte für den Start.
func StandardEinstellungen() Einstellungen {
	return Einstellungen{
		MaxSchritte: 1000,
		Sperren:     []Sperre{{Markierung: "art9_verdacht", Region: RegionAusserhalbEU}},
	}
}

// Pfoertner prüft Anfragen. Er ist sicher für gleichzeitige Zugriffe.
type Pfoertner struct {
	einst    Einstellungen
	katalog  *katalog.Katalog
	speicher Speicher
}

// Neu erstellt einen Pförtner.
func Neu(einst Einstellungen, k *katalog.Katalog, s Speicher) *Pfoertner {
	if einst.Uhr == nil {
		einst.Uhr = time.Now
	}
	return &Pfoertner{einst: einst, katalog: k, speicher: s}
}

func (p *Pfoertner) jetzt() time.Time { return p.einst.Uhr().UTC() }

// LegeFirmaAn trägt eine Firma ein. Personen und Ziele gehören immer zu genau einer Firma.
func (p *Pfoertner) LegeFirmaAn(ctx context.Context, f Firma) error {
	if f.ID == "" {
		return errors.New("firma ohne id")
	}
	for _, z := range f.Ziele {
		if z.Region != RegionEU && z.EinwilligungVon == "" {
			return fmt.Errorf("ziel %s/%s liegt nicht sicher in der eu und braucht eine einwilligung", z.Art, z.Adresse)
		}
	}
	return p.speicher.Vorgang(ctx, f.ID, func(v Vorgang) error {
		if _, da, err := v.Firma(); err != nil {
			return err
		} else if da {
			return fmt.Errorf("%w: firma %q", ErrGibtEsSchon, f.ID)
		}
		if err := v.LegeFirmaAn(f); err != nil {
			return err
		}
		return schreibe(v, "firma_angelegt", map[string]string{"firma": f.ID})
	})
}

// LegePersonAn trägt eine Person in ihre Firma ein.
func (p *Pfoertner) LegePersonAn(ctx context.Context, pe Person) error {
	switch pe.Rolle {
	case RolleAdmin, RolleLeitend, RolleAusfuehrend:
	default:
		return fmt.Errorf("unbekannte rolle %q", pe.Rolle)
	}
	if pe.ID == "" || pe.Firma == "" {
		return errors.New("person braucht id und firma")
	}
	return p.speicher.Vorgang(ctx, pe.Firma, func(v Vorgang) error {
		if _, da, err := v.Firma(); err != nil || !da {
			return errors.Join(err, fmt.Errorf("firma %q unbekannt", pe.Firma))
		}
		if _, da, err := v.Person(pe.ID); err != nil || da {
			return errors.Join(err, fmt.Errorf("person %q gibt es schon", pe.ID))
		}
		if err := v.LegePersonAn(pe); err != nil {
			return err
		}
		return schreibe(v, "person_angelegt", map[string]string{"person": pe.ID, "rolle": string(pe.Rolle)})
	})
}

// ErteileErlaubnis vergibt ein Recht. Nur Admin und Leitende ihrer eigenen
// Firma dürfen das, nie die KI (Grundregel 1).
func (p *Pfoertner) ErteileErlaubnis(ctx context.Context, e Erlaubnis) error {
	if e.ID == "" || e.Firma == "" {
		return errors.New("erlaubnis braucht id und firma")
	}
	if e.Recht == "" || e.Recht == beipackzettel.KeinRecht || len(pfad(e.Bereich)) == 0 {
		return errors.New("erlaubnis braucht recht und bereich")
	}
	if e.Dauer != Einmalig && e.Dauer != Dauerhaft {
		return fmt.Errorf("unbekannte dauer %q", e.Dauer)
	}
	return p.speicher.Vorgang(ctx, e.Firma, func(v Vorgang) error {
		geber, ok, err := v.Person(e.ErteiltVon)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%q darf in firma %q keine erlaubnis erteilen", e.ErteiltVon, e.Firma)
		}
		if !geber.Rolle.DarfFreigeben() {
			return fmt.Errorf("rolle %q darf keine erlaubnis erteilen", geber.Rolle)
		}
		if _, da, err := v.Erlaubnis(e.ID); err != nil || da {
			return errors.Join(err, fmt.Errorf("erlaubnis-id %q schon vergeben", e.ID))
		}
		for _, id := range e.Fuer {
			if _, ok, err := v.Person(id); err != nil || !ok {
				return errors.Join(err, fmt.Errorf("person %q gehört nicht zu firma %q", id, e.Firma))
			}
		}
		e.ErteiltAm = p.jetzt()
		e.Verbraucht, e.Widerrufen = false, false
		if err := v.SpeichereErlaubnis(e); err != nil {
			return err
		}
		return schreibe(v, "erlaubnis_erteilt", map[string]string{
			"erlaubnis": e.ID, "recht": e.Recht, "bereich": e.Bereich, "dauer": string(e.Dauer),
			"fuer": strings.Join(e.Fuer, ","), "erteilt_von": e.ErteiltVon,
		})
	})
}

// Widerrufe nimmt eine Erlaubnis zurück.
func (p *Pfoertner) Widerrufe(ctx context.Context, firma, erlaubnisID, personID string) error {
	return p.speicher.Vorgang(ctx, firma, func(v Vorgang) error {
		pe, ok, err := v.Person(personID)
		if err != nil {
			return err
		}
		e, da, err := v.Erlaubnis(erlaubnisID)
		if err != nil {
			return err
		}
		if !ok || !pe.Rolle.DarfFreigeben() || !da {
			return errors.New("widerruf nicht möglich")
		}
		e.Widerrufen = true
		if err := v.SpeichereErlaubnis(e); err != nil {
			return err
		}
		return schreibe(v, "erlaubnis_widerrufen", map[string]string{"erlaubnis": erlaubnisID, "von": personID})
	})
}

// Stoppe hält eine Aufgabe an. Danach wird jeder weitere Schritt abgelehnt.
func (p *Pfoertner) Stoppe(ctx context.Context, firma, aufgabeID, personID string) error {
	if aufgabeID == "" {
		return errors.New("aufgabe fehlt")
	}
	return p.speicher.Vorgang(ctx, firma, func(v Vorgang) error {
		if _, ok, err := v.Person(personID); err != nil || !ok {
			return errors.Join(err, errors.New("stopp nur durch eine person der eigenen firma"))
		}
		a, err := v.Aufgabe(aufgabeID)
		if err != nil {
			return err
		}
		a.Gestoppt = true
		if err := v.SpeichereAufgabe(aufgabeID, a); err != nil {
			return err
		}
		return schreibe(v, "aufgabe_gestoppt", map[string]string{"aufgabe": aufgabeID, "von": personID})
	})
}

// Vermerke schreibt einen Eintrag ins Audit der Firma, z. B. nach einer Ausführung.
func (p *Pfoertner) Vermerke(ctx context.Context, firma, art string, inhalt map[string]string) error {
	return p.speicher.Vorgang(ctx, firma, func(v Vorgang) error {
		return schreibe(v, art, inhalt)
	})
}

// Pruefe entscheidet über einen Schritt. Jede Entscheidung geht ins Audit.
// Kann das Audit nicht geschrieben werden, wird abgelehnt und nichts geändert.
func (p *Pfoertner) Pruefe(ctx context.Context, a Anfrage) Entscheidung {
	if a.Firma == "" || a.Person == "" || a.Aufgabe == "" || a.Faehigkeit == "" || a.Version == "" {
		// Ohne Firma gibt es keine Kette, in die das gehört. Abgelehnt wird trotzdem.
		return Entscheidung{Ergebnis: Abgelehnt, Grund: "Anfrage unvollständig: Firma, Person, Aufgabe, Fähigkeit und Version sind Pflicht."}
	}
	var ent Entscheidung
	err := p.speicher.Vorgang(ctx, a.Firma, func(v Vorgang) error {
		var anwenden func() error
		var err error
		ent, anwenden, err = p.entscheide(v, a)
		if err != nil {
			return err
		}
		inhalt := map[string]string{
			"person": a.Person, "aufgabe": a.Aufgabe, "faehigkeit": a.Faehigkeit + "@" + a.Version,
			"bereich": a.Bereich, "ergebnis": string(ent.Ergebnis), "grund": ent.Grund,
			"erlaubnis": ent.ErlaubnisID, "markierungen": strings.Join(a.Markierungen, ","),
		}
		if a.Ziel != nil {
			inhalt["ziel"] = a.Ziel.Art + "/" + a.Ziel.Adresse
		}
		e, err := v.Schreibe("pruefung", inhalt)
		if err != nil {
			return errAudit
		}
		ent.AuditNr = e.Nr
		if anwenden != nil {
			return anwenden()
		}
		return nil
	})
	switch {
	case errors.Is(err, errAudit):
		return Entscheidung{Ergebnis: Abgelehnt, Grund: "Der Auditor kann gerade nicht schreiben. Ohne Aufzeichnung keine Aktion."}
	case errors.Is(err, errFirmaUnbekannt):
		return Entscheidung{Ergebnis: Abgelehnt, Grund: "Firma unbekannt."}
	case err != nil:
		return Entscheidung{Ergebnis: Abgelehnt, Grund: "Der Speicher ist gerade nicht erreichbar. Ohne Aufzeichnung keine Aktion."}
	}
	return ent
}

var (
	errAudit          = errors.New("audit nicht geschrieben")
	errFirmaUnbekannt = errors.New("firma unbekannt")
)

// entscheide trifft die Entscheidung, ohne etwas zu verändern. Was sich bei
// "erlaubt" ändern muss, kommt als Funktion zurück und läuft erst, wenn
// der Audit-Eintrag geschrieben ist.
func (p *Pfoertner) entscheide(v Vorgang, a Anfrage) (Entscheidung, func() error, error) {
	nein := func(grund string, args ...any) (Entscheidung, func() error, error) {
		return Entscheidung{Ergebnis: Abgelehnt, Grund: fmt.Sprintf(grund, args...)}, nil, nil
	}

	// 1. Wer fragt?
	firma, ok, err := v.Firma()
	if err != nil {
		return Entscheidung{}, nil, err
	}
	if !ok {
		// Eine unbekannte Firma hat keine Audit-Kette, also kein Eintrag.
		return Entscheidung{}, nil, errFirmaUnbekannt
	}
	if _, ok, err := v.Person(a.Person); err != nil {
		return Entscheidung{}, nil, err
	} else if !ok {
		return nein("Person gehört nicht zu dieser Firma.")
	}

	// 2. Läuft die Aufgabe noch?
	auf, err := v.Aufgabe(a.Aufgabe)
	if err != nil {
		return Entscheidung{}, nil, err
	}
	if auf.Gestoppt {
		return nein("Die Aufgabe wurde gestoppt.")
	}
	if auf.Schritte >= p.einst.MaxSchritte {
		return nein("Die Aufgabe hat die Obergrenze von %d Schritten erreicht.", p.einst.MaxSchritte)
	}

	// 3. Was soll passieren? Nur der Beipackzettel zählt.
	z, ok := p.katalog.Finde(a.Faehigkeit, a.Version)
	if !ok {
		return nein("Fähigkeit %s@%s steht nicht im Katalog.", a.Faehigkeit, a.Version)
	}
	if ver, da := auf.Versionen[z.Name]; da && ver != z.Version {
		return nein("Diese Aufgabe nutzt %s in Version %s und bleibt dabei.", z.Name, ver)
	}

	// 4. Gibt es eine passende Erlaubnis?
	var erl *Erlaubnis
	if z.Recht != beipackzettel.KeinRecht {
		if len(pfad(a.Bereich)) == 0 {
			return nein("Für %s fehlt der Bereich (%s).", z.Recht, z.Bereich)
		}
		kandidaten, err := v.Erlaubnisse(z.Recht)
		if err != nil {
			return Entscheidung{}, nil, err
		}
		erl = p.findeErlaubnis(kandidaten, a.Person, a.Bereich)
		if erl == nil {
			return nein("Es fehlt die Erlaubnis %q für %s.", z.Recht, a.Bereich)
		}
	}

	// 5. Verlassen Daten BoticStudio?
	if a.Ziel != nil {
		if grund := p.pruefeZiel(firma, a, z); grund != "" {
			return nein("%s", grund)
		}
	}

	// 6. Muss ein Mensch bestätigen?
	if z.WirktNachAussen || z.Risiko == beipackzettel.RisikoHoch {
		ent := Entscheidung{Ergebnis: Wartet, Grund: "Das wirkt nach außen oder ist riskant. Eine leitende Person muss bestätigen."}
		if erl != nil {
			ent.ErlaubnisID = erl.ID
		}
		return ent, nil, nil
	}

	ent := Entscheidung{Ergebnis: Erlaubt, Grund: "Erlaubt."}
	if erl != nil {
		ent.ErlaubnisID = erl.ID
	}
	return ent, func() error {
		auf.Schritte++
		if auf.Versionen == nil {
			auf.Versionen = map[string]string{}
		}
		auf.Versionen[z.Name] = z.Version
		if err := v.SpeichereAufgabe(a.Aufgabe, auf); err != nil {
			return err
		}
		if erl != nil && erl.Dauer == Einmalig {
			erl.Verbraucht = true
			return v.SpeichereErlaubnis(*erl)
		}
		return nil
	}, nil
}

func (p *Pfoertner) findeErlaubnis(kandidaten []Erlaubnis, person, bereich string) *Erlaubnis {
	jetzt := p.jetzt()
	gewuenscht := pfad(bereich)
	var beste *Erlaubnis
	for i := range kandidaten {
		e := &kandidaten[i]
		if e.Widerrufen || e.Verbraucht {
			continue
		}
		if !e.GueltigBis.IsZero() && !jetzt.Before(e.GueltigBis) {
			continue
		}
		if len(e.Fuer) > 0 && !enthaelt(e.Fuer, person) {
			continue
		}
		if !istUnter(gewuenscht, pfad(e.Bereich)) {
			continue
		}
		// Dauerhafte Erlaubnisse zuerst, damit einmalige nicht unnötig verbraucht werden.
		if beste == nil || (beste.Dauer == Einmalig && e.Dauer == Dauerhaft) ||
			(beste.Dauer == e.Dauer && e.ID < beste.ID) {
			beste = e
		}
	}
	return beste
}

func (p *Pfoertner) pruefeZiel(firma Firma, a Anfrage, z beipackzettel.Zettel) string {
	if a.Ziel.Art == "" || a.Ziel.Adresse == "" {
		return "Ziel unvollständig."
	}
	if !z.DarfSenden(a.Ziel.Art) {
		return fmt.Sprintf("Laut Beipackzettel sendet %s keine Daten an %q.", z.Name, a.Ziel.Art)
	}
	var fz *FreigegebenesZiel
	for i, kandidat := range firma.Ziele {
		if kandidat.Ziel == *a.Ziel {
			fz = &firma.Ziele[i]
			break
		}
	}
	if fz == nil {
		return fmt.Sprintf("Ziel %s steht nicht auf der Liste erlaubter Ziele.", a.Ziel.Adresse)
	}
	region := fz.Region
	if region != RegionEU {
		region = RegionAusserhalbEU
		if fz.EinwilligungVon == "" {
			return "Für dieses Ziel außerhalb der EU fehlt die Einwilligung des Admins."
		}
	}
	for _, s := range p.einst.Sperren {
		if s.Region == region && enthaelt(a.Markierungen, s.Markierung) {
			return fmt.Sprintf("Daten mit der Markierung %q gehen nicht an Ziele in Region %q.", s.Markierung, region)
		}
	}
	return ""
}

func schreibe(v Vorgang, art string, inhalt map[string]string) error {
	if _, err := v.Schreibe(art, inhalt); err != nil {
		return fmt.Errorf("auditor: %w", err)
	}
	return nil
}

// pfad zerlegt einen Bereich in Teile. Leere Teile, "." und ".." sind
// nicht erlaubt, damit niemand aus seinem Bereich herausklettert.
func pfad(bereich string) []string {
	if strings.TrimSpace(bereich) == "" {
		return nil
	}
	teile := strings.Split(strings.Trim(bereich, "/"), "/")
	for _, t := range teile {
		if t == "" || t == "." || t == ".." {
			return nil
		}
	}
	return teile
}

// istUnter sagt, ob gewuenscht gleich oder unterhalb von erlaubt liegt.
// Verglichen wird Teil für Teil: "postfach/info@a.de" deckt
// "postfach/info@a.de/INBOX", aber nicht "postfach/info@a.de.evil".
func istUnter(gewuenscht, erlaubt []string) bool {
	if len(gewuenscht) == 0 || len(erlaubt) == 0 || len(gewuenscht) < len(erlaubt) {
		return false
	}
	for i := range erlaubt {
		if gewuenscht[i] != erlaubt[i] {
			return false
		}
	}
	return true
}

func enthaelt(liste []string, wert string) bool {
	for _, v := range liste {
		if v == wert {
			return true
		}
	}
	return false
}
