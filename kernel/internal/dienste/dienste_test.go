package dienste

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func hashVon(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestFindeUndFirmen(t *testing.T) {
	s, h, err := NeuerSchluessel()
	if err != nil {
		t.Fatal(err)
	}
	if hashVon(s) != h || len(s) < MinLaenge {
		t.Fatal("schlüssel und hash passen nicht")
	}
	l, err := Neu(Datei{Dienste: []Eintrag{{Name: "dashboard", SchluesselSHA256: h, Firmen: []string{"a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := l.Finde(s)
	if !ok || d.Name != "dashboard" {
		t.Fatalf("nicht gefunden: %v %v", d, ok)
	}
	if !d.Darf("a") || d.Darf("b") || d.Darf("") {
		t.Fatal("firmen falsch geprüft")
	}
	for _, falsch := range []string{"", "kurz", s + "x", strings.ToUpper(s), h} {
		if _, ok := l.Finde(falsch); ok {
			t.Fatalf("falscher schlüssel angenommen: %q", falsch)
		}
	}
}

func TestFehlerhafteEintraegeVerhindernDenStart(t *testing.T) {
	_, h, _ := NeuerSchluessel()
	_, h2, _ := NeuerSchluessel()
	faelle := map[string][]Eintrag{
		"leer":         nil,
		"name":         {{Name: "Dash Board", SchluesselSHA256: h, Firmen: []string{"a"}}},
		"hash":         {{Name: "d", SchluesselSHA256: "abc", Firmen: []string{"a"}}},
		"ohne firmen":  {{Name: "d", SchluesselSHA256: h}},
		"leere firma":  {{Name: "d", SchluesselSHA256: h, Firmen: []string{""}}},
		"doppelt":      {{Name: "d", SchluesselSHA256: h, Firmen: []string{"a"}}, {Name: "d", SchluesselSHA256: h2, Firmen: []string{"a"}}},
		"gleicher key": {{Name: "d", SchluesselSHA256: h, Firmen: []string{"a"}}, {Name: "e", SchluesselSHA256: h, Firmen: []string{"a"}}},
	}
	for name, e := range faelle {
		if _, err := Neu(Datei{Dienste: e}); err == nil {
			t.Errorf("%s: hätte abgelehnt werden müssen", name)
		}
	}
}
