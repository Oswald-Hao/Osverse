//go:build windows

package history

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWindowsHistoryUsesLocalAppDataAndReplacesExistingFile(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "AppData", "Local", "Osverse", "state", "history.json")
	if store.path != want {
		t.Fatalf("history path = %q", store.path)
	}
	for _, input := range []Input{
		{OperationID: "one", ComponentID: "codex-cli", Name: "Codex CLI", Action: "install", Status: "completed", Message: "installed"},
		{OperationID: "two", ComponentID: "claude-cli", Name: "Claude Code", Action: "install", Status: "completed", Message: "installed"},
	} {
		if _, err := store.Append(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.List(context.Background())
	if err != nil || len(entries) != 2 || entries[0].OperationID != "two" {
		t.Fatalf("history entries = (%#v, %v)", entries, err)
	}
}
