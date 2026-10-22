package auditor

import "testing"

func TestKetteErkenntVeraenderung(t *testing.T) {
	s := NeuSpeicher()
	for _, art := range []string{"eins", "zwei", "drei"} {
		if _, err := s.Schreibe("a", art, map[string]string{"x": art}); err != nil {
			t.Fatal(err)
		}
	}
	e := s.Eintraege()
	if err := PruefeKette(e); err != nil {
		t.Fatal(err)
	}

	geaendert := append([]Eintrag(nil), e...)
	geaendert[1].Inhalt = map[string]string{"x": "gefälscht"}
	if PruefeKette(geaendert) == nil {
		t.Fatal("veränderter inhalt nicht erkannt")
	}

	luecke := []Eintrag{e[0], e[2]}
	if PruefeKette(luecke) == nil {
		t.Fatal("fehlender eintrag nicht erkannt")
	}
}

func TestEintragIstKopie(t *testing.T) {
	s := NeuSpeicher()
	inhalt := map[string]string{"x": "1"}
	if _, err := s.Schreibe("a", "art", inhalt); err != nil {
		t.Fatal(err)
	}
	inhalt["x"] = "2"
	if err := PruefeKette(s.Eintraege()); err != nil {
		t.Fatal("nachträgliche änderung am aufrufer hat das audit verändert:", err)
	}
}
