//go:build windows

package history

func historyStateComponents() []string { return []string{"AppData", "Local", "Osverse", "state"} }
