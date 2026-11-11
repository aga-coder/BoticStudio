//go:build !windows

package main

// machePrivat: außerhalb von Windows reicht der Modus 0600 beim Anlegen.
func machePrivat(string) error { return nil }
