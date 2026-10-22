// Package datenbank ist der Speicher des Pförtners in PostgreSQL.
//
// Die Mandantentrennung erzwingt die Datenbank selbst: Jeder Vorgang ist eine
// Transaktion, in der die Firma mit SET LOCAL gesetzt wird. Row-Level Security
// (mit FORCE) lässt nur Zeilen dieser Firma durch, auch wenn eine Abfrage im
// Code einmal keinen Firmenfilter hätte.
package datenbank

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aga-coder/boticstudio/kernel/internal/auditor"
	"github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
	"github.com/aga-coder/boticstudio/kernel/internal/tresor"
)

//go:embed migrationen/*.sql
var migrationen embed.FS

// Oeffne verbindet sich mit der Datenbank. Fehlermeldungen enthalten nie
// das Passwort aus der Verbindungsangabe.
func Oeffne(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// Die Angabe selbst kann das Passwort enthalten, daher kein Detail.
		return nil, errors.New("datenbank: verbindungsangabe ungültig")
	}
	passwort := cfg.ConnConfig.Password
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("datenbank: %s", schwaerze(err.Error(), passwort))
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("datenbank nicht erreichbar: %s", schwaerze(err.Error(), passwort))
	}
	return pool, nil
}

func schwaerze(text, passwort string) string {
	if passwort == "" {
		return text
	}
	return strings.ReplaceAll(text, passwort, tresor.Verdeckt)
}

// Migriere bringt das Schema auf den neuesten Stand. Läuft mit der Rolle,
// der die Tabellen gehören, nie mit der Rolle des Pförtners.
func Migriere(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.boticstudio_migrationen (
		version integer PRIMARY KEY, name text NOT NULL, angewendet_am timestamptz NOT NULL DEFAULT now())`); err != nil {
		return nil, err
	}
	dateien, err := fs.Glob(migrationen, "migrationen/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(dateien)
	var angewendet []string
	for _, datei := range dateien {
		name := strings.TrimPrefix(datei, "migrationen/")
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("migration %s: name muss mit einer nummer beginnen", name)
		}
		sql, err := migrationen.ReadFile(datei)
		if err != nil {
			return nil, err
		}
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			// Sperre, damit zwei Starts nicht gleichzeitig migrieren.
			if _, err := tx.Exec(ctx, `LOCK TABLE public.boticstudio_migrationen IN EXCLUSIVE MODE`); err != nil {
				return err
			}
			var da bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.boticstudio_migrationen WHERE version = $1)`, version).Scan(&da); err != nil {
				return err
			}
			if da {
				return nil
			}
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("migration %s: %w", name, err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO public.boticstudio_migrationen (version, name) VALUES ($1, $2)`, version, name); err != nil {
				return err
			}
			angewendet = append(angewendet, name)
			return nil
		})
		if err != nil {
			return angewendet, err
		}
	}
	return angewendet, nil
}

// Speicher ist der PostgreSQL-Speicher des Pförtners.
type Speicher struct {
	Pool *pgxpool.Pool
	Uhr  func() time.Time
}

// Neu erstellt den Speicher auf einer Verbindung mit der Rolle boticstudio_pfoertner.
func Neu(pool *pgxpool.Pool) *Speicher {
	return &Speicher{Pool: pool, Uhr: time.Now}
}

// Vorgang öffnet eine Transaktion, setzt die Firma und führt fn aus.
func (s *Speicher) Vorgang(ctx context.Context, firma string, fn func(pfoertner.Vorgang) error) error {
	if firma == "" {
		return pfoertner.ErrUnbekannteFirma
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('boticstudio.firma', $1, true)`, firma); err != nil {
			return err
		}
		return fn(&vorgang{ctx: ctx, tx: tx, firma: firma, uhr: s.Uhr})
	})
}

// AuditEintraege liefert die Audit-Kette einer Firma, z. B. für den Wochenbericht.
func (s *Speicher) AuditEintraege(ctx context.Context, firma string) ([]auditor.Eintrag, error) {
	var liste []auditor.Eintrag
	err := s.Vorgang(ctx, firma, func(v pfoertner.Vorgang) error {
		var err error
		liste, err = v.(*vorgang).eintraege()
		return err
	})
	return liste, err
}

type vorgang struct {
	ctx   context.Context
	tx    pgx.Tx
	firma string
	uhr   func() time.Time
}

func (v *vorgang) Firma() (pfoertner.Firma, bool, error) {
	var id string
	err := v.tx.QueryRow(v.ctx, `SELECT id FROM kern.firmen WHERE id = $1`, v.firma).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return pfoertner.Firma{}, false, nil
	}
	if err != nil {
		return pfoertner.Firma{}, false, err
	}
	f := pfoertner.Firma{ID: id}
	zeilen, err := v.tx.Query(v.ctx, `SELECT art, adresse, region, coalesce(einwilligung_von, ''), einwilligung_am
		FROM kern.ziele WHERE firma = $1 ORDER BY art, adresse`, v.firma)
	if err != nil {
		return pfoertner.Firma{}, false, err
	}
	defer zeilen.Close()
	for zeilen.Next() {
		var z pfoertner.FreigegebenesZiel
		var am *time.Time
		if err := zeilen.Scan(&z.Art, &z.Adresse, &z.Region, &z.EinwilligungVon, &am); err != nil {
			return pfoertner.Firma{}, false, err
		}
		if am != nil {
			z.EinwilligungAm = am.UTC()
		}
		f.Ziele = append(f.Ziele, z)
	}
	return f, true, zeilen.Err()
}

func (v *vorgang) LegeFirmaAn(f pfoertner.Firma) error {
	if f.ID != v.firma {
		return errors.New("firma passt nicht zum vorgang")
	}
	if _, err := v.tx.Exec(v.ctx, `INSERT INTO kern.firmen (id) VALUES ($1)`, f.ID); err != nil {
		return err
	}
	for _, z := range f.Ziele {
		var am *time.Time
		if !z.EinwilligungAm.IsZero() {
			am = &z.EinwilligungAm
		}
		var von *string
		if z.EinwilligungVon != "" {
			von = &z.EinwilligungVon
		}
		if _, err := v.tx.Exec(v.ctx, `INSERT INTO kern.ziele (firma, art, adresse, region, einwilligung_von, einwilligung_am)
			VALUES ($1, $2, $3, $4, $5, $6)`, f.ID, z.Art, z.Adresse, z.Region, von, am); err != nil {
			return err
		}
	}
	return nil
}

func (v *vorgang) Person(id string) (pfoertner.Person, bool, error) {
	p := pfoertner.Person{ID: id, Firma: v.firma}
	err := v.tx.QueryRow(v.ctx, `SELECT rolle FROM kern.personen WHERE firma = $1 AND id = $2`, v.firma, id).Scan(&p.Rolle)
	if errors.Is(err, pgx.ErrNoRows) {
		return pfoertner.Person{}, false, nil
	}
	return p, err == nil, err
}

func (v *vorgang) LegePersonAn(p pfoertner.Person) error {
	_, err := v.tx.Exec(v.ctx, `INSERT INTO kern.personen (firma, id, rolle) VALUES ($1, $2, $3)`, p.Firma, p.ID, string(p.Rolle))
	return err
}

const erlaubnisSpalten = `id, recht, bereich, dauer, gueltig_bis, fuer, erteilt_von, erteilt_am, verbraucht, widerrufen`

func (v *vorgang) leseErlaubnisse(sql string, args ...any) ([]pfoertner.Erlaubnis, error) {
	zeilen, err := v.tx.Query(v.ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer zeilen.Close()
	var liste []pfoertner.Erlaubnis
	for zeilen.Next() {
		e := pfoertner.Erlaubnis{Firma: v.firma}
		var bis *time.Time
		var dauer string
		if err := zeilen.Scan(&e.ID, &e.Recht, &e.Bereich, &dauer, &bis, &e.Fuer, &e.ErteiltVon, &e.ErteiltAm,
			&e.Verbraucht, &e.Widerrufen); err != nil {
			return nil, err
		}
		e.Dauer = pfoertner.Dauer(dauer)
		e.ErteiltAm = e.ErteiltAm.UTC()
		if bis != nil {
			e.GueltigBis = bis.UTC()
		}
		if len(e.Fuer) == 0 {
			e.Fuer = nil
		}
		liste = append(liste, e)
	}
	return liste, zeilen.Err()
}

func (v *vorgang) Erlaubnisse(recht string) ([]pfoertner.Erlaubnis, error) {
	return v.leseErlaubnisse(`SELECT `+erlaubnisSpalten+` FROM kern.erlaubnisse
		WHERE firma = $1 AND recht = $2 ORDER BY id FOR UPDATE`, v.firma, recht)
}

func (v *vorgang) Erlaubnis(id string) (pfoertner.Erlaubnis, bool, error) {
	liste, err := v.leseErlaubnisse(`SELECT `+erlaubnisSpalten+` FROM kern.erlaubnisse
		WHERE firma = $1 AND id = $2 FOR UPDATE`, v.firma, id)
	if err != nil || len(liste) == 0 {
		return pfoertner.Erlaubnis{}, false, err
	}
	return liste[0], true, nil
}

func (v *vorgang) SpeichereErlaubnis(e pfoertner.Erlaubnis) error {
	var bis *time.Time
	if !e.GueltigBis.IsZero() {
		bis = &e.GueltigBis
	}
	fuer := e.Fuer
	if fuer == nil {
		fuer = []string{}
	}
	_, err := v.tx.Exec(v.ctx, `INSERT INTO kern.erlaubnisse (firma, `+erlaubnisSpalten+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (firma, id) DO UPDATE SET verbraucht = EXCLUDED.verbraucht, widerrufen = EXCLUDED.widerrufen`,
		e.Firma, e.ID, e.Recht, e.Bereich, string(e.Dauer), bis, fuer, e.ErteiltVon, e.ErteiltAm, e.Verbraucht, e.Widerrufen)
	return err
}

func (v *vorgang) Aufgabe(id string) (pfoertner.Aufgabenstand, error) {
	if _, err := v.tx.Exec(v.ctx, `INSERT INTO kern.aufgaben (firma, id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, v.firma, id); err != nil {
		return pfoertner.Aufgabenstand{}, err
	}
	var a pfoertner.Aufgabenstand
	var versionen []byte
	err := v.tx.QueryRow(v.ctx, `SELECT gestoppt, schritte, versionen FROM kern.aufgaben
		WHERE firma = $1 AND id = $2 FOR UPDATE`, v.firma, id).Scan(&a.Gestoppt, &a.Schritte, &versionen)
	if err != nil {
		return pfoertner.Aufgabenstand{}, err
	}
	if err := json.Unmarshal(versionen, &a.Versionen); err != nil {
		return pfoertner.Aufgabenstand{}, err
	}
	return a, nil
}

func (v *vorgang) SpeichereAufgabe(id string, a pfoertner.Aufgabenstand) error {
	versionen, err := json.Marshal(a.Versionen)
	if err != nil {
		return err
	}
	if a.Versionen == nil {
		versionen = []byte("{}")
	}
	_, err = v.tx.Exec(v.ctx, `INSERT INTO kern.aufgaben (firma, id, gestoppt, schritte, versionen) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (firma, id) DO UPDATE SET gestoppt = EXCLUDED.gestoppt, schritte = EXCLUDED.schritte, versionen = EXCLUDED.versionen`,
		v.firma, id, a.Gestoppt, a.Schritte, versionen)
	return err
}

func (v *vorgang) Vorpruefung(fp string) ([]string, bool, error) {
	var markierungen []string
	var anzahl int
	err := v.tx.QueryRow(v.ctx, `SELECT count(*), coalesce(array_agg(DISTINCT m) FILTER (WHERE m IS NOT NULL), '{}')
		FROM kern.vorpruefungen LEFT JOIN LATERAL unnest(markierungen) AS m ON true
		WHERE firma = $1 AND fingerabdruck = $2`, v.firma, fp).Scan(&anzahl, &markierungen)
	if err != nil {
		return nil, false, err
	}
	return markierungen, anzahl > 0, nil
}

func (v *vorgang) SpeichereVorpruefung(fp, faehigkeit string, markierungen []string) error {
	if markierungen == nil {
		markierungen = []string{}
	}
	_, err := v.tx.Exec(v.ctx, `INSERT INTO kern.vorpruefungen (firma, fingerabdruck, faehigkeit, markierungen, geprueft_am)
		VALUES ($1, $2, $3, $4, $5)`, v.firma, fp, faehigkeit, markierungen, v.uhr())
	return err
}

func (v *vorgang) Schreibe(art string, inhalt map[string]string) (auditor.Eintrag, error) {
	var vorher *auditor.Eintrag
	var nr int
	var hash string
	err := v.tx.QueryRow(v.ctx, `SELECT letzte_nr, letzter_hash FROM audit.ketten WHERE firma = $1 FOR UPDATE`, v.firma).Scan(&nr, &hash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return auditor.Eintrag{}, err
	default:
		vorher = &auditor.Eintrag{Nr: nr, Hash: hash, Firma: v.firma}
	}
	e, err := auditor.Naechster(vorher, v.firma, art, inhalt, v.uhr())
	if err != nil {
		return auditor.Eintrag{}, err
	}
	daten, err := json.Marshal(e.Inhalt)
	if err != nil {
		return auditor.Eintrag{}, err
	}
	if _, err := v.tx.Exec(v.ctx, `INSERT INTO audit.eintraege (firma, nr, zeit, art, inhalt, vorher_hash, hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, e.Firma, e.Nr, e.Zeit, e.Art, daten, e.VorherHash, e.Hash); err != nil {
		return auditor.Eintrag{}, err
	}
	if _, err := v.tx.Exec(v.ctx, `INSERT INTO audit.ketten (firma, letzte_nr, letzter_hash) VALUES ($1, $2, $3)
		ON CONFLICT (firma) DO UPDATE SET letzte_nr = EXCLUDED.letzte_nr, letzter_hash = EXCLUDED.letzter_hash`,
		e.Firma, e.Nr, e.Hash); err != nil {
		return auditor.Eintrag{}, err
	}
	return e, nil
}

func (v *vorgang) eintraege() ([]auditor.Eintrag, error) {
	zeilen, err := v.tx.Query(v.ctx, `SELECT firma, nr, zeit, art, inhalt, vorher_hash, hash
		FROM audit.eintraege WHERE firma = $1 ORDER BY nr`, v.firma)
	if err != nil {
		return nil, err
	}
	defer zeilen.Close()
	var liste []auditor.Eintrag
	for zeilen.Next() {
		var e auditor.Eintrag
		var inhalt []byte
		if err := zeilen.Scan(&e.Firma, &e.Nr, &e.Zeit, &e.Art, &inhalt, &e.VorherHash, &e.Hash); err != nil {
			return nil, err
		}
		e.Zeit = e.Zeit.UTC()
		if err := json.Unmarshal(inhalt, &e.Inhalt); err != nil {
			return nil, err
		}
		liste = append(liste, e)
	}
	return liste, zeilen.Err()
}
