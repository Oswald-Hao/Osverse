package geminiinstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitActivationFailureRemovesNewPayload(t *testing.T) {
	home := t.TempDir()
	paths := managedPathsFor(home, "linux", geminiVersion)
	if err := os.MkdirAll(paths.toolRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	payload, marker := testPayload(t, filepath.Join(home, "payload"), "linux", "valid")
	want := errors.New("activate")
	err := commitAndActivate(home, payload, paths, marker, "linux", func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("commitAndActivate error = %v", err)
	}
	if _, err := os.Lstat(paths.finalRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new payload remained after rollback: %v", err)
	}
}

func TestCommitRepairsDamagedOwnedPayloadBeforeActivation(t *testing.T) {
	home := t.TempDir()
	paths := managedPathsFor(home, "linux", geminiVersion)
	if err := os.MkdirAll(paths.toolRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	payload, marker := testPayload(t, filepath.Join(home, "payload"), "linux", "valid")
	_, _ = testPayload(t, paths.finalRoot, "linux", "damaged")
	if err := commitAndActivate(home, payload, paths, marker, "linux", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(paths.finalRoot, "app", "package", "bundle", "gemini.js"))
	if err != nil || string(raw) != "valid" {
		t.Fatalf("repaired script = %q, %v", raw, err)
	}
	matches, err := filepath.Glob(filepath.Join(paths.root, "recovery", "install-gemini-cli-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("quarantine matches = %#v, %v", matches, err)
	}
}

func testPayload(t *testing.T, root, goos, script string) (string, []byte) {
	t.Helper()
	files := map[string]string{
		"app/package/package.json":     "manifest",
		"app/package/bundle/gemini.js": script,
		"bin/gemini":                   "wrapper",
		"runtime/bin/node":             "node",
	}
	if goos == "windows" {
		delete(files, "bin/gemini")
		delete(files, "runtime/bin/node")
		files["bin/gemini.cmd"] = "wrapper"
		files["runtime/node.exe"] = "node"
	}
	for relative, content := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	marker := []byte("component=gemini-cli\n")
	if err := os.WriteFile(filepath.Join(root, ".osverse-gemini-runtime"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, marker
}
