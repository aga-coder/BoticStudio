// Package testdb gibt Tests eine frische PostgreSQL-Datenbank.
//
// Die Tests laufen nur, wenn BOTICSTUDIO_TEST_DATENBANK gesetzt ist: eine
// Verbindung mit einer Rolle, die Datenbanken und Rollen anlegen darf
// (z. B. postgres://boticstudio_eigentuemer:...@localhost:5432/boticstudio_test).
// Jeder Test bekommt eine eigene Datenbank aus einer migrierten Vorlage.
// Der Pförtner verbindet sich darin als boticstudio_pfoertner, so wie im Betrieb.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aga-coder/boticstudio/kernel/internal/datenbank"
)

// Umgebungsvariable mit der Verbindung für Tests.
const Variable = "BOTICSTUDIO_TEST_DATENBANK"

// PfoertnerPasswort ist nur für Testdatenbanken.
const PfoertnerPasswort = "nur-fuer-tests-pfoertner"

var (
	vorlageEinmal sync.Once
	vorlage       string
	vorlageFehler error
)

// Datenbank ist eine frische Testdatenbank.
type Datenbank struct {
	// Pfoertner ist mit der Rolle boticstudio_pfoertner verbunden.
	Pfoertner *pgxpool.Pool
	// Eigentuemer ist mit der Rolle verbunden, der die Tabellen gehören.
	Eigentuemer *pgxpool.Pool
	// PfoertnerDSN ist die Verbindungsangabe der Pförtner-Rolle.
	PfoertnerDSN string
}

// Neu legt eine frische Datenbank an oder überspringt den Test.
func Neu(t testing.TB) *Datenbank {
	t.Helper()
	basis := os.Getenv(Variable)
	if basis == "" {
		t.Skip(Variable + " nicht gesetzt, PostgreSQL-Test übersprungen")
	}
	ctx := context.Background()
	vorlageEinmal.Do(func() { vorlage, vorlageFehler = legeVorlageAn(ctx, basis) })
	if vorlageFehler != nil {
		t.Fatal(vorlageFehler)
	}

	name := "boticstudio_t_" + zufall()
	admin, err := pgx.Connect(ctx, basis)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, vorlage)); err != nil {
		t.Fatal(err)
	}

	eigentuemerDSN := mitDatenbank(t, basis, name, "", "")
	pfoertnerDSN := mitDatenbank(t, basis, name, "boticstudio_pfoertner", PfoertnerPasswort)
	eigentuemer, err := datenbank.Oeffne(ctx, eigentuemerDSN)
	if err != nil {
		t.Fatal(err)
	}
	pf, err := datenbank.Oeffne(ctx, pfoertnerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pf.Close()
		eigentuemer.Close()
		if admin, err := pgx.Connect(ctx, basis); err == nil {
			_, _ = admin.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name))
			admin.Close(ctx)
		}
	})
	return &Datenbank{Pfoertner: pf, Eigentuemer: eigentuemer, PfoertnerDSN: pfoertnerDSN}
}

func legeVorlageAn(ctx context.Context, basis string) (string, error) {
	name := "boticstudio_vorlage_" + zufall()
	admin, err := pgx.Connect(ctx, basis)
	if err != nil {
		return "", err
	}
	defer admin.Close(ctx)
	// Mehrere Test-Pakete laufen gleichzeitig und ändern dieselbe Rolle.
	// Die Sperre lässt sie nacheinander einrichten.
	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock(4711)"); err != nil {
		return "", err
	}
	defer func() { _, _ = admin.Exec(ctx, "SELECT pg_advisory_unlock(4711)") }()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		return "", err
	}
	u, err := url.Parse(basis)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	pool, err := datenbank.Oeffne(ctx, u.String())
	if err != nil {
		return "", err
	}
	defer pool.Close()
	if _, err := datenbank.Migriere(ctx, pool); err != nil {
		return "", err
	}
	// Im Betrieb setzt das der Betreiber. Für Tests bekommt die Rolle ein festes Passwort.
	if _, err := pool.Exec(ctx, "ALTER ROLE boticstudio_pfoertner LOGIN PASSWORD '"+PfoertnerPasswort+"'"); err != nil {
		return "", err
	}
	return name, nil
}

func mitDatenbank(t testing.TB, basis, name, benutzer, passwort string) string {
	t.Helper()
	u, err := url.Parse(basis)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	if benutzer != "" {
		u.User = url.UserPassword(benutzer, passwort)
	}
	return u.String()
}

func zufall() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
