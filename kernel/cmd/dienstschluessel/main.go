// Befehl dienstschluessel legt für einen Dienst einen neuen Schlüssel an.
//
//	go run ./cmd/dienstschluessel -name dashboard -firmen testfirma-echt \
//	  -dienste /ausserhalb/des/repos/dienste.json -ausgabe /ausserhalb/des/repos/dashboard.schluessel
//
// Der Schlüssel landet nur in der Datei -ausgabe (nur für den Besitzer
// lesbar, eine vorhandene Datei wird nie überschrieben). In die Datei
// -dienste kommt nur sein SHA-256. Gibt es den Dienst dort schon, ersetzt der
// neue Schlüssel den alten. Danach den Pförtner neu starten. Der Schlüssel
// wird nie ausgegeben.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/aga-coder/boticstudio/kernel/internal/dienste"
)

func main() {
	name := flag.String("name", "", "Name des Dienstes, z. B. dashboard oder ablauf")
	firmen := flag.String("firmen", "", "Firmen, für die der Dienst anfragen darf, mit Komma getrennt")
	dienstdatei := flag.String("dienste", "", "Datei mit den Diensten (wird angelegt, wenn es sie nicht gibt)")
	ausgabe := flag.String("ausgabe", "", "neue Datei für den Schlüssel")
	flag.Parse()
	if err := lege(*name, *firmen, *dienstdatei, *ausgabe); err != nil {
		fmt.Fprintln(os.Stderr, "dienstschluessel:", err)
		os.Exit(1)
	}
	fmt.Printf("Schlüssel für %q gespeichert in %s. Pförtner neu starten.\n", *name, *ausgabe)
}

func lege(name, firmenListe, dienstdatei, ausgabe string) error {
	if name == "" || firmenListe == "" || dienstdatei == "" || ausgabe == "" {
		return errors.New("-name, -firmen, -dienste und -ausgabe sind nötig")
	}
	var d dienste.Datei
	switch daten, err := os.ReadFile(dienstdatei); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(daten, &d); err != nil {
			return fmt.Errorf("%s nicht lesbar: %w", dienstdatei, err)
		}
	}
	schluessel, hash, err := dienste.NeuerSchluessel()
	if err != nil {
		return err
	}
	eintrag := dienste.Eintrag{Name: name, SchluesselSHA256: hash, Firmen: strings.Split(firmenListe, ",")}
	neu := []dienste.Eintrag{eintrag}
	for _, e := range d.Dienste {
		if e.Name != name {
			neu = append(neu, e)
		}
	}
	d.Dienste = neu
	// Erst prüfen, was der Pförtner beim Start prüfen wird.
	if _, err := dienste.Neu(d); err != nil {
		return err
	}
	f, err := os.OpenFile(ausgabe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("schlüsseldatei: %w", err)
	}
	if _, err := f.WriteString(schluessel); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	inhalt, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dienstdatei, append(inhalt, '\n'), 0o600)
}
