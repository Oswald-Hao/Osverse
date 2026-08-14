//go:build !windows && !darwin

package selfupdate

func updatePathComponents() []string { return []string{".local", "share", "osverse", "updates"} }
