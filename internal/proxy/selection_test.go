package proxy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSelectionStorePersistsOverwritesAndClearsValidatedPreference(t *testing.T) {
	store, err := NewSelectionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if selection, err := store.Load(); err != nil || selection != (Selection{}) {
		t.Fatalf("initial Load() = (%#v, %v)", selection, err)
	}
	first := Selection{Protocol: ProtocolSOCKS5, Port: 7897}
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	if selection, err := store.Load(); err != nil || selection != first {
		t.Fatalf("Load() = (%#v, %v), want %#v", selection, err, first)
	}
	second := Selection{Protocol: ProtocolHTTPSConnect, Port: 2080}
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	if selection, err := store.Load(); err != nil || selection != second {
		t.Fatalf("overwritten Load() = (%#v, %v), want %#v", selection, err, second)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("selection mode = %v", info.Mode().Perm())
		}
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if selection, err := store.Load(); err != nil || selection != (Selection{}) {
		t.Fatalf("cleared Load() = (%#v, %v)", selection, err)
	}
}

func TestSelectionStoreRejectsInvalidOrExpandedDocuments(t *testing.T) {
	store, err := NewSelectionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ensureDirectory(true); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"protocol":"http","port":0}`,
		`{"protocol":"ftp","port":7890}`,
		`{"protocol":"http","port":7890,"url":"https://example.test"}`,
		`{"protocol":"http","port":7890} {}`,
	} {
		if err := os.WriteFile(store.path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatalf("Load() accepted %q", raw)
		}
	}
	if err := store.Save(Selection{Protocol: Protocol("ftp"), Port: 7890}); err == nil {
		t.Fatal("Save() accepted an unsupported protocol")
	}
}

func TestSelectionStoreRejectsSymlinkedPreference(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ordinary Windows users cannot create symlinks")
	}
	store, err := NewSelectionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ensureDirectory(true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte(`{"protocol":"http","port":7890}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, store.path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load() followed a symlink")
	}
	if err := store.Clear(); err == nil {
		t.Fatal("Clear() followed a symlink")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("outside target changed: %v", err)
	}
}
