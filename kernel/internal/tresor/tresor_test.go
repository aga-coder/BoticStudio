package tresor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const geheim = "sehr-geheimes-passwort"

func TestGeheimnisWirdNieAusgegeben(t *testing.T) {
	g := NeuGeheimnis(geheim)
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("test", "zugang", g)
	j, _ := json.Marshal(struct{ Zugang Geheimnis }{g})
	for name, text := range map[string]string{
		"%v": fmt.Sprintf("%v", g), "%s": fmt.Sprintf("%s", g), "%+v": fmt.Sprintf("%+v", g),
		"%#v": fmt.Sprintf("%#v", g), "json": string(j), "log": log.String(),
	} {
		if strings.Contains(text, geheim) {
			t.Errorf("%s gibt das geheimnis aus: %s", name, text)
		}
	}
	if g.Offenlegen() != geheim {
		t.Fatal("offenlegen liefert falschen wert")
	}
}

func TestTresorTrenntFirmen(t *testing.T) {
	s := NeuSpeicher()
	s.Lege("a", "postfach", NeuGeheimnis(geheim))
	if _, err := s.Hole("b", "postfach"); err != ErrNichtGefunden {
		t.Fatal("firma b bekam den zugang von firma a")
	}
	g, err := s.Hole("a", "postfach")
	if err != nil || g.Offenlegen() != geheim {
		t.Fatal("eigener zugang nicht gefunden")
	}
}
