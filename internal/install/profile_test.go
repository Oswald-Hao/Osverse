package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureProfilePATHPreservesContentModeAndCreatesPrivateBackup(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".bashrc")
	backupRoot := filepath.Join(home, "backups")
	if err := os.Mkdir(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(backupRoot, "bashrc.before-osverse")
	original := []byte("export EDITOR=vim\n")
	if err := os.WriteFile(profile, original, 0o640); err != nil {
		t.Fatal(err)
	}

	state, err := ensureProfilePATH(profile, backup)
	if err != nil || !state.changed {
		t.Fatalf("ensureProfilePATH() = (%#v, %v)", state, err)
	}
	updated, _ := os.ReadFile(profile)
	if !bytesEqualPrefix(updated, original) || strings.Count(string(updated), pathBlockStart) != 1 || strings.Count(string(updated), pathBlockEnd) != 1 {
		t.Fatalf("updated profile = %q", updated)
	}
	info, _ := os.Stat(profile)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("profile mode = %o", info.Mode().Perm())
	}
	backupContent, _ := os.ReadFile(backup)
	backupInfo, _ := os.Stat(backup)
	if string(backupContent) != string(original) || backupInfo.Mode().Perm() != 0o600 {
		t.Fatalf("backup = %q mode %o", backupContent, backupInfo.Mode().Perm())
	}

	second, err := ensureProfilePATH(profile, backup)
	if err != nil || second.changed {
		t.Fatalf("idempotent ensure = (%#v, %v)", second, err)
	}
	again, _ := os.ReadFile(profile)
	if string(again) != string(updated) {
		t.Fatal("idempotent call changed profile")
	}
}

func TestEnsureProfilePATHRejectsConflictsAndSymlinks(t *testing.T) {
	home := t.TempDir()
	backupRoot := filepath.Join(home, "backups")
	if err := os.Mkdir(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(home, ".profile")
	conflict := []byte(pathBlockStart + "\npartial")
	if err := os.WriteFile(profile, conflict, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureProfilePATH(profile, filepath.Join(backupRoot, "profile")); err == nil {
		t.Fatal("partial managed block accepted")
	}
	unchanged, _ := os.ReadFile(profile)
	if string(unchanged) != string(conflict) {
		t.Fatal("conflicting profile changed")
	}

	target := filepath.Join(home, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".zshrc")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureProfilePATH(link, filepath.Join(backupRoot, "zshrc")); err == nil {
		t.Fatal("symlink profile accepted")
	}
}

func TestRestoreProfileRestoresExistingAndRemovesNewFile(t *testing.T) {
	home := t.TempDir()
	backupRoot := filepath.Join(home, "backups")
	if err := os.Mkdir(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(existing, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := ensureProfilePATH(existing, filepath.Join(backupRoot, "bashrc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := restoreProfile(state); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(existing)
	if string(content) != "before\n" {
		t.Fatalf("restored existing = %q", content)
	}

	created := filepath.Join(home, ".zshrc")
	state, err = ensureProfilePATH(created, filepath.Join(backupRoot, "zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := restoreProfile(state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(created); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new profile remains after restore: %v", err)
	}
}

func bytesEqualPrefix(value, prefix []byte) bool {
	return len(value) >= len(prefix) && string(value[:len(prefix)]) == string(prefix)
}
