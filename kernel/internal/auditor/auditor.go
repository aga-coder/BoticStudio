// Package auditor beschreibt den Audit-Eintrag und seine Hash-Kette.
//
// Einträge werden nur angehängt, nie geändert. Jede Firma hat ihre eigene
// Kette: Jeder Eintrag trägt den Fingerabdruck (SHA-256) des vorherigen
// Eintrags derselben Firma. Wird ein Eintrag verändert oder entfernt, fällt
// das beim Prüfen der Kette sofort auf. Gespeichert wird die Kette vom
// Speicher des Pförtners, im selben Vorgang wie die Entscheidung selbst.
package auditor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// Naechster baut den Eintrag, der auf vorher folgt (vorher nil: erster
// Eintrag der Firma). Die Zeit wird auf Mikrosekunden gekürzt, damit sie
// in PostgreSQL unverändert gespeichert wird und der Fingerabdruck stimmt.
func Naechster(vorher *Eintrag, firma, art string, inhalt map[string]string, zeit time.Time) (Eintrag, error) {
	if firma == "" {
		return Eintrag{}, errors.New("audit: eintrag ohne firma")
	}
	kopie := make(map[string]string, len(inhalt))
	for k, v := range inhalt {
		kopie[k] = v
	}
	e := Eintrag{Nr: 1, Zeit: zeit.UTC().Truncate(time.Microsecond), Firma: firma, Art: art, Inhalt: kopie}
	if vorher != nil {
		if vorher.Firma != firma {
			return Eintrag{}, errors.New("audit: kette einer anderen firma")
		}
		e.Nr = vorher.Nr + 1
		e.VorherHash = vorher.Hash
	}
	h, err := fingerabdruck(e)
	if err != nil {
		return Eintrag{}, err
	}
	e.Hash = h
	return e, nil
}

// PruefeKette prüft, ob die Einträge einer Firma lückenlos und unverändert sind.
func PruefeKette(eintraege []Eintrag) error {
	vorher := ""
	for i, e := range eintraege {
		if e.Nr != i+1 {
			return fmt.Errorf("audit: eintrag %d hat nummer %d, es fehlt etwas", i+1, e.Nr)
		}
		if i > 0 && e.Firma != eintraege[0].Firma {
			return fmt.Errorf("audit: eintrag %d gehört zu einer anderen firma", e.Nr)
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
