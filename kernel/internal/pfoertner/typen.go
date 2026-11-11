package pfoertner

import "time"

// Ergebnis ist eine der drei Antworten des Pförtners.
type Ergebnis string

const (
	Erlaubt   Ergebnis = "erlaubt"
	Abgelehnt Ergebnis = "abgelehnt"
	// Wartet heißt: Ein Mensch muss bestätigen, bevor es weitergeht.
	Wartet Ergebnis = "wartet"
)

// Rolle einer Person in ihrer Firma.
type Rolle string

const (
	RolleAdmin       Rolle = "admin"
	RolleLeitend     Rolle = "leitend"
	RolleAusfuehrend Rolle = "ausfuehrend"
)

// DarfFreigeben sagt, ob die Rolle Erlaubnisse vergeben darf.
func (r Rolle) DarfFreigeben() bool { return r == RolleAdmin || r == RolleLeitend }

// Regionen für Ziele. Eine unbekannte Region gilt als außerhalb der EU.
const (
	RegionEU           = "eu"
	RegionAusserhalbEU = "ausserhalb_eu"
)

// Dauer einer Erlaubnis.
type Dauer string

const (
	Einmalig  Dauer = "einmalig"
	Dauerhaft Dauer = "dauerhaft"
)

// Person ist ein Nutzer einer Firma.
type Person struct {
	ID    string `json:"id"`
	Firma string `json:"firma"`
	Rolle Rolle  `json:"rolle"`
}

// Ziel ist ein Ort außerhalb von BoticStudio, an den Daten gehen sollen.
type Ziel struct {
	Art     string `json:"art"`     // z. B. "ki_modell"
	Adresse string `json:"adresse"` // z. B. "anbieter.example"
}

// FreigegebenesZiel steht auf der Liste erlaubter Ziele einer Firma.
type FreigegebenesZiel struct {
	Ziel
	Region string `json:"region"`
	// Einwilligung des Admins. Für Ziele außerhalb der EU Pflicht.
	EinwilligungVon string    `json:"einwilligung_von,omitempty"`
	EinwilligungAm  time.Time `json:"einwilligung_am,omitempty"`
}

// Firma ist ein Arbeitsbereich mit eigener Liste erlaubter Ziele.
type Firma struct {
	ID    string              `json:"id"`
	Ziele []FreigegebenesZiel `json:"ziele"`
}

// Erlaubnis gibt ein Recht in einem Bereich frei.
type Erlaubnis struct {
	ID    string `json:"id"`
	Firma string `json:"firma"`
	Recht string `json:"recht"`
	// Bereich als Pfad, z. B. "postfach/info@firma.de/INBOX".
	// Eine Erlaubnis gilt für ihren Bereich und alles darunter.
	Bereich string `json:"bereich"`
	Dauer   Dauer  `json:"dauer"`
	// GueltigBis leer heißt: ohne Ablauf (nur bei dauerhaft sinnvoll).
	GueltigBis time.Time `json:"gueltig_bis,omitempty"`
	// Fuer nennt die Personen, für die sie gilt. Leer heißt: alle der Firma.
	Fuer       []string  `json:"fuer,omitempty"`
	ErteiltVon string    `json:"erteilt_von"`
	ErteiltAm  time.Time `json:"erteilt_am"`
	Verbraucht bool      `json:"verbraucht,omitempty"`
	Widerrufen bool      `json:"widerrufen,omitempty"`
}

// Sperre verbietet, Daten mit einer Markierung in eine Region zu senden.
type Sperre struct {
	Markierung string `json:"markierung"`
	Region     string `json:"region"`
}

// Anfrage ist ein einzelner Schritt, den der Orchestrator ausführen möchte.
type Anfrage struct {
	Firma      string `json:"firma"`
	Person     string `json:"person"`  // in wessen Auftrag
	Aufgabe    string `json:"aufgabe"` // zu welcher laufenden Aufgabe
	Faehigkeit string `json:"faehigkeit"`
	Version    string `json:"version"`
	Bereich    string `json:"bereich,omitempty"`
	Ziel       *Ziel  `json:"ziel,omitempty"`
	// Markierungen, die der Orchestrator zusätzlich setzt. Er kann damit
	// nur strenger machen: Markierungen aus der Vorprüfung fügt der Pförtner
	// selbst hinzu, keiner kann sie entfernen.
	Markierungen []string `json:"markierungen,omitempty"`
	// Daten: Eingabe -> Fingerabdruck ihres Inhalts. Setzt nur die
	// Ausführung, nie der Aufrufer. Leer heißt: unbekannt, was hinausgeht.
	Daten map[string]string `json:"-"`
}

// Entscheidung ist die Antwort des Pförtners.
type Entscheidung struct {
	Ergebnis    Ergebnis `json:"ergebnis"`
	Grund       string   `json:"grund"`
	ErlaubnisID string   `json:"erlaubnis_id,omitempty"`
	AuditNr     int      `json:"audit_nr,omitempty"`
	// FreigabeID nennt bei "wartet" die Freigabe, die ein Mensch entscheiden
	// muss, und bei "erlaubt" die Freigabe, die dafür verbraucht wurde.
	FreigabeID string `json:"freigabe_id,omitempty"`
}

// Stand einer Freigabe.
type FreigabeStand string

const (
	FreigabeOffen      FreigabeStand = "offen"
	FreigabeBestaetigt FreigabeStand = "bestaetigt"
	FreigabeAbgelehnt  FreigabeStand = "abgelehnt"
	FreigabeVerbraucht FreigabeStand = "verbraucht"
)

// Freigabe ist die Bestätigung eines Menschen für genau einen Schritt.
//
// Sie gilt für eine Aufgabe, eine Fähigkeit, einen Bereich, ein Ziel und
// genau die Daten, die beim Anfragen vorlagen (Schritt ist der Fingerabdruck
// über all das). Ändert sich eines davon, passt die Freigabe nicht mehr.
// Gespeichert werden nur Fingerabdrücke, nie die Daten selbst.
type Freigabe struct {
	ID         string `json:"id"`
	Firma      string `json:"firma"`
	Aufgabe    string `json:"aufgabe"`
	Person     string `json:"person"` // in wessen Auftrag der Schritt angefragt wurde
	Faehigkeit string `json:"faehigkeit"`
	Version    string `json:"version"`
	Bereich    string `json:"bereich,omitempty"`
	Ziel       string `json:"ziel,omitempty"` // "art/adresse"
	Schritt    string `json:"schritt"`
	// Daten: Eingabe -> Fingerabdruck. Damit kann die Oberfläche prüfen, dass
	// sie dem Menschen genau das zeigt, was später ausgeführt wird.
	Daten          map[string]string `json:"daten,omitempty"`
	Stand          FreigabeStand     `json:"stand"`
	AngefragtAm    time.Time         `json:"angefragt_am"`
	GueltigBis     time.Time         `json:"gueltig_bis"`
	EntschiedenVon string            `json:"entschieden_von,omitempty"`
	EntschiedenAm  time.Time         `json:"entschieden_am,omitempty"`
}
