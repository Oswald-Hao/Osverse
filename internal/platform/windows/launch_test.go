//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"testing"

	xwindows "golang.org/x/sys/windows"
)

func TestNormalizeFinalPath(t *testing.T) {
	if got := normalizeFinalPath(`\\?\C:\Users\Alice\tool.exe`); got != `C:\Users\Alice\tool.exe` {
		t.Fatalf("normalizeFinalPath drive = %q", got)
	}
	if got := normalizeFinalPath(`\\?\UNC\server\share\tool.exe`); got != `\\server\share\tool.exe` {
		t.Fatalf("normalizeFinalPath UNC = %q", got)
	}
}

func TestLockedIdentityDetectsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.exe")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := openLockedIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	defer xwindows.CloseHandle(first.handle)
	if err := os.WriteFile(path+".next", []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".next", path); err == nil {
		t.Fatal("replacement succeeded while target identity was locked")
	}
}
