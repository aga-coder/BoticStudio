package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aga-coder/boticstudio/kernel/internal/dienste"
)

func TestLegeUndErsetze(t *testing.T) {
	ordner := t.TempDir()
	datei := filepath.Join(ordner, "dienste.json")
	if err := lege("dashboard", "a,b", datei, filepath.Join(ordner, "d1")); err != nil {
		t.Fatal(err)
	}
	if err := lege("ablauf", "a", datei, filepath.Join(ordner, "a1")); err != nil {
		t.Fatal(err)
	}
	// Neuer Schlüssel für das Dashboard ersetzt den alten.
	if err := lege("dashboard", "a", datei, filepath.Join(ordner, "d2")); err != nil {
		t.Fatal(err)
	}
	// Eine vorhandene Schlüsseldatei wird nie überschrieben.
	if err := lege("ablauf", "a", datei, filepath.Join(ordner, "d2")); err == nil {
		t.Fatal("vorhandene datei überschrieben")
	}
	l, err := dienste.Lade(datei)
	if err != nil {
		t.Fatal(err)
	}
	lies := func(n string) string {
		b, err := os.ReadFile(filepath.Join(ordner, n))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if d, ok := l.Finde(lies("d2")); !ok || d.Name != "dashboard" || d.Darf("b") {
		t.Fatalf("neuer schlüssel: %v %v", d, ok)
	}
	if _, ok := l.Finde(lies("d1")); ok {
		t.Fatal("alter schlüssel gilt noch")
	}
	if d, ok := l.Finde(lies("a1")); !ok || d.Name != "ablauf" {
		t.Fatal("ablauf fehlt")
	}
	if inhalt, _ := os.ReadFile(datei); strings.Contains(string(inhalt), lies("d2")) {
		t.Fatal("schlüssel steht in der dienste-datei")
	}
	if info, _ := os.Stat(filepath.Join(ordner, "d2")); info.Mode().Perm() != 0o600 {
		t.Fatalf("rechte: %v", info.Mode().Perm())
	}
}
