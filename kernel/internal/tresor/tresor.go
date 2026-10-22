// Package tresor verwahrt Zugangsdaten. Nur der Pförtner öffnet ihn.
//
// Ein Geheimnis lässt sich nicht ausgeben: weder als Text noch als JSON
// noch im Log. Es wird nur über Offenlegen direkt an einen Anschluss
// gegeben. So landen Zugangsdaten nie in einem KI-Text, im Audit oder
// in der Oberfläche (Grundregel 10).
package tresor

import (
	"errors"
	"log/slog"
	"sync"
)

// Verdeckt steht überall dort, wo ein Geheimnis ausgegeben würde.
const Verdeckt = "[verdeckt]"

// Geheimnis ist ein Zugangswert, der sich nicht versehentlich ausgeben lässt.
type Geheimnis struct{ wert string }

// NeuGeheimnis verpackt einen Zugangswert.
func NeuGeheimnis(wert string) Geheimnis { return Geheimnis{wert: wert} }

func (Geheimnis) String() string               { return Verdeckt }
func (Geheimnis) GoString() string             { return Verdeckt }
func (Geheimnis) MarshalJSON() ([]byte, error) { return []byte(`"` + Verdeckt + `"`), nil }
func (Geheimnis) MarshalText() ([]byte, error) { return []byte(Verdeckt), nil }
func (Geheimnis) LogValue() slog.Value         { return slog.StringValue(Verdeckt) }

// Offenlegen gibt den Wert heraus. Nur der Code, der einen Anschluss
// bedient, darf das aufrufen.
func (g Geheimnis) Offenlegen() string { return g.wert }

// ErrNichtGefunden heißt: Für diese Firma liegt kein solcher Zugang vor.
var ErrNichtGefunden = errors.New("tresor: zugang nicht gefunden")

// Tresor liefert Zugänge, immer nur innerhalb einer Firma.
type Tresor interface {
	Hole(firma, name string) (Geheimnis, error)
}

// Speicher ist ein Tresor im Arbeitsspeicher, als Platzhalter für den
// STACKIT Secrets Manager.
type Speicher struct {
	mu    sync.RWMutex
	daten map[string]map[string]Geheimnis
}

// NeuSpeicher erstellt einen leeren Tresor.
func NeuSpeicher() *Speicher {
	return &Speicher{daten: map[string]map[string]Geheimnis{}}
}

// Lege legt einen Zugang für eine Firma ab.
func (s *Speicher) Lege(firma, name string, g Geheimnis) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.daten[firma] == nil {
		s.daten[firma] = map[string]Geheimnis{}
	}
	s.daten[firma][name] = g
}

// Hole liefert einen Zugang, nur aus der eigenen Firma.
func (s *Speicher) Hole(firma, name string) (Geheimnis, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.daten[firma][name]
	if !ok {
		return Geheimnis{}, ErrNichtGefunden
	}
	return g, nil
}
