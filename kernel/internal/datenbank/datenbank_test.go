package datenbank_test

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
	"github.com/aga-coder/boticstudio/kernel/internal/beipackzettel"
	"github.com/aga-coder/boticstudio/kernel/internal/datenbank"
	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
	"github.com/aga-coder/boticstudio/kernel/internal/testdb"
)

var ctx = context.Background()

// befuelle legt zwei Firmen mit je einer Person, Erlaubnis und Aufgabe an.
func befuelle(t *testing.T, db *testdb.Datenbank) *pfoertner.Pfoertner {
	t.Helper()
	k := katalog.Neu()
	z, err := beipackzettel.Lies([]byte(`{"fassung":1,"name":"lesen","version":"1","beschreibung":"liest","recht":"lesen","bereich":"postfach","risiko":"niedrig","wirkt_nach_aussen":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Fuege(z); err != nil {
		t.Fatal(err)
	}
	p := pfoertner.Neu(pfoertner.StandardEinstellungen(), k, datenbank.Neu(db.Pfoertner))
	for _, f := range []string{"a", "b"} {
		for _, err := range []error{
			p.LegeFirmaAn(ctx, pfoertner.Firma{ID: f}),
			p.LegePersonAn(ctx, pfoertner.Person{ID: "chef-" + f, Firma: f, Rolle: pfoertner.RolleAdmin}),
			p.ErteileErlaubnis(ctx, pfoertner.Erlaubnis{ID: "e-" + f, Firma: f, Recht: "lesen", Bereich: "postfach/" + f, Dauer: pfoertner.Dauerhaft, ErteiltVon: "chef-" + f}),
		} {
			if err != nil {
				t.Fatal(err)
			}
		}
		ent := p.Pruefe(ctx, pfoertner.Anfrage{Firma: f, Person: "chef-" + f, Aufgabe: "t1", Faehigkeit: "lesen", Version: "1", Bereich: "postfach/" + f})
		if ent.Ergebnis != pfoertner.Erlaubt {
			t.Fatalf("%s: %+v", f, ent)
		}
	}
	return p
}

// alsFirma führt fn in einer Transaktion aus, in der nur die Firma gesetzt ist.
// firma "" heißt: keine Firma gesetzt.
func alsFirma(t *testing.T, pool *pgxpool.Pool, firma string, fn func(tx pgx.Tx)) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if firma != "" {
		if _, err := tx.Exec(ctx, `SELECT set_config('boticstudio.firma', $1, true)`, firma); err != nil {
			t.Fatal(err)
		}
	}
	fn(tx)
}

func zaehle(t *testing.T, tx pgx.Tx, tabelle string) int {
	t.Helper()
	var n int
	// Absichtlich ohne Firmenfilter: Die Datenbank selbst muss trennen.
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+tabelle).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var tabellen = []string{"kern.firmen", "kern.personen", "kern.erlaubnisse", "kern.aufgaben", "kern.vorpruefungen", "audit.ketten", "audit.eintraege"}

// Eine Abfrage ganz ohne Firmenfilter sieht nur die eigene Firma, ohne
// gesetzte Firma gar nichts. Das gilt für den Pförtner und, wegen FORCE,
// auch für den Eigentümer der Tabellen.
func TestAbfrageOhneFilterSiehtNurEigeneFirma(t *testing.T) {
	db := testdb.Neu(t)
	befuelle(t, db)
	for name, pool := range map[string]*pgxpool.Pool{"pfoertner": db.Pfoertner, "eigentuemer": db.Eigentuemer} {
		alsFirma(t, pool, "", func(tx pgx.Tx) {
			for _, tab := range tabellen {
				if n := zaehle(t, tx, tab); n != 0 {
					t.Errorf("%s ohne firma sieht %d zeilen in %s", name, n, tab)
				}
			}
		})
		alsFirma(t, pool, "a", func(tx pgx.Tx) {
			for _, tab := range []string{"kern.firmen", "kern.personen", "kern.erlaubnisse", "kern.aufgaben", "audit.ketten"} {
				if n := zaehle(t, tx, tab); n != 1 {
					t.Errorf("%s als a sieht %d zeilen in %s, erwartet 1", name, n, tab)
				}
			}
			var fremde int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit.eintraege WHERE firma <> 'a'`).Scan(&fremde); err != nil {
				t.Fatal(err)
			}
			if fremde != 0 {
				t.Errorf("%s als a sieht %d audit-einträge anderer firmen", name, fremde)
			}
		})
	}
}

func TestSchreibenInFremdeFirmaScheitert(t *testing.T) {
	db := testdb.Neu(t)
	befuelle(t, db)

	// Einfügen für Firma b, während Firma a gesetzt ist.
	for _, sql := range []string{
		`INSERT INTO kern.personen (firma, id, rolle) VALUES ('b', 'eindringling', 'admin')`,
		`INSERT INTO kern.aufgaben (firma, id) VALUES ('b', 'x')`,
		`INSERT INTO audit.eintraege (firma, nr, zeit, art, inhalt, vorher_hash, hash) VALUES ('b', 99, now(), 'x', '{}', '', '')`,
	} {
		alsFirma(t, db.Pfoertner, "a", func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "row-level security") {
				t.Errorf("%s: fehler %v, erwartet row-level security", sql, err)
			}
		})
	}

	// Ändern von Zeilen der Firma b, während Firma a gesetzt ist: trifft nichts.
	alsFirma(t, db.Pfoertner, "a", func(tx pgx.Tx) {
		tag, err := tx.Exec(ctx, `UPDATE kern.erlaubnisse SET widerrufen = true WHERE firma = 'b'`)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("update fremder erlaubnis: %v, %d zeilen", err, tag.RowsAffected())
		}
	})

	// Eine Zeile von a auf b umschreiben geht nicht.
	alsFirma(t, db.Pfoertner, "a", func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `UPDATE kern.aufgaben SET firma = 'b'`); err == nil {
			t.Error("zeile in fremde firma verschoben")
		}
	})
}

func TestPfoertnerDarfNichtLoeschenUndAuditNichtAendern(t *testing.T) {
	db := testdb.Neu(t)
	befuelle(t, db)
	for _, sql := range []string{
		`DELETE FROM kern.erlaubnisse`,
		`DELETE FROM kern.personen`,
		`DELETE FROM audit.eintraege`,
		`UPDATE audit.eintraege SET art = 'gefälscht'`,
		`TRUNCATE audit.eintraege`,
		`ALTER TABLE kern.personen DISABLE ROW LEVEL SECURITY`,
		`SET ROLE boticstudio_eigentuemer`,
	} {
		alsFirma(t, db.Pfoertner, "a", func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, sql); err == nil {
				t.Errorf("pförtner durfte: %s", sql)
			}
		})
	}
	// Auch der Eigentümer kann das Audit nicht ändern oder löschen.
	for _, sql := range []string{`UPDATE audit.eintraege SET art = 'gefälscht'`, `DELETE FROM audit.eintraege`, `TRUNCATE audit.eintraege`} {
		alsFirma(t, db.Eigentuemer, "a", func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "nur zum anhängen") {
				t.Errorf("eigentümer: %s: %v", sql, err)
			}
		})
	}
}

// Wächter für die Zukunft: Jede Tabelle in kern, audit, arbeit und konto muss RLS mit FORCE
// und eine Richtlinie haben. Eine neue Tabelle ohne Trennung fällt hier auf.
func TestJedeTabelleHatErzwungeneTrennung(t *testing.T) {
	db := testdb.Neu(t)
	zeilen, err := db.Eigentuemer.Query(ctx, `
		SELECT n.nspname || '.' || c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid)
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN ('kern', 'audit', 'arbeit', 'konto') AND c.relkind IN ('r', 'p')`)
	if err != nil {
		t.Fatal(err)
	}
	defer zeilen.Close()
	anzahl := 0
	for zeilen.Next() {
		var name string
		var rls, force, richtlinie bool
		if err := zeilen.Scan(&name, &rls, &force, &richtlinie); err != nil {
			t.Fatal(err)
		}
		anzahl++
		if !rls || !force || !richtlinie {
			t.Errorf("%s: rls=%v force=%v richtlinie=%v", name, rls, force, richtlinie)
		}
	}
	if anzahl < len(tabellen)+1 {
		t.Fatalf("nur %d tabellen gefunden", anzahl)
	}

	var super, umgehen bool
	if err := db.Eigentuemer.QueryRow(ctx, `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'boticstudio_pfoertner'`).Scan(&super, &umgehen); err != nil {
		t.Fatal(err)
	}
	if super || umgehen {
		t.Fatalf("pförtner-rolle: superuser=%v bypassrls=%v", super, umgehen)
	}
}

// Zehn gleichzeitige Prüfungen auf eine einmalige Erlaubnis: genau eine geht durch.
func TestEinmaligeErlaubnisAuchBeiGleichzeitigenZugriffen(t *testing.T) {
	db := testdb.Neu(t)
	p := befuelle(t, db)
	if err := p.ErteileErlaubnis(ctx, pfoertner.Erlaubnis{ID: "einmal", Firma: "a", Recht: "lesen", Bereich: "postfach/einmal", Dauer: pfoertner.Einmalig, ErteiltVon: "chef-a"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ergebnisse := make(chan pfoertner.Ergebnis, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ergebnisse <- p.Pruefe(ctx, pfoertner.Anfrage{Firma: "a", Person: "chef-a", Aufgabe: "parallel-" + string(rune('a'+i)),
				Faehigkeit: "lesen", Version: "1", Bereich: "postfach/einmal"}).Ergebnis
		}(i)
	}
	wg.Wait()
	close(ergebnisse)
	erlaubt := 0
	for e := range ergebnisse {
		if e == pfoertner.Erlaubt {
			erlaubt++
		}
	}
	if erlaubt != 1 {
		t.Fatalf("%d mal erlaubt, erwartet genau 1", erlaubt)
	}

	eintraege, err := datenbank.Neu(db.Pfoertner).AuditEintraege(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := auditor.PruefeKette(eintraege); err != nil {
		t.Fatal(err)
	}
}

func TestPasswortNieInFehlermeldungen(t *testing.T) {
	db := testdb.Neu(t)
	u, err := url.Parse(db.PfoertnerDSN)
	if err != nil {
		t.Fatal(err)
	}
	const falsch = "falsches-passwort-xyz-123"
	u.User = url.UserPassword("boticstudio_pfoertner", falsch)
	for _, dsn := range []string{
		u.String(),
		"postgres://boticstudio_pfoertner:" + falsch + "@127.0.0.1:1/x?connect_timeout=1",
		"postgres://boticstudio_pfoertner:" + falsch + "@[kaputt",
		"host=localhost password=" + falsch + " port=keine-zahl",
	} {
		_, err := datenbank.Oeffne(ctx, dsn)
		if err == nil {
			t.Fatalf("verbindung mit falschem passwort geklappt")
		}
		if strings.Contains(err.Error(), falsch) {
			t.Errorf("passwort in fehlermeldung: %v", err)
		}
	}
}

// Der Ablauf schreibt Ergebnisse nur für die eigene Firma, sieht nichts von
// anderen Firmen und kommt weder an Erlaubnisse noch an das Audit.
func TestAblaufRolleNurArbeitsdatenDerEigenenFirma(t *testing.T) {
	db := testdb.Neu(t)
	befuelle(t, db)
	ablauf, err := datenbank.Oeffne(ctx, db.AblaufDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer ablauf.Close()

	einfuegen := `INSERT INTO arbeit.einordnungen (firma, bereich, uid, stand, kategorie, aufgabe)
		VALUES ($1, 'postfach/x', '1', 'eingeordnet', 'Anfrage', 't')`
	for _, f := range []string{"a", "b"} {
		alsFirma(t, ablauf, f, func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, einfuegen, f); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
	alsFirma(t, ablauf, "", func(tx pgx.Tx) {
		if n := zaehle(t, tx, "arbeit.einordnungen"); n != 0 {
			t.Errorf("ohne firma %d zeilen sichtbar", n)
		}
	})
	alsFirma(t, ablauf, "a", func(tx pgx.Tx) {
		if n := zaehle(t, tx, "arbeit.einordnungen"); n != 1 {
			t.Errorf("als a %d zeilen sichtbar, erwartet 1", n)
		}
		if _, err := tx.Exec(ctx, einfuegen, "b"); err == nil || !strings.Contains(err.Error(), "row-level security") {
			t.Errorf("schreiben für b als a: %v", err)
		}
	})
	for _, sql := range []string{
		`SELECT count(*) FROM kern.erlaubnisse`,
		`SELECT count(*) FROM audit.eintraege`,
		`SELECT count(*) FROM kern.vorpruefungen`,
		`DELETE FROM arbeit.einordnungen`,
	} {
		alsFirma(t, ablauf, "a", func(tx pgx.Tx) {
			if _, err := tx.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "permission denied") {
				t.Errorf("%s: fehler %v, erwartet permission denied", sql, err)
			}
		})
	}
}
