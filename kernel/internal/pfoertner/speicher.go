package pfoertner

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
)

// Speicher hält, was der Pförtner sich merken muss: Firmen, Personen,
// Erlaubnisse, Aufgaben und das Audit.
//
// Jeder Zugriff läuft in einem Vorgang, der an genau eine Firma gebunden
// ist. Ein Vorgang sieht nur Daten dieser Firma. In PostgreSQL erzwingt das
// die Datenbank selbst (Row-Level Security), nicht der Code des Pförtners.
type Speicher interface {
	// Vorgang führt fn aus. Gibt fn einen Fehler zurück, bleibt nichts
	// gespeichert, auch kein Audit-Eintrag.
	Vorgang(ctx context.Context, firma string, fn func(Vorgang) error) error
}

// Vorgang ist ein Zugriff auf die Daten genau einer Firma.
type Vorgang interface {
	Firma() (Firma, bool, error)
	LegeFirmaAn(f Firma) error
	Person(id string) (Person, bool, error)
	LegePersonAn(p Person) error
	// Erlaubnisse liefert die Erlaubnisse zu einem Recht und sperrt sie bis
	// zum Ende des Vorgangs, damit eine einmalige nur einmal verbraucht wird.
	Erlaubnisse(recht string) ([]Erlaubnis, error)
	Erlaubnis(id string) (Erlaubnis, bool, error)
	SpeichereErlaubnis(e Erlaubnis) error
	// Aufgabe liefert den Stand einer Aufgabe und sperrt ihn bis zum Ende des Vorgangs.
	Aufgabe(id string) (Aufgabenstand, error)
	SpeichereAufgabe(id string, a Aufgabenstand) error
	// Vorpruefung liefert die Markierungen aller Prüfungen eines Inhalts.
	// geprueft ist false, wenn ihn noch keine prüfende Fähigkeit gesehen hat.
	Vorpruefung(fingerabdruck string) (markierungen []string, geprueft bool, err error)
	SpeichereVorpruefung(fingerabdruck, faehigkeit string, markierungen []string) error
	// Schreibe hängt einen Eintrag an die Audit-Kette der Firma.
	Schreibe(art string, inhalt map[string]string) (auditor.Eintrag, error)
}

// Aufgabenstand ist, was der Pförtner über eine laufende Aufgabe weiß.
type Aufgabenstand struct {
	Gestoppt bool
	Schritte int
	// Versionen: Fähigkeit -> Version, für die Dauer der Aufgabe fest.
	Versionen map[string]string
}

// ErrGibtEsSchon heißt: Der Eintrag ist schon vorhanden.
var ErrGibtEsSchon = errors.New("gibt es schon")

// ErrUnbekannteFirma heißt: Der Vorgang gilt einer Firma, die es nicht gibt.
var ErrUnbekannteFirma = errors.New("firma unbekannt")

// Arbeitsspeicher ist ein Speicher im Arbeitsspeicher, für Tests und lokale
// Versuche. Ein Vorgang arbeitet auf einer Kopie und übernimmt sie erst am
// Ende, damit ein Fehler wirklich nichts hinterlässt.
type Arbeitsspeicher struct {
	mu    sync.Mutex
	daten map[string]*firmendaten
	Uhr   func() time.Time
	// AuditFehler lässt jedes Schreiben ins Audit scheitern (nur für Tests).
	AuditFehler error
}

type firmendaten struct {
	firma       Firma
	personen    map[string]Person
	erlaubnisse map[string]Erlaubnis
	aufgaben    map[string]Aufgabenstand
	// vorpruefungen: Fingerabdruck -> Markierungen (vereinigt)
	vorpruefungen map[string][]string
	audit         []auditor.Eintrag
}

// NeuerArbeitsspeicher erstellt einen leeren Speicher.
func NeuerArbeitsspeicher() *Arbeitsspeicher {
	return &Arbeitsspeicher{daten: map[string]*firmendaten{}, Uhr: time.Now}
}

// Vorgang sperrt den ganzen Speicher. Für Tests reicht das.
func (s *Arbeitsspeicher) Vorgang(_ context.Context, firma string, fn func(Vorgang) error) error {
	if firma == "" {
		return ErrUnbekannteFirma
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := &speicherVorgang{s: s, firma: firma}
	if d := s.daten[firma]; d != nil {
		v.d = d.kopie()
	}
	if err := fn(v); err != nil {
		return err
	}
	if v.d != nil {
		s.daten[firma] = v.d
	}
	return nil
}

// Audit liefert die Audit-Kette einer Firma.
func (s *Arbeitsspeicher) Audit(firma string) []auditor.Eintrag {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.daten[firma]; d != nil {
		return append([]auditor.Eintrag(nil), d.audit...)
	}
	return nil
}

func (d *firmendaten) kopie() *firmendaten {
	k := &firmendaten{
		firma:         d.firma,
		personen:      make(map[string]Person, len(d.personen)),
		erlaubnisse:   make(map[string]Erlaubnis, len(d.erlaubnisse)),
		aufgaben:      make(map[string]Aufgabenstand, len(d.aufgaben)),
		vorpruefungen: make(map[string][]string, len(d.vorpruefungen)),
		audit:         append([]auditor.Eintrag(nil), d.audit...),
	}
	for fp, m := range d.vorpruefungen {
		k.vorpruefungen[fp] = append([]string(nil), m...)
	}
	for id, p := range d.personen {
		k.personen[id] = p
	}
	for id, e := range d.erlaubnisse {
		k.erlaubnisse[id] = e
	}
	for id, a := range d.aufgaben {
		versionen := make(map[string]string, len(a.Versionen))
		for n, v := range a.Versionen {
			versionen[n] = v
		}
		a.Versionen = versionen
		k.aufgaben[id] = a
	}
	return k
}

type speicherVorgang struct {
	s     *Arbeitsspeicher
	firma string
	d     *firmendaten
}

func (v *speicherVorgang) Firma() (Firma, bool, error) {
	if v.d == nil {
		return Firma{}, false, nil
	}
	return v.d.firma, true, nil
}

func (v *speicherVorgang) LegeFirmaAn(f Firma) error {
	if v.d != nil {
		return errors.New("firma gibt es schon")
	}
	if f.ID != v.firma {
		return errors.New("firma passt nicht zum vorgang")
	}
	v.d = &firmendaten{firma: f, personen: map[string]Person{}, erlaubnisse: map[string]Erlaubnis{}, aufgaben: map[string]Aufgabenstand{}}
	return nil
}

func (v *speicherVorgang) Person(id string) (Person, bool, error) {
	if v.d == nil {
		return Person{}, false, nil
	}
	p, ok := v.d.personen[id]
	return p, ok, nil
}

func (v *speicherVorgang) LegePersonAn(p Person) error {
	if v.d == nil || p.Firma != v.firma {
		return ErrUnbekannteFirma
	}
	if _, da := v.d.personen[p.ID]; da {
		return errors.New("person gibt es schon")
	}
	v.d.personen[p.ID] = p
	return nil
}

func (v *speicherVorgang) Erlaubnisse(recht string) ([]Erlaubnis, error) {
	if v.d == nil {
		return nil, nil
	}
	var liste []Erlaubnis
	for _, e := range v.d.erlaubnisse {
		if e.Recht == recht {
			liste = append(liste, e)
		}
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].ID < liste[j].ID })
	return liste, nil
}

func (v *speicherVorgang) Erlaubnis(id string) (Erlaubnis, bool, error) {
	if v.d == nil {
		return Erlaubnis{}, false, nil
	}
	e, ok := v.d.erlaubnisse[id]
	return e, ok, nil
}

func (v *speicherVorgang) SpeichereErlaubnis(e Erlaubnis) error {
	if v.d == nil || e.Firma != v.firma {
		return ErrUnbekannteFirma
	}
	v.d.erlaubnisse[e.ID] = e
	return nil
}

func (v *speicherVorgang) Aufgabe(id string) (Aufgabenstand, error) {
	if v.d == nil {
		return Aufgabenstand{}, ErrUnbekannteFirma
	}
	a, ok := v.d.aufgaben[id]
	if !ok {
		a = Aufgabenstand{Versionen: map[string]string{}}
	}
	return a, nil
}

func (v *speicherVorgang) SpeichereAufgabe(id string, a Aufgabenstand) error {
	if v.d == nil {
		return ErrUnbekannteFirma
	}
	v.d.aufgaben[id] = a
	return nil
}

func (v *speicherVorgang) Vorpruefung(fp string) ([]string, bool, error) {
	if v.d == nil {
		return nil, false, nil
	}
	m, da := v.d.vorpruefungen[fp]
	return append([]string(nil), m...), da, nil
}

func (v *speicherVorgang) SpeichereVorpruefung(fp, _ string, markierungen []string) error {
	if v.d == nil {
		return ErrUnbekannteFirma
	}
	if v.d.vorpruefungen == nil {
		v.d.vorpruefungen = map[string][]string{}
	}
	m := append([]string{}, v.d.vorpruefungen[fp]...)
	for _, neu := range markierungen {
		if !enthaelt(m, neu) {
			m = append(m, neu)
		}
	}
	v.d.vorpruefungen[fp] = m
	return nil
}

func (v *speicherVorgang) Schreibe(art string, inhalt map[string]string) (auditor.Eintrag, error) {
	if v.s.AuditFehler != nil {
		return auditor.Eintrag{}, v.s.AuditFehler
	}
	if v.d == nil {
		return auditor.Eintrag{}, ErrUnbekannteFirma
	}
	var vorher *auditor.Eintrag
	if n := len(v.d.audit); n > 0 {
		vorher = &v.d.audit[n-1]
	}
	e, err := auditor.Naechster(vorher, v.firma, art, inhalt, v.s.Uhr())
	if err != nil {
		return auditor.Eintrag{}, err
	}
	v.d.audit = append(v.d.audit, e)
	return e, nil
}
