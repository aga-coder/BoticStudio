package pfoertner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	. "github.com/aga-coder/boticstudio/kernel/internal/pfoertner"
)

// Das Dashboard rechnet den Fingerabdruck eines Textes selbst nach, um zu
// prüfen, dass es dem Menschen genau das zeigt, was der Pförtner freigibt.
// Diese Datei hält fest, was dabei herauskommen muss. Der Test im Ordner
// orchestrator prüft gegen dieselbe Datei.
func TestFingerabdruckBeispiele(t *testing.T) {
	pfad := filepath.Join("..", "..", "..", "testdaten", "fingerabdruecke.json")
	roh, err := os.ReadFile(pfad)
	muss(t, err)
	var datei struct {
		Hinweis   string `json:"hinweis"`
		Beispiele []struct {
			Text          string `json:"text"`
			Fingerabdruck string `json:"fingerabdruck"`
		} `json:"beispiele"`
	}
	muss(t, json.Unmarshal(roh, &datei))
	if len(datei.Beispiele) < 5 {
		t.Fatal("zu wenige beispiele")
	}
	for _, b := range datei.Beispiele {
		// So kommt eine Eingabe an: als JSON gelesen, dann als beliebiger Wert weitergegeben.
		var wert any
		alsJSON, err := json.Marshal(b.Text)
		muss(t, err)
		muss(t, json.Unmarshal(alsJSON, &wert))
		fp, err := Fingerabdruck(wert)
		muss(t, err)
		if fp != b.Fingerabdruck {
			t.Errorf("%q: %s, in der datei steht %q", b.Text, fp, b.Fingerabdruck)
		}
	}
}
