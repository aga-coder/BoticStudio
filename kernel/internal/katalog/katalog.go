// Package katalog hält alle bekannten Beipackzettel.
//
// Der Katalog wird aus Dateien geladen. Eine neue Fähigkeit kommt dazu,
// indem ihr Beipackzettel in den Katalog-Ordner gelegt wird. Am Pförtner
// ändert sich dafür nichts (Regel A).
package katalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
)

// Katalog ist sicher für gleichzeitige Zugriffe.
type Katalog struct {
	mu     sync.RWMutex
	zettel map[string]beipackzettel.Zettel
}

// Neu erstellt einen leeren Katalog.
func Neu() *Katalog {
	return &Katalog{zettel: map[string]beipackzettel.Zettel{}}
}

// LadeOrdner liest alle *.json-Dateien eines Ordners als Beipackzettel.
// Ist ein Zettel fehlerhaft, wird nichts übernommen.
func LadeOrdner(ordner string) (*Katalog, error) {
	pfade, err := filepath.Glob(filepath.Join(ordner, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(pfade)
	k := Neu()
	for _, pfad := range pfade {
		daten, err := os.ReadFile(pfad)
		if err != nil {
			return nil, err
		}
		z, err := beipackzettel.Lies(daten)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(pfad), err)
		}
		if err := k.Fuege(z); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(pfad), err)
		}
	}
	return k, nil
}

// Fuege nimmt einen geprüften Zettel auf. Eine Version, die es schon gibt,
// darf nicht überschrieben werden: Aufgaben verlassen sich auf sie.
func (k *Katalog) Fuege(z beipackzettel.Zettel) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, da := k.zettel[z.Schluessel()]; da {
		return fmt.Errorf("katalog: %s gibt es schon", z.Schluessel())
	}
	k.zettel[z.Schluessel()] = z
	return nil
}

// Finde liefert den Zettel zu Name und Version.
func (k *Katalog) Finde(name, version string) (beipackzettel.Zettel, bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	z, ok := k.zettel[name+"@"+version]
	return z, ok
}

// Alle liefert alle Zettel, sortiert nach Schlüssel.
func (k *Katalog) Alle() []beipackzettel.Zettel {
	k.mu.RLock()
	defer k.mu.RUnlock()
	alle := make([]beipackzettel.Zettel, 0, len(k.zettel))
	for _, z := range k.zettel {
		alle = append(alle, z)
	}
	sort.Slice(alle, func(i, j int) bool { return alle[i].Schluessel() < alle[j].Schluessel() })
	return alle
}
