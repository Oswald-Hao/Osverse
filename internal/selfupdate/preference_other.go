//go:build !linux

package selfupdate

func linuxArtifactPreference() (string, string, bool) { return "", "", false }
