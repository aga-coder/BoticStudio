// Package beipackzettel liest und prüft den Beipackzettel einer Fähigkeit.
//
// Regel B: Der Beipackzettel hat Fassungen. Neue Felder dürfen dazukommen,
// alte Felder werden nie gestrichen oder umgedeutet. Fehlt einem älteren
// Zettel ein Feld, gilt der strengste Wert. Einen Zettel aus einer neueren
// Fassung, als dieser Pförtner kennt, lehnt er ab.
package beipackzettel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// AktuelleFassung ist die neueste Fassung, die dieser Pförtner versteht.
//
// Fassung 1: Grundfelder.
// Fassung 2: dazu "zugang".
const AktuelleFassung = 2

// KeinRecht ist der ausdrücklich erklärte Wert für Fähigkeiten,
// die keine Erlaubnis brauchen.
const KeinRecht = "keins"

// Risiko ist die Risikostufe einer Fähigkeit.
type Risiko string

const (
	RisikoNiedrig Risiko = "niedrig"
	RisikoMittel  Risiko = "mittel"
	RisikoHoch    Risiko = "hoch"
)

// Zettel ist ein geprüfter Beipackzettel.
type Zettel struct {
	Fassung      int      `json:"fassung"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Beschreibung string   `json:"beschreibung"`
	Eingaben     []string `json:"eingaben"`
	Ausgaben     []string `json:"ausgaben"`
	// Recht ist das nötige Recht, z. B. "bereich.tun", oder KeinRecht.
	Recht string `json:"recht"`
	// Bereich sagt, wofür das Recht gilt, z. B. "postfach/ordner".
	// Der Pförtner liest ihn nur als Text, er deutet ihn nicht.
	Bereich         string `json:"bereich"`
	Risiko          Risiko `json:"risiko"`
	WirktNachAussen bool   `json:"wirkt_nach_aussen"`
	// Datenausgang listet die Arten von Zielen, an die Daten gehen dürfen.
	// Leer heißt: Es verlassen keine Daten BoticStudio.
	Datenausgang []string `json:"datenausgang"`
	// Zugang nennt die Art von Zugangsdaten, die die Fähigkeit braucht,
	// z. B. "postfach". Leer heißt: keine. Seit Fassung 2.
	Zugang string `json:"zugang"`
}

// Schluessel ist die eindeutige Kennung "name@version".
func (z Zettel) Schluessel() string { return z.Name + "@" + z.Version }

// DarfSenden sagt, ob der Zettel Daten an diese Art von Ziel erlaubt.
func (z Zettel) DarfSenden(art string) bool {
	for _, a := range z.Datenausgang {
		if a == art {
			return true
		}
	}
	return false
}

// roh hält die Felder so, wie sie im Text stehen. Zeiger zeigen,
// ob ein Feld fehlt, damit der strengste Wert eingesetzt werden kann.
type roh struct {
	Fassung         *int     `json:"fassung"`
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	Beschreibung    string   `json:"beschreibung"`
	Eingaben        []string `json:"eingaben"`
	Ausgaben        []string `json:"ausgaben"`
	Recht           string   `json:"recht"`
	Bereich         string   `json:"bereich"`
	Risiko          *Risiko  `json:"risiko"`
	WirktNachAussen *bool    `json:"wirkt_nach_aussen"`
	Datenausgang    []string `json:"datenausgang"`
	Zugang          *string  `json:"zugang"`
}

// Lies prüft einen Beipackzettel im JSON-Format.
func Lies(daten []byte) (Zettel, error) {
	dec := json.NewDecoder(bytes.NewReader(daten))
	// Unbekannte Felder sind ein Tippfehler oder eine neuere Fassung.
	// Beides darf nicht stillschweigend durchgehen.
	dec.DisallowUnknownFields()
	var r roh
	if err := dec.Decode(&r); err != nil {
		return Zettel{}, fmt.Errorf("beipackzettel nicht lesbar: %w", err)
	}

	if r.Fassung == nil {
		return Zettel{}, errors.New("beipackzettel: feld \"fassung\" fehlt")
	}
	if *r.Fassung < 1 || *r.Fassung > AktuelleFassung {
		return Zettel{}, fmt.Errorf("beipackzettel: fassung %d ist diesem pförtner unbekannt (kennt bis %d)", *r.Fassung, AktuelleFassung)
	}

	var fehlt []string
	for feld, wert := range map[string]string{
		"name": r.Name, "version": r.Version, "beschreibung": r.Beschreibung, "recht": r.Recht,
	} {
		if strings.TrimSpace(wert) == "" {
			fehlt = append(fehlt, feld)
		}
	}
	if len(fehlt) > 0 {
		sort.Strings(fehlt)
		return Zettel{}, fmt.Errorf("beipackzettel %q: pflichtfelder fehlen: %s", r.Name, strings.Join(fehlt, ", "))
	}
	if strings.Contains(r.Name, "@") || strings.Contains(r.Version, "@") {
		return Zettel{}, fmt.Errorf("beipackzettel %q: name und version dürfen kein @ enthalten", r.Name)
	}
	if r.Zugang != nil && *r.Fassung < 2 {
		return Zettel{}, fmt.Errorf("beipackzettel %q: feld \"zugang\" gibt es erst ab fassung 2", r.Name)
	}
	if r.Recht != KeinRecht && strings.TrimSpace(r.Bereich) == "" {
		return Zettel{}, fmt.Errorf("beipackzettel %q: zu recht %q fehlt der bereich", r.Name, r.Recht)
	}

	z := Zettel{
		Fassung:      *r.Fassung,
		Name:         r.Name,
		Version:      r.Version,
		Beschreibung: r.Beschreibung,
		Eingaben:     r.Eingaben,
		Ausgaben:     r.Ausgaben,
		Recht:        r.Recht,
		Bereich:      r.Bereich,
		Datenausgang: r.Datenausgang,
		// Strengste Werte, falls nicht angegeben.
		Risiko:          RisikoHoch,
		WirktNachAussen: true,
	}
	if r.Risiko != nil {
		switch *r.Risiko {
		case RisikoNiedrig, RisikoMittel, RisikoHoch:
			z.Risiko = *r.Risiko
		default:
			return Zettel{}, fmt.Errorf("beipackzettel %q: unbekannte risikostufe %q", r.Name, *r.Risiko)
		}
	}
	if r.WirktNachAussen != nil {
		z.WirktNachAussen = *r.WirktNachAussen
	}
	// Fehlt der Zugang, bekommt die Fähigkeit keine Zugangsdaten.
	if r.Zugang != nil {
		if strings.Contains(*r.Zugang, "/") {
			return Zettel{}, fmt.Errorf("beipackzettel %q: zugang darf kein / enthalten", r.Name)
		}
		if *r.Zugang != "" && r.Recht == KeinRecht {
			return Zettel{}, fmt.Errorf("beipackzettel %q: wer zugangsdaten braucht, braucht auch ein recht", r.Name)
		}
		z.Zugang = *r.Zugang
	}
	return z, nil
}
