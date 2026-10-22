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
		"ohne fassung":         {`{"name":"x","version":"1","beschreibung":"x","recht":"keins"}`, "fassung"},
		"neuere fassung":       {`{"fassung":4,"name":"x","version":"1","beschreibung":"x","recht":"keins"}`, "unbekannt"},
		"fassung null":         {`{"fassung":0,"name":"x","version":"1","beschreibung":"x","recht":"keins"}`, "unbekannt"},
		"unbekanntes feld":     {`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"keins","neu":true}`, "unknown field"},
		"ohne recht":           {`{"fassung":1,"name":"x","version":"1","beschreibung":"x"}`, "recht"},
		"pflichtfelder":        {`{"fassung":1,"recht":"keins"}`, "beschreibung, name, version"},
		"recht ohne bereich":   {`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"x.tun"}`, "bereich"},
		"falsches risiko":      {`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"keins","risiko":"egal"}`, "risikostufe"},
		"zugang in fassung 1":  {`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"x.tun","bereich":"b","zugang":"postfach"}`, "erst ab fassung 2"},
		"zugang ohne recht":    {`{"fassung":2,"name":"x","version":"1","beschreibung":"x","recht":"keins","zugang":"postfach"}`, "braucht auch ein recht"},
		"zugang mit pfad":      {`{"fassung":2,"name":"x","version":"1","beschreibung":"x","recht":"x.tun","bereich":"b","zugang":"a/b"}`, "kein /"},
		"prueft in fassung 2":  {`{"fassung":2,"name":"x","version":"1","beschreibung":"x","recht":"keins","eingaben":["a"],"ausgaben":["markierungen"],"prueft":["a"]}`, "erst ab fassung 3"},
		"prüfer gibt aus":      {`{"fassung":3,"name":"x","version":"1","beschreibung":"x","recht":"keins","eingaben":["a"],"ausgaben":["markierungen"],"prueft":["a"],"datenausgang":["ki_modell"]}`, "keine daten hinausgeben"},
		"prüfer ohne ergebnis": {`{"fassung":3,"name":"x","version":"1","beschreibung":"x","recht":"keins","eingaben":["a"],"prueft":["a"]}`, "markierungen"},
		"prüft unbekanntes":    {`{"fassung":3,"name":"x","version":"1","beschreibung":"x","recht":"keins","eingaben":["a"],"ausgaben":["markierungen"],"prueft":["b"]}`, "steht nicht bei den eingaben"},
		"vorprüfung unbekannt": {`{"fassung":3,"name":"x","version":"1","beschreibung":"x","recht":"keins","eingaben":["a"],"braucht_vorpruefung":["b"]}`, "steht nicht bei den eingaben"},
		"at im namen":          {`{"fassung":1,"name":"x@y","version":"1","beschreibung":"x","recht":"keins"}`, "@"},
	} {
		_, err := Lies([]byte(fall.json))
		if err == nil || !strings.Contains(err.Error(), fall.fehler) {
			t.Errorf("%s: fehler %v, erwartet etwas mit %q", name, err, fall.fehler)
		}
	}
}

// Regel B: Ein Zettel der Fassung 1 bleibt gültig, auch neben Fassung 2,
// und bekommt ohne "zugang" keine Zugangsdaten.
func TestFassung1BleibtGueltig(t *testing.T) {
	alt, err := Lies([]byte(`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"x.tun","bereich":"b","risiko":"niedrig","wirkt_nach_aussen":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if alt.Zugang != "" {
		t.Fatalf("fassung 1 bekommt zugang %q", alt.Zugang)
	}
	neu, err := Lies([]byte(`{"fassung":2,"name":"x","version":"2","beschreibung":"x","recht":"x.tun","bereich":"b","zugang":"postfach"}`))
	if err != nil {
		t.Fatal(err)
	}
	if neu.Zugang != "postfach" {
		t.Fatalf("zugang nicht gelesen: %+v", neu)
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

// Regel B: Fehlt "braucht_vorpruefung" bei einer Fähigkeit mit Datenausgang,
// muss alles vorgeprüft sein, was hinausgehen kann. Das gilt auch für Fassung 1.
func TestVorpruefungStrengsterWert(t *testing.T) {
	alt, err := Lies([]byte(`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"keins","eingaben":["a","b"],"datenausgang":["ki_modell"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(alt.BrauchtVorpruefung, ",") != "a,b" {
		t.Fatalf("nicht alles vorzuprüfen: %v", alt.BrauchtVorpruefung)
	}
	ohneAusgang, _ := Lies([]byte(`{"fassung":1,"name":"x","version":"1","beschreibung":"x","recht":"keins","eingaben":["a"]}`))
	if len(ohneAusgang.BrauchtVorpruefung) != 0 {
		t.Fatalf("ohne datenausgang braucht nichts vorprüfung: %v", ohneAusgang.BrauchtVorpruefung)
	}
	neu, err := Lies([]byte(`{"fassung":3,"name":"x","version":"1","beschreibung":"x","recht":"keins","eingaben":["a","b"],"datenausgang":["ki_modell"],"braucht_vorpruefung":["a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(neu.BrauchtVorpruefung, ",") != "a" {
		t.Fatalf("falsch gelesen: %v", neu.BrauchtVorpruefung)
	}
	pruefer, err := Lies([]byte(`{"fassung":3,"name":"p","version":"1","beschreibung":"p","recht":"keins","eingaben":["a"],"ausgaben":["markierungen"],"prueft":["a"]}`))
	if err != nil || strings.Join(pruefer.Prueft, ",") != "a" {
		t.Fatalf("%v %+v", err, pruefer)
	}
}
