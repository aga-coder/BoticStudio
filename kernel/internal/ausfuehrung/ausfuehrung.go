// Package ausfuehrung führt einen erlaubten Schritt aus.
//
// Ablauf: Die Ausführung bildet zu jeder Eingabe einen Fingerabdruck, damit
// der Pförtner prüfen kann, ob genau diese Daten vorgeprüft sind. Der Pförtner prüft. Nur bei "erlaubt" holt die Ausführung die
// Zugangsdaten aus dem Tresor, gibt sie direkt an das Modul der Fähigkeit
// und reicht das Ergebnis zurück. Zugangsdaten verlassen dabei nie den Weg
// Tresor → Modul: Sie stehen weder im Ergebnis noch im Audit noch in einer
// Fehlermeldung.
package ausfuehrung

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
	"github.com/aga-coder/boticstudio/kernel/internal/tresor"
)

// Reservierte Eingaben setzt nur der Pförtner, nie der Orchestrator.
const (
	FeldBereich = "bereich"
	FeldZugang  = "zugang"
	FeldZiel    = "ziel"
)

// Aufrufer ruft ein Modul auf, z. B. über MCP.
type Aufrufer interface {
	Rufe(ctx context.Context, adresse, werkzeug string, args map[string]any) (json.RawMessage, error)
}

// Auftrag ist ein Schritt mit seinen Eingaben.
type Auftrag struct {
	Anfrage  pfoertner.Anfrage `json:"anfrage"`
	Eingaben map[string]any    `json:"eingaben,omitempty"`
}

// Antwort enthält die Entscheidung und, wenn ausgeführt, das Ergebnis.
type Antwort struct {
	Entscheidung pfoertner.Entscheidung `json:"entscheidung"`
	Ergebnis     json.RawMessage        `json:"ergebnis,omitempty"`
	Fehler       string                 `json:"fehler,omitempty"`
}

// ErrUngueltig heißt: Der Auftrag ist falsch gebaut und wurde nicht geprüft.
var ErrUngueltig = errors.New("auftrag ungültig")

// Ausfuehrer verbindet Pförtner, Tresor und Module.
type Ausfuehrer struct {
	Pfoertner *pfoertner.Pfoertner
	Katalog   *katalog.Katalog
	Tresor    tresor.Tresor
	Aufrufer  Aufrufer
	// Module ordnet "name@version" der Adresse des Moduls zu.
	Module map[string]string
	// Zeitlimit je Aufruf. Hängt ein Modul, laufen die anderen weiter.
	Zeitlimit time.Duration
}

// Fuehre prüft den Auftrag und führt ihn aus, wenn er erlaubt ist.
func (f *Ausfuehrer) Fuehre(ctx context.Context, auftrag Auftrag) (Antwort, error) {
	a := auftrag.Anfrage
	z, ok := f.Katalog.Finde(a.Faehigkeit, a.Version)
	if ok {
		if err := pruefeEingaben(z, auftrag.Eingaben); err != nil {
			return Antwort{}, err
		}
	}

	// Fingerabdrücke setzt nur die Ausführung, aus den echten Eingaben.
	a.Daten = make(map[string]string, len(auftrag.Eingaben))
	for k, v := range auftrag.Eingaben {
		fp, err := pfoertner.Fingerabdruck(v)
		if err != nil {
			return Antwort{}, fmt.Errorf("%w: eingabe %q nicht lesbar", ErrUngueltig, k)
		}
		a.Daten[k] = fp
	}

	ent := f.Pfoertner.Pruefe(ctx, a)
	antwort := Antwort{Entscheidung: ent}
	if ent.Ergebnis != pfoertner.Erlaubt {
		return antwort, nil
	}

	args := make(map[string]any, len(auftrag.Eingaben)+2)
	for k, v := range auftrag.Eingaben {
		args[k] = v
	}
	if z.Recht != beipackzettel.KeinRecht {
		args[FeldBereich] = a.Bereich
	}
	if a.Ziel != nil {
		// Das Modul sendet nur an das Ziel, das der Pförtner geprüft hat.
		args[FeldZiel] = map[string]any{"art": a.Ziel.Art, "adresse": a.Ziel.Adresse}
	}

	var geheim []string
	if z.Zugang != "" {
		zugang, werte, err := f.holeZugang(a.Firma, z.Zugang, a.Bereich)
		if err != nil {
			return f.fehler(ctx, antwort, a, "Zugangsdaten fehlen für diesen Bereich.", err.Error(), nil), nil
		}
		args[FeldZugang] = zugang
		geheim = werte
	}

	adresse, ok := f.Module[z.Schluessel()]
	if !ok {
		return f.fehler(ctx, antwort, a, "Das Modul dieser Fähigkeit ist nicht eingerichtet.", "kein modul für "+z.Schluessel(), nil), nil
	}

	limit := f.Zeitlimit
	if limit <= 0 {
		limit = 60 * time.Second
	}
	aufrufCtx, abbrechen := context.WithTimeout(ctx, limit)
	defer abbrechen()

	ergebnis, err := f.Aufrufer.Rufe(aufrufCtx, adresse, z.Name, args)
	if err != nil {
		return f.fehler(ctx, antwort, a, "Die Fähigkeit hat einen Fehler gemeldet: "+schwaerze(err.Error(), geheim), err.Error(), geheim), nil
	}
	if enthaeltGeheimnis(string(ergebnis), geheim) {
		return f.fehler(ctx, antwort, a, "Das Ergebnis enthielt Zugangsdaten und wurde zurückgehalten.", "geheimnis im ergebnis", nil), nil
	}

	if len(z.Prueft) > 0 {
		var pruefung struct {
			Markierungen *[]string `json:"markierungen"`
		}
		if err := json.Unmarshal(ergebnis, &pruefung); err != nil || pruefung.Markierungen == nil {
			return f.fehler(ctx, antwort, a, "Das Prüfergebnis war nicht lesbar.", "prüfergebnis ohne markierungen", nil), nil
		}
		if err := f.Pfoertner.MerkeVorpruefung(ctx, a, *pruefung.Markierungen); err != nil {
			antwort.Fehler = "Das Prüfergebnis konnte nicht gespeichert werden."
			return antwort, nil
		}
	}

	if err := f.Pfoertner.Vermerke(ctx, a.Firma, "ausgefuehrt", map[string]string{
		"aufgabe": a.Aufgabe, "faehigkeit": z.Schluessel(), "bereich": a.Bereich,
		"pruefung_nr": strconv.Itoa(ent.AuditNr), "bytes": strconv.Itoa(len(ergebnis)),
	}); err != nil {
		antwort.Fehler = "Der Auditor kann gerade nicht schreiben. Das Ergebnis wird nicht weitergegeben."
		return antwort, nil
	}
	antwort.Ergebnis = ergebnis
	return antwort, nil
}

// fehler schreibt den Fehler ins Audit (geschwärzt) und gibt ihn zurück.
func (f *Ausfuehrer) fehler(ctx context.Context, antwort Antwort, a pfoertner.Anfrage, fuerNutzer, intern string, geheim []string) Antwort {
	_ = f.Pfoertner.Vermerke(ctx, a.Firma, "ausfuehrung_fehler", map[string]string{
		"aufgabe": a.Aufgabe, "faehigkeit": a.Faehigkeit + "@" + a.Version, "bereich": a.Bereich,
		"fehler": schwaerze(intern, geheim),
	})
	antwort.Fehler = fuerNutzer
	return antwort
}

// pruefeEingaben lässt nur Eingaben zu, die der Beipackzettel nennt.
// Bereich und Zugang setzt allein der Pförtner.
func pruefeEingaben(z beipackzettel.Zettel, eingaben map[string]any) error {
	for k := range eingaben {
		if k == FeldBereich || k == FeldZugang || k == FeldZiel {
			return fmt.Errorf("%w: eingabe %q setzt nur der pförtner", ErrUngueltig, k)
		}
		bekannt := false
		for _, e := range z.Eingaben {
			if e == k {
				bekannt = true
				break
			}
		}
		if !bekannt {
			return fmt.Errorf("%w: %s kennt die eingabe %q nicht", ErrUngueltig, z.Name, k)
		}
	}
	return nil
}

// holeZugang sucht im Tresor den Zugang zum Bereich, vom genauesten Pfad
// zum allgemeinsten, aber nur unter der Art, die der Beipackzettel nennt.
// Ein Tresor-Eintrag ist ein JSON-Objekt.
func (f *Ausfuehrer) holeZugang(firma, art, bereich string) (map[string]any, []string, error) {
	teile := strings.Split(strings.Trim(bereich, "/"), "/")
	if len(teile) == 0 || teile[0] != art {
		return nil, nil, fmt.Errorf("bereich %q passt nicht zur zugangsart %q", bereich, art)
	}
	for n := len(teile); n >= 1; n-- {
		g, err := f.Tresor.Hole(firma, strings.Join(teile[:n], "/"))
		if errors.Is(err, tresor.ErrNichtGefunden) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		var zugang map[string]any
		if err := json.Unmarshal([]byte(g.Offenlegen()), &zugang); err != nil {
			return nil, nil, errors.New("tresor-eintrag ist kein json-objekt")
		}
		return zugang, tresor.GeheimeWerte(zugang), nil
	}
	return nil, nil, fmt.Errorf("kein zugang für %q", bereich)
}

func enthaeltGeheimnis(text string, geheim []string) bool {
	for _, g := range geheim {
		if strings.Contains(text, g) {
			return true
		}
	}
	return false
}

func schwaerze(text string, geheim []string) string {
	for _, g := range geheim {
		text = strings.ReplaceAll(text, g, tresor.Verdeckt)
	}
	return text
}
