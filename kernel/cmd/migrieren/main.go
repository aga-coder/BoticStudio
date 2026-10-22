// Befehl migrieren bringt das Datenbank-Schema auf den neuesten Stand.
//
// Er läuft mit der Rolle, der die Tabellen gehören (Verbindung aus
// BOTICSTUDIO_DATENBANK_EIGENTUEMER), nie mit der Rolle des Pförtners.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/aga-coder/boticstudio/kernel/internal/datenbank"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	dsn := os.Getenv("BOTICSTUDIO_DATENBANK_EIGENTUEMER")
	if dsn == "" {
		log.Error("BOTICSTUDIO_DATENBANK_EIGENTUEMER fehlt")
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := datenbank.Oeffne(ctx, dsn)
	if err != nil {
		log.Error("migration abgebrochen", "fehler", err)
		os.Exit(1)
	}
	defer pool.Close()
	neu, err := datenbank.Migriere(ctx, pool)
	if err != nil {
		log.Error("migration fehlgeschlagen", "fehler", err)
		os.Exit(1)
	}
	log.Info("schema aktuell", "neu_angewendet", neu)
}
