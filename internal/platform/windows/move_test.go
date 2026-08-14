//go:build windows

package windows

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMovableEvidenceRenamesThePinnedFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.cmd")
	destinationRoot := filepath.Join(root, "recovery")
	if err := os.Mkdir(destinationRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := OpenMovableEvidence(source)
	if err != nil {
		t.Fatal(err)
	}
	defer evidence.Close()
	destination := filepath.Join(destinationRoot, "source.cmd")
	if err := evidence.MoveTo(destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains: %v", err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil || string(raw) != "managed" || evidence.Path() != destination {
		t.Fatalf("moved evidence = (%q, %v, %q)", raw, err, evidence.Path())
	}
}

func TestMovableEvidenceRejectsReparsePoints(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if evidence, err := OpenMovableEvidence(link); err == nil {
		_ = evidence.Close()
		t.Fatal("reparse point was accepted")
	}
}
