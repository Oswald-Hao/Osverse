//go:build !windows

package managedcommand

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectProtectsExternalCommandAndActivateAddsOwnedLinks(t *testing.T) {
	home := t.TempDir()
	toolRoot := filepath.Join(home, ".local", "share", "osverse", "tools", "gemini-cli")
	binRoot := filepath.Join(home, ".local", "bin")
	paths := Paths{
		ToolRoot: toolRoot, CurrentPath: filepath.Join(toolRoot, "current"), BinRoot: binRoot,
		ShimPath: filepath.Join(binRoot, "gemini"), WrapperPath: filepath.Join(toolRoot, "0.57.0", "bin", "gemini"),
	}
	if err := os.MkdirAll(binRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ShimPath, []byte("external"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Inspect(home, "gemini-cli", "gemini", paths); !errors.Is(err, ErrExternalCommand) {
		t.Fatalf("external command error = %v", err)
	}
	if err := os.Remove(paths.ShimPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.WrapperPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.WrapperPath, []byte("wrapper"), 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(home, ".profile")
	if err := os.WriteFile(profile, []byte("export EXISTING=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Activate(home, "gemini-cli", "gemini", "0.57.0", paths); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{paths.CurrentPath, paths.ShimPath} {
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("link %s = %v, %v", link, info, err)
		}
	}
	raw, err := os.ReadFile(profile)
	if err != nil || !strings.Contains(string(raw), "export EXISTING=1") || !strings.Contains(string(raw), pathProfileStart) {
		t.Fatalf("profile = %q, %v", raw, err)
	}
}
