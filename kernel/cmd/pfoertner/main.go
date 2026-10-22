// Befehl pfoertner startet den Pförtner (Kernel) von BoticStudio.
//
// Stufe 1 läuft nur lokal zum Testen, mit erfundenen Testfirmen.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/aga-coder/boticstudio/kernel/internal/api"
	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
)

// testdaten ist der Inhalt einer Datei mit erfundenen Testfirmen.
type testdaten struct {
	Firmen      []pfoertner.Firma     `json:"firmen"`
	Personen    []pfoertner.Person    `json:"personen"`
	Erlaubnisse []pfoertner.Erlaubnis `json:"erlaubnisse"`
}

func main() {
	adresse := flag.String("adresse", "127.0.0.1:8470", "Adresse, auf der der Pförtner hört")
	katalogOrdner := flag.String("katalog", "../katalog", "Ordner mit den Beipackzetteln")
	testdatei := flag.String("testdaten", "", "optionale Datei mit erfundenen Testfirmen")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := starte(*adresse, *katalogOrdner, *testdatei, log); err != nil {
		log.Error("pförtner beendet", "fehler", err)
		os.Exit(1)
	}
}

func starte(adresse, katalogOrdner, testdatei string, log *slog.Logger) error {
	k, err := katalog.LadeOrdner(katalogOrdner)
	if err != nil {
		return fmt.Errorf("katalog: %w", err)
	}
	for _, z := range k.Alle() {
		log.Info("fähigkeit geladen", "faehigkeit", z.Schluessel(), "recht", z.Recht, "risiko", z.Risiko)
	}

	p := pfoertner.Neu(pfoertner.StandardEinstellungen(), k, auditor.NeuSpeicher())
	if testdatei != "" {
		if err := ladeTestdaten(p, testdatei); err != nil {
			return fmt.Errorf("testdaten: %w", err)
		}
	}

	server := &http.Server{
		Addr:              adresse,
		Handler:           api.Handler(p, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	log.Info("pförtner hört", "adresse", adresse)
	return server.ListenAndServe()
}

func ladeTestdaten(p *pfoertner.Pfoertner, pfad string) error {
	daten, err := os.ReadFile(pfad)
	if err != nil {
		return err
	}
	var t testdaten
	if err := json.Unmarshal(daten, &t); err != nil {
		return err
	}
	for _, f := range t.Firmen {
		if err := p.LegeFirmaAn(f); err != nil {
			return err
		}
	}
	for _, pe := range t.Personen {
		if err := p.LegePersonAn(pe); err != nil {
			return err
		}
	}
	for _, e := range t.Erlaubnisse {
		if err := p.ErteileErlaubnis(e); err != nil {
			return err
		}
	}
	return nil
}
