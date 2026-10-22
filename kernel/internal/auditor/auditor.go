// Package auditor schreibt jede Entscheidung des Pförtners mit.
//
// Einträge werden nur angehängt, nie geändert. Jeder Eintrag trägt den
// Fingerabdruck (SHA-256) des vorherigen. Wird ein Eintrag verändert oder
// entfernt, fällt das beim Prüfen der Kette sofort auf.
package auditor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Eintrag ist eine Zeile im Audit.
type Eintrag struct {
	Nr         int               `json:"nr"`
	Zeit       time.Time         `json:"zeit"`
	Firma      string            `json:"firma"`
	Art        string            `json:"art"`
	Inhalt     map[string]string `json:"inhalt"`
	VorherHash string            `json:"vorher_hash"`
	Hash       string            `json:"hash"`
}

// Auditor nimmt Einträge an. Schlägt Schreibe fehl, muss der Aufrufer
// die Aktion ablehnen.
type Auditor interface {
	Schreibe(firma, art string, inhalt map[string]string) (Eintrag, error)
}

// Speicher ist ein Auditor im Arbeitsspeicher, für Tests und den Start.
// Später ersetzt ihn ein getrennter, nur anhängender Speicher in PostgreSQL.
type Speicher struct {
	mu        sync.Mutex
	eintraege []Eintrag
	jetzt     func() time.Time
}

// NeuSpeicher erstellt einen leeren Audit-Speicher.
func NeuSpeicher() *Speicher {
	return &Speicher{jetzt: func() time.Time { return time.Now().UTC() }}
}

// Schreibe hängt einen Eintrag an die Kette an.
func (s *Speicher) Schreibe(firma, art string, inhalt map[string]string) (Eintrag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kopie := make(map[string]string, len(inhalt))
	for k, v := range inhalt {
		kopie[k] = v
	}
	e := Eintrag{
		Nr:     len(s.eintraege) + 1,
		Zeit:   s.jetzt(),
		Firma:  firma,
		Art:    art,
		Inhalt: kopie,
	}
	if n := len(s.eintraege); n > 0 {
		e.VorherHash = s.eintraege[n-1].Hash
	}
	h, err := fingerabdruck(e)
	if err != nil {
		return Eintrag{}, err
	}
	e.Hash = h
	s.eintraege = append(s.eintraege, e)
	return e, nil
}

// Eintraege liefert eine Kopie aller Einträge.
func (s *Speicher) Eintraege() []Eintrag {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Eintrag(nil), s.eintraege...)
}

// PruefeKette prüft, ob die Einträge lückenlos und unverändert sind.
func PruefeKette(eintraege []Eintrag) error {
	vorher := ""
	for i, e := range eintraege {
		if e.Nr != i+1 {
			return fmt.Errorf("audit: eintrag %d hat nummer %d, es fehlt etwas", i+1, e.Nr)
		}
		if e.VorherHash != vorher {
			return fmt.Errorf("audit: eintrag %d passt nicht zum vorherigen", e.Nr)
		}
		h, err := fingerabdruck(e)
		if err != nil {
			return err
		}
		if h != e.Hash {
			return fmt.Errorf("audit: eintrag %d wurde verändert", e.Nr)
		}
		vorher = e.Hash
	}
	return nil
}

func fingerabdruck(e Eintrag) (string, error) {
	e.Hash = ""
	// encoding/json sortiert Map-Schlüssel, der Text ist daher eindeutig.
	daten, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	summe := sha256.Sum256(daten)
	return hex.EncodeToString(summe[:]), nil
}
