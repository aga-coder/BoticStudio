// Package dienste kennt die Dienste, die den Pförtner aufrufen dürfen.
//
// Jeder Dienst (Dashboard, Ablauf) hat einen eigenen zufälligen Schlüssel. Der
// Pförtner kennt davon nur den SHA-256, aus einer Datei, die beim Start
// geladen wird. Zu jedem Dienst steht dort auch, für welche Firmen er anfragen
// darf. Der Schlüssel selbst steht in keinem Log und keiner Fehlermeldung.
package dienste

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
)

// MinLaenge ist die kürzeste Länge eines Schlüssels, die der Pförtner überhaupt prüft.
const MinLaenge = 32

var gueltigerName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

// Eintrag ist eine Zeile der Datei.
type Eintrag struct {
	Name             string   `json:"name"`
	SchluesselSHA256 string   `json:"schluessel_sha256"`
	Firmen           []string `json:"firmen"`
}

// Datei ist der Inhalt der Datei mit den Diensten.
type Datei struct {
	Dienste []Eintrag `json:"dienste"`
}

// Dienst ist ein angemeldeter Dienst.
type Dienst struct {
	Name   string
	firmen []string
}

// Darf sagt, ob der Dienst für diese Firma anfragen darf.
func (d Dienst) Darf(firma string) bool {
	return firma != "" && slices.Contains(d.firmen, firma)
}

type bekannt struct {
	Dienst
	hash []byte
}

// Liste hält die bekannten Dienste.
type Liste struct {
	alle []bekannt
}

// Lade liest die Datei und prüft jeden Eintrag. Ein fehlerhafter Eintrag
// verhindert den Start, statt still übergangen zu werden.
func Lade(pfad string) (*Liste, error) {
	daten, err := os.ReadFile(pfad)
	if err != nil {
		return nil, err
	}
	var d Datei
	if err := json.Unmarshal(daten, &d); err != nil {
		return nil, fmt.Errorf("dienste nicht lesbar: %w", err)
	}
	return Neu(d)
}

// Neu prüft die Einträge und baut die Liste.
func Neu(d Datei) (*Liste, error) {
	if len(d.Dienste) == 0 {
		return nil, errors.New("keine dienste eingetragen")
	}
	l := &Liste{}
	namen := map[string]bool{}
	hashes := map[string]bool{}
	for _, e := range d.Dienste {
		if !gueltigerName.MatchString(e.Name) {
			return nil, fmt.Errorf("dienst %q: ungültiger name", e.Name)
		}
		if namen[e.Name] {
			return nil, fmt.Errorf("dienst %q: doppelt", e.Name)
		}
		h, err := hex.DecodeString(e.SchluesselSHA256)
		if err != nil || len(h) != sha256.Size {
			return nil, fmt.Errorf("dienst %q: schluessel_sha256 muss 64 hex-zeichen haben", e.Name)
		}
		if hashes[e.SchluesselSHA256] {
			return nil, fmt.Errorf("dienst %q: schlüssel wird schon von einem anderen dienst benutzt", e.Name)
		}
		if len(e.Firmen) == 0 || slices.Contains(e.Firmen, "") {
			return nil, fmt.Errorf("dienst %q: firmen fehlen", e.Name)
		}
		namen[e.Name], hashes[e.SchluesselSHA256] = true, true
		l.alle = append(l.alle, bekannt{Dienst{e.Name, slices.Clone(e.Firmen)}, h})
	}
	return l, nil
}

// Finde liefert den Dienst zu einem Schlüssel. Verglichen wird in fester Zeit
// mit allen Einträgen, damit die Antwortzeit nichts verrät.
func (l *Liste) Finde(schluessel string) (Dienst, bool) {
	if len(schluessel) < MinLaenge {
		return Dienst{}, false
	}
	h := sha256.Sum256([]byte(schluessel))
	var treffer Dienst
	gefunden := 0
	for _, b := range l.alle {
		if subtle.ConstantTimeCompare(h[:], b.hash) == 1 {
			treffer, gefunden = b.Dienst, 1
		}
	}
	return treffer, gefunden == 1
}

// NeuerSchluessel erzeugt einen zufälligen Schlüssel und seinen SHA-256 (hex).
func NeuerSchluessel() (schluessel, hash string, err error) {
	roh := make([]byte, 32)
	if _, err := rand.Read(roh); err != nil {
		return "", "", err
	}
	schluessel = base64.RawURLEncoding.EncodeToString(roh)
	h := sha256.Sum256([]byte(schluessel))
	return schluessel, hex.EncodeToString(h[:]), nil
}
