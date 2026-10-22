// Package pfoertner ist der Kern von BoticStudio: Er prüft jeden Schritt.
//
// Regel A: Der Pförtner kennt keine einzelne Fähigkeit. Er vergleicht nur
// den Beipackzettel aus dem Katalog mit den Erlaubnissen und der Zielliste
// der Firma. Er plant nichts, wählt kein Modell und keine Fähigkeit.
package pfoertner

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
)

// Einstellungen gelten für den ganzen Pförtner.
type Einstellungen struct {
	// MaxSchritte je Aufgabe, Schutz gegen Endlosschleifen.
	MaxSchritte int
	// Sperren gelten für alle Firmen.
	Sperren []Sperre
}

// StandardEinstellungen sind die Werte für Stufe 1.
func StandardEinstellungen() Einstellungen {
	return Einstellungen{
		MaxSchritte: 1000,
		Sperren:     []Sperre{{Markierung: "art9_verdacht", Region: RegionAusserhalbEU}},
	}
}

type aufgabe struct {
	gestoppt  bool
	schritte  int
	versionen map[string]string // Fähigkeit -> Version, für die Dauer der Aufgabe fest
}

// Pfoertner prüft Anfragen. Er ist sicher für gleichzeitige Zugriffe.
type Pfoertner struct {
	mu          sync.Mutex
	einst       Einstellungen
	katalog     *katalog.Katalog
	audit       auditor.Auditor
	jetzt       func() time.Time
	firmen      map[string]Firma
	personen    map[string]Person
	erlaubnisse map[string]*Erlaubnis
	aufgaben    map[string]*aufgabe
}

// Neu erstellt einen Pförtner.
func Neu(einst Einstellungen, k *katalog.Katalog, a auditor.Auditor) *Pfoertner {
	return &Pfoertner{
		einst:       einst,
		katalog:     k,
		audit:       a,
		jetzt:       func() time.Time { return time.Now().UTC() },
		firmen:      map[string]Firma{},
		personen:    map[string]Person{},
		erlaubnisse: map[string]*Erlaubnis{},
		aufgaben:    map[string]*aufgabe{},
	}
}

// LegeFirmaAn trägt eine Firma ein. Personen und Ziele gehören immer zu genau einer Firma.
func (p *Pfoertner) LegeFirmaAn(f Firma) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f.ID == "" {
		return errors.New("firma ohne id")
	}
	if _, da := p.firmen[f.ID]; da {
		return fmt.Errorf("firma %q gibt es schon", f.ID)
	}
	for _, z := range f.Ziele {
		if z.Region != RegionEU && z.EinwilligungVon == "" {
			return fmt.Errorf("ziel %s/%s liegt nicht sicher in der eu und braucht eine einwilligung", z.Art, z.Adresse)
		}
	}
	p.firmen[f.ID] = f
	return p.schreibe(f.ID, "firma_angelegt", map[string]string{"firma": f.ID})
}

// LegePersonAn trägt eine Person in ihre Firma ein.
func (p *Pfoertner) LegePersonAn(pe Person) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.firmen[pe.Firma]; !ok {
		return fmt.Errorf("firma %q unbekannt", pe.Firma)
	}
	switch pe.Rolle {
	case RolleAdmin, RolleLeitend, RolleAusfuehrend:
	default:
		return fmt.Errorf("unbekannte rolle %q", pe.Rolle)
	}
	if _, da := p.personen[pe.ID]; da || pe.ID == "" {
		return fmt.Errorf("person %q ungültig oder schon vorhanden", pe.ID)
	}
	p.personen[pe.ID] = pe
	return p.schreibe(pe.Firma, "person_angelegt", map[string]string{"person": pe.ID, "rolle": string(pe.Rolle)})
}

// ErteileErlaubnis vergibt ein Recht. Nur Admin und Leitende ihrer eigenen
// Firma dürfen das, nie die KI (Grundregel 1).
func (p *Pfoertner) ErteileErlaubnis(e Erlaubnis) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	geber, ok := p.personen[e.ErteiltVon]
	if !ok || geber.Firma != e.Firma {
		return fmt.Errorf("%q darf in firma %q keine erlaubnis erteilen", e.ErteiltVon, e.Firma)
	}
	if !geber.Rolle.DarfFreigeben() {
		return fmt.Errorf("rolle %q darf keine erlaubnis erteilen", geber.Rolle)
	}
	if e.ID == "" || p.erlaubnisse[e.ID] != nil {
		return fmt.Errorf("erlaubnis-id %q ungültig oder schon vergeben", e.ID)
	}
	if e.Recht == "" || e.Recht == beipackzettel.KeinRecht || len(pfad(e.Bereich)) == 0 {
		return errors.New("erlaubnis braucht recht und bereich")
	}
	if e.Dauer != Einmalig && e.Dauer != Dauerhaft {
		return fmt.Errorf("unbekannte dauer %q", e.Dauer)
	}
	for _, id := range e.Fuer {
		if pe, ok := p.personen[id]; !ok || pe.Firma != e.Firma {
			return fmt.Errorf("person %q gehört nicht zu firma %q", id, e.Firma)
		}
	}
	e.ErteiltAm = p.jetzt()
	e.Verbraucht, e.Widerrufen = false, false
	if err := p.schreibe(e.Firma, "erlaubnis_erteilt", map[string]string{
		"erlaubnis": e.ID, "recht": e.Recht, "bereich": e.Bereich, "dauer": string(e.Dauer),
		"fuer": strings.Join(e.Fuer, ","), "erteilt_von": e.ErteiltVon,
	}); err != nil {
		return err
	}
	p.erlaubnisse[e.ID] = &e
	return nil
}

// Widerrufe nimmt eine Erlaubnis zurück.
func (p *Pfoertner) Widerrufe(firma, erlaubnisID, personID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	pe, ok := p.personen[personID]
	e := p.erlaubnisse[erlaubnisID]
	if !ok || pe.Firma != firma || !pe.Rolle.DarfFreigeben() || e == nil || e.Firma != firma {
		return errors.New("widerruf nicht möglich")
	}
	if err := p.schreibe(firma, "erlaubnis_widerrufen", map[string]string{"erlaubnis": erlaubnisID, "von": personID}); err != nil {
		return err
	}
	e.Widerrufen = true
	return nil
}

// Stoppe hält eine Aufgabe an. Danach wird jeder weitere Schritt abgelehnt.
func (p *Pfoertner) Stoppe(firma, aufgabeID, personID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pe, ok := p.personen[personID]; !ok || pe.Firma != firma {
		return errors.New("stopp nur durch eine person der eigenen firma")
	}
	if err := p.schreibe(firma, "aufgabe_gestoppt", map[string]string{"aufgabe": aufgabeID, "von": personID}); err != nil {
		return err
	}
	p.aufgabeVon(firma, aufgabeID).gestoppt = true
	return nil
}

// Pruefe entscheidet über einen Schritt. Jede Entscheidung geht an den
// Auditor. Kann er nicht schreiben, wird abgelehnt.
func (p *Pfoertner) Pruefe(a Anfrage) Entscheidung {
	p.mu.Lock()
	defer p.mu.Unlock()

	ent, anwenden := p.entscheide(a)

	inhalt := map[string]string{
		"person": a.Person, "aufgabe": a.Aufgabe, "faehigkeit": a.Faehigkeit + "@" + a.Version,
		"bereich": a.Bereich, "ergebnis": string(ent.Ergebnis), "grund": ent.Grund,
		"erlaubnis": ent.ErlaubnisID, "markierungen": strings.Join(a.Markierungen, ","),
	}
	if a.Ziel != nil {
		inhalt["ziel"] = a.Ziel.Art + "/" + a.Ziel.Adresse
	}
	e, err := p.audit.Schreibe(a.Firma, "pruefung", inhalt)
	if err != nil {
		return Entscheidung{Ergebnis: Abgelehnt, Grund: "Der Auditor kann gerade nicht schreiben. Ohne Aufzeichnung keine Aktion."}
	}
	ent.AuditNr = e.Nr
	if anwenden != nil {
		anwenden()
	}
	return ent
}

// entscheide trifft die Entscheidung, ohne etwas zu verändern. Was sich bei
// "erlaubt" ändern muss, kommt als Funktion zurück und läuft erst, wenn
// der Auditor geschrieben hat.
func (p *Pfoertner) entscheide(a Anfrage) (Entscheidung, func()) {
	nein := func(grund string, args ...any) (Entscheidung, func()) {
		return Entscheidung{Ergebnis: Abgelehnt, Grund: fmt.Sprintf(grund, args...)}, nil
	}

	// 1. Wer fragt?
	if a.Firma == "" || a.Person == "" || a.Aufgabe == "" || a.Faehigkeit == "" || a.Version == "" {
		return nein("Anfrage unvollständig: Firma, Person, Aufgabe, Fähigkeit und Version sind Pflicht.")
	}
	if _, ok := p.firmen[a.Firma]; !ok {
		return nein("Firma unbekannt.")
	}
	person, ok := p.personen[a.Person]
	if !ok || person.Firma != a.Firma {
		return nein("Person gehört nicht zu dieser Firma.")
	}

	// 2. Läuft die Aufgabe noch?
	auf := p.aufgaben[schluessel(a.Firma, a.Aufgabe)]
	if auf != nil && auf.gestoppt {
		return nein("Die Aufgabe wurde gestoppt.")
	}
	if auf != nil && auf.schritte >= p.einst.MaxSchritte {
		return nein("Die Aufgabe hat die Obergrenze von %d Schritten erreicht.", p.einst.MaxSchritte)
	}

	// 3. Was soll passieren? Nur der Beipackzettel zählt.
	z, ok := p.katalog.Finde(a.Faehigkeit, a.Version)
	if !ok {
		return nein("Fähigkeit %s@%s steht nicht im Katalog.", a.Faehigkeit, a.Version)
	}
	if auf != nil {
		if v, da := auf.versionen[z.Name]; da && v != z.Version {
			return nein("Diese Aufgabe nutzt %s in Version %s und bleibt dabei.", z.Name, v)
		}
	}

	// 4. Gibt es eine passende Erlaubnis?
	var erl *Erlaubnis
	if z.Recht != beipackzettel.KeinRecht {
		if len(pfad(a.Bereich)) == 0 {
			return nein("Für %s fehlt der Bereich (%s).", z.Recht, z.Bereich)
		}
		erl = p.findeErlaubnis(a.Firma, a.Person, z.Recht, a.Bereich)
		if erl == nil {
			return nein("Es fehlt die Erlaubnis %q für %s.", z.Recht, a.Bereich)
		}
	}

	// 5. Verlassen Daten BoticStudio?
	if a.Ziel != nil {
		if grund := p.pruefeZiel(a, z); grund != "" {
			return nein("%s", grund)
		}
	}

	// 6. Muss ein Mensch bestätigen?
	if z.WirktNachAussen || z.Risiko == beipackzettel.RisikoHoch {
		ent := Entscheidung{Ergebnis: Wartet, Grund: "Das wirkt nach außen oder ist riskant. Eine leitende Person muss bestätigen."}
		if erl != nil {
			ent.ErlaubnisID = erl.ID
		}
		return ent, nil
	}

	ent := Entscheidung{Ergebnis: Erlaubt, Grund: "Erlaubt."}
	if erl != nil {
		ent.ErlaubnisID = erl.ID
	}
	return ent, func() {
		aa := p.aufgabeVon(a.Firma, a.Aufgabe)
		aa.schritte++
		aa.versionen[z.Name] = z.Version
		if erl != nil && erl.Dauer == Einmalig {
			erl.Verbraucht = true
		}
	}
}

func (p *Pfoertner) findeErlaubnis(firma, person, recht, bereich string) *Erlaubnis {
	jetzt := p.jetzt()
	gewuenscht := pfad(bereich)
	var beste *Erlaubnis
	for _, e := range p.erlaubnisse {
		if e.Firma != firma || e.Recht != recht || e.Widerrufen || e.Verbraucht {
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

func (p *Pfoertner) pruefeZiel(a Anfrage, z beipackzettel.Zettel) string {
	if a.Ziel.Art == "" || a.Ziel.Adresse == "" {
		return "Ziel unvollständig."
	}
	if !z.DarfSenden(a.Ziel.Art) {
		return fmt.Sprintf("Laut Beipackzettel sendet %s keine Daten an %q.", z.Name, a.Ziel.Art)
	}
	var fz *FreigegebenesZiel
	for i, kandidat := range p.firmen[a.Firma].Ziele {
		if kandidat.Ziel == *a.Ziel {
			fz = &p.firmen[a.Firma].Ziele[i]
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

func (p *Pfoertner) aufgabeVon(firma, id string) *aufgabe {
	k := schluessel(firma, id)
	a := p.aufgaben[k]
	if a == nil {
		a = &aufgabe{versionen: map[string]string{}}
		p.aufgaben[k] = a
	}
	return a
}

func (p *Pfoertner) schreibe(firma, art string, inhalt map[string]string) error {
	if _, err := p.audit.Schreibe(firma, art, inhalt); err != nil {
		return fmt.Errorf("auditor: %w", err)
	}
	return nil
}

// schluessel trennt Aufgaben verschiedener Firmen, auch bei gleicher ID.
func schluessel(firma, aufgabe string) string { return firma + "\x00" + aufgabe }

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
