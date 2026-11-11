//go:build windows

package main

import (
	"encoding/csv"
	"fmt"
	"os/exec"
	"strings"
)

// machePrivat beschränkt die Datei auf den eigenen Windows-Benutzer.
// Der Modus 0600 wirkt unter Windows nicht, dort zählt die Zugriffsliste.
func machePrivat(pfad string) error {
	aus, err := exec.Command("whoami", "/user", "/fo", "csv", "/nh").Output()
	if err != nil {
		return fmt.Errorf("eigener Windows-Benutzer nicht ermittelbar: %w", err)
	}
	felder, err := csv.NewReader(strings.NewReader(strings.TrimSpace(string(aus)))).Read()
	if err != nil || len(felder) < 2 || !strings.HasPrefix(felder[1], "S-1-") {
		return fmt.Errorf("eigener Windows-Benutzer nicht ermittelbar")
	}
	if err := exec.Command("icacls", pfad, "/inheritance:r", "/grant:r", "*"+felder[1]+":F").Run(); err != nil {
		return fmt.Errorf("zugriffsrechte nicht setzbar (icacls): %w", err)
	}
	return nil
}
