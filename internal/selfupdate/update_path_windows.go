//go:build windows

package selfupdate

func updatePathComponents() []string { return []string{"AppData", "Local", "Osverse", "updates"} }
