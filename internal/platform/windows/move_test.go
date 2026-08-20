//go:build windows

package windows

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	xwindows "golang.org/x/sys/windows"
)

func TestClassifyRenameErrorReportsOnlyLockStatusesAsInUse(t *testing.T) {
	for _, status := range []xwindows.NTStatus{
		xwindows.STATUS_ACCESS_DENIED,
		xwindows.STATUS_SHARING_VIOLATION,
		xwindows.STATUS_FILE_LOCK_CONFLICT,
		xwindows.STATUS_LOCK_NOT_GRANTED,
	} {
		if err := classifyRenameError(status); !errors.Is(err, ErrMoveInUse) {
			t.Errorf("classifyRenameError(%v) = %v, want ErrMoveInUse", status, err)
		}
	}
	other := xwindows.STATUS_OBJECT_PATH_NOT_FOUND
	if err := classifyRenameError(other); errors.Is(err, ErrMoveInUse) || !errors.Is(err, other) {
		t.Errorf("classifyRenameError(%v) = %v, want unchanged", other, err)
	}
}

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
	if evidence.Path() != destination {
		t.Fatalf("moved evidence path = %q, want %q", evidence.Path(), destination)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains: %v", err)
	}
	if err := evidence.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil || string(raw) != "managed" {
		t.Fatalf("moved evidence = (%q, %v)", raw, err)
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
