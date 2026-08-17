package profiles

import (
	"path/filepath"
	"testing"
)

func resolvedTestHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func adapterInput() Input {
	return Input{
		ID: "profile", Name: "Work", APIKey: "secret-key-1234",
		BaseURL: "https://api.example/v1", Model: "model-name",
	}
}
