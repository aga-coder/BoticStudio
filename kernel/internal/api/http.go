// Package api macht den Pförtner über HTTP erreichbar.
//
// Angeboten wird nur, was der Orchestrator braucht: prüfen, ausführen und stoppen.
// Erlaubnisse vergeben geht bewusst nicht über diesen Weg, damit der
// Orchestrator sich nie selbst Rechte geben kann (Grundregel 1). Das kommt
// mit der Oberfläche und einer eigenen Anmeldung der Aufrufer.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/aga-coder/boticstudio/kernel/internal/ausfuehrung"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
)

const maxGroesse = 1 << 20

// Stoppauftrag hält eine Aufgabe an.
type Stoppauftrag struct {
	Firma   string `json:"firma"`
	Aufgabe string `json:"aufgabe"`
	Person  string `json:"person"`
}

// Handler liefert alle Wege des Pförtners.
func Handler(p *pfoertner.Pfoertner, f *ausfuehrung.Ausfuehrer, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /gesund", func(w http.ResponseWriter, _ *http.Request) {
		schreibeJSON(w, http.StatusOK, map[string]string{"zustand": "ok"})
	})

	mux.HandleFunc("POST /v1/pruefen", func(w http.ResponseWriter, r *http.Request) {
		var a pfoertner.Anfrage
		if err := lies(w, r, &a); err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		ent := p.Pruefe(r.Context(), a)
		log.Info("pruefung", "firma", a.Firma, "aufgabe", a.Aufgabe,
			"faehigkeit", a.Faehigkeit, "ergebnis", ent.Ergebnis, "audit_nr", ent.AuditNr)
		schreibeJSON(w, http.StatusOK, ent)
	})

	mux.HandleFunc("POST /v1/ausfuehren", func(w http.ResponseWriter, r *http.Request) {
		var auftrag ausfuehrung.Auftrag
		if err := lies(w, r, &auftrag); err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		antwort, err := f.Fuehre(r.Context(), auftrag)
		if err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
			return
		}
		// Eingaben und Ergebnis kommen nie ins Log, sie können Mailinhalte enthalten.
		a := auftrag.Anfrage
		log.Info("ausfuehrung", "firma", a.Firma, "aufgabe", a.Aufgabe, "faehigkeit", a.Faehigkeit,
			"ergebnis", antwort.Entscheidung.Ergebnis, "fehler", antwort.Fehler != "")
		schreibeJSON(w, http.StatusOK, antwort)
	})

	mux.HandleFunc("POST /v1/stoppen", func(w http.ResponseWriter, r *http.Request) {
		var s Stoppauftrag
		if err := lies(w, r, &s); err != nil {
			schreibeJSON(w, http.StatusBadRequest, map[string]string{"fehler": err.Error()})
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

	return mux
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
