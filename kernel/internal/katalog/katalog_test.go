package katalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionWirdNichtUeberschrieben(t *testing.T) {
	ordner := t.TempDir()
	zettel := `{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"keins"}`
	for _, datei := range []string{"a.json", "b.json"} {
		if err := os.WriteFile(filepath.Join(ordner, datei), []byte(zettel), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LadeOrdner(ordner); err == nil {
		t.Fatal("doppelte version angenommen")
	}
}

func TestFehlerhafterZettelStopptDasLaden(t *testing.T) {
	ordner := t.TempDir()
	if err := os.WriteFile(filepath.Join(ordner, "kaputt.json"), []byte(`{"fassung":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LadeOrdner(ordner); err == nil {
		t.Fatal("fehlerhafter zettel angenommen")
	}
}
