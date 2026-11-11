// Package api macht den Pförtner über HTTP erreichbar.
//
// Angeboten wird nur, was der Orchestrator braucht: prüfen, ausführen und stoppen.
// Erlaubnisse vergeben geht bewusst nicht über diesen Weg, damit der
// Orchestrator sich nie selbst Rechte geben kann (Grundregel 1).
//
// Freigaben (Bestätigung eines Menschen für einen wartenden Schritt) darf nur
// ein Dienst weitergeben, bei dem das eigens eingetragen ist ("darf_freigeben").
// Das ist das Dashboard, das Menschen selbst anmeldet. Der Schlüssel des
// Orchestrators kann keine Freigabe entscheiden.
//
// Jede Anfrage außer /gesund braucht den Schlüssel eines bekannten Dienstes
// (Kopfzeile "Authorization: Bearer ..."), und der Dienst darf nur für die
// Firmen anfragen, die bei ihm eingetragen sind.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/aga-coder/boticstudio/kernel/internal/ausfuehrung"
	"github.com/aga-coder/boticstudio/kernel/internal/dienste"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
)

const maxGroesse = 1 << 20

// Stoppauftrag hält eine Aufgabe an.
type Stoppauftrag struct {
	Firma   string `json:"firma"`
	Aufgabe string `json:"aufgabe"`
	Person  string `json:"person"`
}

// Freigabeauftrag ist die Entscheidung eines Menschen über einen wartenden Schritt.
type Freigabeauftrag struct {
	Firma      string `json:"firma"`
	Freigabe   string `json:"freigabe"`
	Person     string `json:"person"`
	Bestaetigt bool   `json:"bestaetigt"`
}

// Freigabenfrage fragt, was in einer Firma auf einen Menschen wartet.
type Freigabenfrage struct {
	Firma  string `json:"firma"`
	Person string `json:"person"`
}

type dienstSchluessel struct{}

// Handler liefert alle Wege des Pförtners. Ohne Liste der Dienste nimmt er nichts an.
func Handler(p *pfoertner.Pfoertner, f *ausfuehrung.Ausfuehrer, d *dienste.Liste, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /gesund", func(w http.ResponseWriter, _ *http.Request) {
		schreibeJSON(w, http.StatusOK, map[string]string{"zustand": "ok"})
	})

	// fuer prüft, ob der angemeldete Dienst für die Firma anfragen darf.
	fuer := func(w http.ResponseWriter, r *http.Request, firma string) (string, bool) {
		dienst, _ := r.Context().Value(dienstSchluessel{}).(dienste.Dienst)
		if !dienst.Darf(firma) {
			log.Warn("dienst für firma abgelehnt", "dienst", dienst.Name, "firma", firma, "weg", r.URL.Path)
			schreibeJSON(w, http.StatusForbidden, map[string]string{"fehler": "dienst darf für diese firma nicht anfragen"})
			return "", false
		}
		return dienst.Name, true
	}

	mux.HandleFunc("POST /v1/pruefen", func(w http.ResponseWriter, r *http.Request) {
		var a pfoertner.Anfrage
		if err := lies(w, r, &a); err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		dienst, ok := fuer(w, r, a.Firma)
		if !ok {
			return
		}
		ent := p.Pruefe(r.Context(), a)
		log.Info("pruefung", "dienst", dienst, "firma", a.Firma, "aufgabe", a.Aufgabe,
			"faehigkeit", a.Faehigkeit, "ergebnis", ent.Ergebnis, "audit_nr", ent.AuditNr)
		schreibeJSON(w, http.StatusOK, ent)
	})

	mux.HandleFunc("POST /v1/ausfuehren", func(w http.ResponseWriter, r *http.Request) {
		var auftrag ausfuehrung.Auftrag
		if err := lies(w, r, &auftrag); err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		dienst, ok := fuer(w, r, auftrag.Anfrage.Firma)
		if !ok {
			return
		}
		antwort, err := f.Fuehre(r.Context(), auftrag)
		if err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		// Eingaben und Ergebnis kommen nie ins Log, sie können Mailinhalte enthalten.
		a := auftrag.Anfrage
		log.Info("ausfuehrung", "dienst", dienst, "firma", a.Firma, "aufgabe", a.Aufgabe, "faehigkeit", a.Faehigkeit,
			"ergebnis", antwort.Entscheidung.Ergebnis, "fehler", antwort.Fehler != "")
		schreibeJSON(w, http.StatusOK, antwort)
	})

	mux.HandleFunc("POST /v1/stoppen", func(w http.ResponseWriter, r *http.Request) {
		var s Stoppauftrag
		if err := lies(w, r, &s); err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		if _, ok := fuer(w, r, s.Firma); !ok {
			return
		}
		if err := p.Stoppe(r.Context(), s.Firma, s.Aufgabe, s.Person); err != nil {
			// Keine Details nach außen: Speicherfehler könnten interne Angaben enthalten.
			log.Warn("stopp abgelehnt", "firma", s.Firma, "aufgabe", s.Aufgabe)
			schreibeJSON(w, http.StatusForbidden, map[string]string{"fehler": "stopp nicht möglich"})
			return
		}
		schreibeJSON(w, http.StatusOK, map[string]string{"zustand": "gestoppt"})
	})

	// freigeben prüft zusätzlich, ob der Dienst Freigaben weitergeben darf.
	freigeben := func(w http.ResponseWriter, r *http.Request, firma string) (string, bool) {
		name, ok := fuer(w, r, firma)
		if !ok {
			return "", false
		}
		if dienst, _ := r.Context().Value(dienstSchluessel{}).(dienste.Dienst); !dienst.DarfFreigeben() {
			log.Warn("dienst darf keine freigaben weitergeben", "dienst", name, "firma", firma, "weg", r.URL.Path)
			schreibeJSON(w, http.StatusForbidden, map[string]string{"fehler": "dienst darf keine freigaben weitergeben"})
			return "", false
		}
		return name, true
	}

	mux.HandleFunc("POST /v1/freigaben/offen", func(w http.ResponseWriter, r *http.Request) {
		var frage Freigabenfrage
		if err := lies(w, r, &frage); err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		if _, ok := freigeben(w, r, frage.Firma); !ok {
			return
		}
		liste, err := p.OffeneFreigaben(r.Context(), frage.Firma, frage.Person)
		if err != nil {
			schreibeJSON(w, http.StatusForbidden, map[string]string{"fehler": "freigaben nicht lesbar"})
			return
		}
		if liste == nil {
			liste = []pfoertner.Freigabe{}
		}
		schreibeJSON(w, http.StatusOK, map[string]any{"freigaben": liste})
	})

	mux.HandleFunc("POST /v1/freigaben/entscheiden", func(w http.ResponseWriter, r *http.Request) {
		var auftrag Freigabeauftrag
		if err := lies(w, r, &auftrag); err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		dienst, ok := freigeben(w, r, auftrag.Firma)
		if !ok {
			return
		}
		frei, err := p.EntscheideFreigabe(r.Context(), auftrag.Firma, auftrag.Freigabe, auftrag.Person, auftrag.Bestaetigt)
		if err != nil {
			// Nur der eigene, datenfreie Grund geht hinaus, nie ein Speicherfehler.
			grund := "freigabe nicht möglich"
			if errors.Is(err, pfoertner.ErrFreigabe) {
				grund = err.Error()
			}
			log.Warn("freigabe nicht entschieden", "dienst", dienst, "firma", auftrag.Firma, "freigabe", auftrag.Freigabe)
			schreibeJSON(w, http.StatusConflict, map[string]string{"fehler": grund})
			return
		}
		log.Info("freigabe entschieden", "dienst", dienst, "firma", auftrag.Firma, "freigabe", frei.ID,
			"aufgabe", frei.Aufgabe, "stand", frei.Stand)
		schreibeJSON(w, http.StatusOK, frei)
	})

	return anmeldung(mux, d, log)
}

// anmeldung lässt nur bekannte Dienste durch. /gesund bleibt offen, es verrät nichts.
func anmeldung(weiter http.Handler, d *dienste.Liste, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gesund" {
			weiter.ServeHTTP(w, r)
			return
		}
		schluessel, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		var dienst dienste.Dienst
		if ok && d != nil {
			dienst, ok = d.Finde(schluessel)
		} else {
			ok = false
		}
		if !ok {
			// Nie den Schlüssel ins Log, nur dass einer fehlte oder falsch war.
			log.Warn("dienst nicht angemeldet", "weg", r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Bearer realm="pfoertner"`)
			schreibeJSON(w, http.StatusUnauthorized, map[string]string{"fehler": "dienst nicht angemeldet"})
			return
		}
		weiter.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), dienstSchluessel{}, dienst)))
	})
}

func lies(w http.ResponseWriter, r *http.Request, ziel any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGroesse))
	dec.DisallowUnknownFields()
	if err := dec.Decode(ziel); err != nil {
		return errors.New("anfrage nicht lesbar: " + err.Error())
	}
	if dec.More() {
		return errors.New("anfrage enthält mehr als ein json-objekt")
	}
	return nil
}

func schreibeJSON(w http.ResponseWriter, status int, wert any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(wert)
}
