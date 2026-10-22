package auditor

import (
	"testing"
	"time"
)

func kette(t *testing.T, firma string, arten ...string) []Eintrag {
	t.Helper()
	var alle []Eintrag
	var vorher *Eintrag
	for _, art := range arten {
		e, err := Naechster(vorher, firma, art, map[string]string{"x": art}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		alle = append(alle, e)
		vorher = &alle[len(alle)-1]
	}
	return alle
}

func TestKetteErkenntVeraenderung(t *testing.T) {
	e := kette(t, "a", "eins", "zwei", "drei")
	if err := PruefeKette(e); err != nil {
		t.Fatal(err)
	}

	geaendert := append([]Eintrag(nil), e...)
	geaendert[1].Inhalt = map[string]string{"x": "gefälscht"}
	if PruefeKette(geaendert) == nil {
		t.Fatal("veränderter inhalt nicht erkannt")
	}

	if PruefeKette([]Eintrag{e[0], e[2]}) == nil {
		t.Fatal("fehlender eintrag nicht erkannt")
	}
}

func TestJedeFirmaHatEigeneKette(t *testing.T) {
	a := kette(t, "a", "eins")
	b := kette(t, "b", "eins")
	if a[0].Nr != 1 || b[0].Nr != 1 || b[0].VorherHash != "" {
		t.Fatal("ketten nicht getrennt")
	}
	if _, err := Naechster(&a[0], "b", "zwei", nil, time.Now()); err == nil {
		t.Fatal("eintrag an kette einer anderen firma gehängt")
	}
	if PruefeKette([]Eintrag{a[0], b[0]}) == nil {
		t.Fatal("gemischte kette nicht erkannt")
	}
}

func TestEintragIstKopie(t *testing.T) {
	inhalt := map[string]string{"x": "1"}
	e, err := Naechster(nil, "a", "art", inhalt, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	inhalt["x"] = "2"
	if err := PruefeKette([]Eintrag{e}); err != nil {
		t.Fatal("nachträgliche änderung am aufrufer hat das audit verändert:", err)
	}
}
