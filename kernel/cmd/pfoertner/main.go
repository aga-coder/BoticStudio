// Befehl pfoertner startet den Pförtner (Kernel) von BoticStudio.
//
// Die Verbindung zur Datenbank kommt aus der Umgebungsvariable
// BOTICSTUDIO_DATENBANK, nie aus einem Aufrufparameter, damit das Passwort
// nicht in der Prozessliste steht. Fehlt sie, arbeitet der Pförtner nur im
// Arbeitsspeicher (zum lokalen Ausprobieren).
//
// Ohne Datei mit Diensten (-dienste) startet der Pförtner nicht: Er nimmt nur
// Anfragen von Diensten an, deren Schlüssel er kennt.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/aga-coder/boticstudio/kernel/internal/api"
	"github.com/aga-coder/boticstudio/kernel/internal/ausfuehrung"
	"github.com/aga-coder/boticstudio/kernel/internal/datenbank"
	"github.com/aga-coder/boticstudio/kernel/internal/dienste"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	"github.com/aga-coder/boticstudio/kernel/internal/mcpanschluss"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
	"github.com/aga-coder/boticstudio/kernel/internal/tresor"
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
	moduldatei := flag.String("module", "", "Datei, die jeder Fähigkeit (name@version) die Adresse ihres Moduls zuordnet")
	tresordatei := flag.String("tresor", "", "nur lokal: Datei mit Test-Zugängen (nie ins Repo)")
	dienstdatei := flag.String("dienste", "", "Datei mit den Diensten, die anfragen dürfen (siehe cmd/dienstschluessel)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := starte(einstellung{*adresse, *katalogOrdner, *testdatei, *moduldatei, *tresordatei, *dienstdatei}, log); err != nil {
		log.Error("pförtner beendet", "fehler", err)
		os.Exit(1)
	}
}

type einstellung struct {
	adresse, katalog, testdaten, module, tresor, dienste string
}

func starte(e einstellung, log *slog.Logger) error {
	if e.dienste == "" {
		return errors.New("-dienste fehlt: ohne bekannte Dienste nimmt der Pförtner nichts an")
	}
	d, err := dienste.Lade(e.dienste)
	if err != nil {
		return fmt.Errorf("dienste: %w", err)
	}
	k, err := katalog.LadeOrdner(e.katalog)
	if err != nil {
		return fmt.Errorf("katalog: %w", err)
	}
	for _, z := range k.Alle() {
		log.Info("fähigkeit geladen", "faehigkeit", z.Schluessel(), "recht", z.Recht, "risiko", z.Risiko)
	}

	ctx := context.Background()
	var speicher pfoertner.Speicher = pfoertner.NeuerArbeitsspeicher()
	if dsn := os.Getenv("BOTICSTUDIO_DATENBANK"); dsn != "" {
		pool, err := datenbank.Oeffne(ctx, dsn)
		if err != nil {
			return err
		}
		defer pool.Close()
		speicher = datenbank.Neu(pool)
		log.Info("speicher", "art", "postgresql")
	} else {
		log.Warn("speicher", "art", "arbeitsspeicher", "hinweis", "nichts wird dauerhaft gespeichert")
	}
	p := pfoertner.Neu(pfoertner.StandardEinstellungen(), k, speicher)
	if e.testdaten != "" {
		err := ladeTestdaten(ctx, p, e.testdaten)
		switch {
		case errors.Is(err, pfoertner.ErrGibtEsSchon):
			log.Info("testdaten schon vorhanden, nichts geladen")
		case err != nil:
			return fmt.Errorf("testdaten: %w", err)
		}
	}

	module := map[string]string{}
	if e.module != "" {
		daten, err := os.ReadFile(e.module)
		if err != nil {
			return fmt.Errorf("module: %w", err)
		}
		if err := json.Unmarshal(daten, &module); err != nil {
			return fmt.Errorf("module: %w", err)
		}
	}
	var t tresor.Tresor = tresor.NeuSpeicher()
	if e.tresor != "" {
		if t, err = tresor.LadeDatei(e.tresor); err != nil {
			return fmt.Errorf("tresor: %w", err)
		}
	}
	f := &ausfuehrung.Ausfuehrer{
		Pfoertner: p, Katalog: k, Tresor: t, Module: module,
		Aufrufer:  mcpanschluss.Anschluss{HTTP: &http.Client{Timeout: 90 * time.Second}},
		Zeitlimit: 60 * time.Second,
	}

	server := &http.Server{
		Addr:              e.adresse,
		Handler:           api.Handler(p, f, d, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      90 * time.Second,
	}
	log.Info("pförtner hört", "adresse", e.adresse)
	return server.ListenAndServe()
}

func ladeTestdaten(ctx context.Context, p *pfoertner.Pfoertner, pfad string) error {
	daten, err := os.ReadFile(pfad)
	if err != nil {
		return err
	}
	var t testdaten
	if err := json.Unmarshal(daten, &t); err != nil {
		return err
	}
	for _, f := range t.Firmen {
		if err := p.LegeFirmaAn(ctx, f); err != nil {
			return err
		}
	}
	for _, pe := range t.Personen {
		if err := p.LegePersonAn(ctx, pe); err != nil {
			return err
		}
	}
	for _, e := range t.Erlaubnisse {
		if err := p.ErteileErlaubnis(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
