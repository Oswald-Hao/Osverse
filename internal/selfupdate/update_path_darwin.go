//go:build darwin

package selfupdate

func updatePathComponents() []string {
	return []string{"Library", "Application Support", "Osverse", "updates"}
}
