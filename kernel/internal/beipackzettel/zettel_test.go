package beipackzettel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVollstaendigerZettel(t *testing.T) {
	z, err := Lies([]byte(`{"fassung":1,"name":"x","version":"1","beschreibung":"tut x","recht":"x.tun","bereich":"postfach",
		"risiko":"niedrig","wirkt_nach_aussen":false,"datenausgang":["ki_modell"],"eingaben":["a"],"ausgaben":["b"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if z.Risiko != RisikoNiedrig || z.WirktNachAussen || !z.DarfSenden("ki_modell") || z.DarfSenden("web") || z.Schluessel() != "x@1" {
		t.Fatalf("falsch gelesen: %+v", z)
	}
}

// Regel B: Fehlt ein Feld, gilt der strengste Wert.
func TestFehlendeFelderSindStreng(t *testing.T) {
	z, err := Lies([]byte(`{"fassung":1,"name":"alt","version":"1","beschreibung":"alt","recht":"keins"}`))
	if err != nil {
		t.Fatal(err)
	}
	if z.Risiko != RisikoHoch || !z.WirktNachAussen || len(z.Datenausgang) != 0 {
		t.Fatalf("nicht der strengste wert: %+v", z)
	}
}

func TestUngueltigeZettel(t *testing.T) {
	for name, fall := range map[string]struct{ json, fehler string }{
		"ohne fassung":       {`{"name":"x","version":"1","beschreibung":"x","recht":"keins"}`, "fassung"},
		"neuere fassung":     {`{"fassung":2,"name":"x","version":"1","beschreibung":"x","recht":"keins"}`, "unbekannt"},
		"fassung null":       {`{"fassung":0,"name":"x","version":"1","beschreibung":"x","recht":"keins"}`, "unbekannt"},
		"unbekanntes feld":   {`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"keins","neu":true}`, "unknown field"},
		"ohne recht":         {`{"fassung":1,"name":"x","version":"1","beschreibung":"x"}`, "recht"},
		"pflichtfelder":      {`{"fassung":1,"recht":"keins"}`, "beschreibung, name, version"},
		"recht ohne bereich": {`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"x.tun"}`, "bereich"},
		"falsches risiko":    {`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"keins","risiko":"egal"}`, "risikostufe"},
		"at im namen":        {`{"fassung":1,"name":"x@y","version":"1","beschreibung":"x","recht":"keins"}`, "@"},
	} {
		_, err := Lies([]byte(fall.json))
		if err == nil || !strings.Contains(err.Error(), fall.fehler) {
			t.Errorf("%s: fehler %v, erwartet etwas mit %q", name, err, fall.fehler)
		}
	}
}

// Die echten Beipackzettel im Repo müssen gültig sein.
func TestKatalogImRepo(t *testing.T) {
	pfade, err := filepath.Glob(filepath.Join("..", "..", "..", "katalog", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pfade) < 3 {
		t.Fatalf("nur %d beipackzettel gefunden", len(pfade))
	}
	for _, pfad := range pfade {
		daten, err := os.ReadFile(pfad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Lies(daten); err != nil {
			t.Errorf("%s: %v", pfad, err)
		}
	}
}
