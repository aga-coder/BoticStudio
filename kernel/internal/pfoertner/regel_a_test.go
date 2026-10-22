package pfoertner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aga-coder/boticstudio/kernel/internal/katalog"
	. "github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
)

// Regel A, Teil 1: Eine ganz neue Fähigkeit funktioniert, sobald ihr
// Beipackzettel im Katalog-Ordner liegt. Am Pförtner ändert sich nichts.
func TestRegelANeueFaehigkeitOhneCodeAenderung(t *testing.T) {
	ordner := t.TempDir()
	zettel := `{"fassung":1,"name":"termine_lesen","version":"1.0.0","beschreibung":"liest Termine",
		"recht":"kalender.lesen","bereich":"kalender","risiko":"niedrig","wirkt_nach_aussen":false}`
	if err := os.WriteFile(filepath.Join(ordner, "termine_lesen.json"), []byte(zettel), 0o600); err != nil {
		t.Fatal(err)
	}
	k, err := katalog.LadeOrdner(ordner)
	if err != nil {
		t.Fatal(err)
	}
	p := Neu(StandardEinstellungen(), k, NeuerArbeitsspeicher())
	muss(t, p.LegeFirmaAn(ctx, Firma{ID: "a"}))
	muss(t, p.LegePersonAn(ctx, Person{ID: "anna", Firma: "a", Rolle: RolleLeitend}))

	a := Anfrage{Firma: "a", Person: "anna", Aufgabe: "t1", Faehigkeit: "termine_lesen", Version: "1.0.0", Bereich: "kalender/team"}
	erwarte(t, p.Pruefe(ctx, a), Abgelehnt, "kalender.lesen")
	muss(t, p.ErteileErlaubnis(ctx, Erlaubnis{ID: "k", Firma: "a", Recht: "kalender.lesen", Bereich: "kalender/team", Dauer: Dauerhaft, ErteiltVon: "anna"}))
	erwarte(t, p.Pruefe(ctx, a), Erlaubt, "")
}

// Regel A, Teil 2: Im Code des Pförtners steht keine einzelne Fähigkeit.
// Durchsucht wird jede Go-Datei des Kernels außer Tests.
func TestRegelAKeineFaehigkeitImCode(t *testing.T) {
	verboten := []string{"mail_lesen", "mail_einordnen", "besondere_daten_pruefen", "mail.lesen", "mail.einordnen", "imap", "smtp", "openrouter"}
	wurzel := filepath.Join("..", "..")
	err := filepath.WalkDir(wurzel, func(pfad string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(pfad, ".go") || strings.HasSuffix(pfad, "_test.go") {
			return err
		}
		daten, err := os.ReadFile(pfad)
		if err != nil {
			return err
		}
		text := strings.ToLower(string(daten))
		for _, wort := range verboten {
			if strings.Contains(text, wort) {
				t.Errorf("%s enthält %q: der pförtner darf keine einzelne fähigkeit kennen", pfad, wort)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
