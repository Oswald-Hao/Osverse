package removal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
)

func TestUserInstallationIsMovedToRecoverableTrashAfterConfirmation(t *testing.T) {
	home := t.TempDir()
	command := writeRemovalFile(t, filepath.Join(home, ".local", "bin", "claude"), 0o700)
	config := writeRemovalFile(t, filepath.Join(home, ".claude", "settings.json"), 0o600)
	component := removalComponent("claude-code", "Core CLI", domain.Installation{
		Path: command, ResolvedPath: command, Version: "2.1.232", Source: "path",
	})
	manager := testRemovalManager(t, home, nil)

	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Effects) != 1 || plan.Effects[0].Path != command || !plan.Effects[0].Recoverable || plan.Effects[0].Action != "trash" {
		t.Fatalf("plan effects = %#v", plan.Effects)
	}
	if _, err := os.Stat(command); err != nil {
		t.Fatal("preview changed disk")
	}
	result, err := manager.Execute(context.Background(), plan.ID, component)
	if err != nil || !result.Removed {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	if _, err := os.Lstat(command); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command still exists: %v", err)
	}
	if _, err := os.Stat(config); err != nil {
		t.Fatalf("configuration was removed: %v", err)
	}
	trashFiles := filepath.Join(home, ".local", "share", "Trash", "files")
	entries, err := os.ReadDir(trashFiles)
	if err != nil || len(entries) != 1 {
		t.Fatalf("trash entries = %#v, %v", entries, err)
	}
	infoEntries, err := os.ReadDir(filepath.Join(home, ".local", "share", "Trash", "info"))
	if err != nil || len(infoEntries) != 1 {
		t.Fatalf("trash info = %#v, %v", infoEntries, err)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); !errors.Is(err, ErrPlanUnavailable) {
		t.Fatalf("reused plan error = %v", err)
	}
}

func TestManagedCLIPlanRemovesOnlyOwnedEntryAndRoot(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".local", "share", "osverse", "tools", "opencode-cli")
	target := writeRemovalFile(t, filepath.Join(root, "1.18.18", "package", "bin", "opencode"), 0o700)
	launcher := filepath.Join(home, ".local", "bin", "opencode")
	if err := os.MkdirAll(filepath.Dir(launcher), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, launcher); err != nil {
		t.Fatal(err)
	}
	component := removalComponent("opencode-cli", "Core CLI", domain.Installation{
		Path: launcher, ResolvedPath: target, Version: "1.18.18", Source: "osverse", Managed: true,
	})
	manager := testRemovalManager(t, home, nil)
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Effects) != 2 || plan.Effects[0].Path != launcher || plan.Effects[1].Path != root {
		t.Fatalf("managed effects = %#v", plan.Effects)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{launcher, root} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed path %s remains: %v", path, err)
		}
	}
}

func TestDpkgRemovalDelegatesOnlyFixedComponent(t *testing.T) {
	home := t.TempDir()
	remover := &fakeSystemRemover{}
	manager := testRemovalManager(t, home, remover)
	component := removalComponent("claude-desktop", "Desktop Applications", domain.Installation{
		Path: "/usr/bin/claude-desktop", ResolvedPath: "/usr/bin/claude-desktop", Version: "1.0.0", Source: "dpkg",
	})
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil || len(plan.Effects) != 1 || plan.Effects[0].Action != "package" || plan.Effects[0].Recoverable {
		t.Fatalf("CreatePlan() = (%#v, %v)", plan, err)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
		t.Fatal(err)
	}
	if remover.componentID != "claude-desktop" {
		t.Fatalf("removed package component = %q", remover.componentID)
	}
}

func TestRemovalRejectsUnsafeUnknownAndStaleEvidence(t *testing.T) {
	home := t.TempDir()
	manager := testRemovalManager(t, home, nil)
	if _, err := manager.CreatePlan(context.Background(), removalComponent("unknown", "Core CLI", domain.Installation{Path: "/tmp/x", ResolvedPath: "/tmp/x"})); !errors.Is(err, ErrRemovalUnsupported) {
		t.Fatalf("unknown error = %v", err)
	}
	if _, err := manager.CreatePlan(context.Background(), removalComponent("codex-cli", "Core CLI", domain.Installation{Path: "/usr/bin/codex", ResolvedPath: "/usr/bin/codex", Source: "path"})); !errors.Is(err, ErrRemovalUnsupported) {
		t.Fatalf("system path error = %v", err)
	}

	command := writeRemovalFile(t, filepath.Join(home, ".local", "bin", "codex"), 0o700)
	component := removalComponent("codex-cli", "Core CLI", domain.Installation{Path: command, ResolvedPath: command, Source: "path"})
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(command, []byte("replacement"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); !errors.Is(err, ErrEvidenceChanged) {
		t.Fatalf("changed identity error = %v", err)
	}
	if _, err := os.Stat(command); err != nil {
		t.Fatal("replacement was removed")
	}
}

func TestTrashMoveRollsBackEarlierEntriesWhenAConflictAppears(t *testing.T) {
	home := t.TempDir()
	first := writeRemovalFile(t, filepath.Join(home, ".local", "bin", "codex"), 0o700)
	second := writeRemovalFile(t, filepath.Join(home, ".npm", "bin", "codex"), 0o700)
	component := domain.Component{
		ID: "codex-cli", Name: "Codex CLI", Category: "Core CLI", Status: domain.StatusConflict,
		Installations: []domain.Installation{
			{Path: first, ResolvedPath: first, Source: "path"},
			{Path: second, ResolvedPath: second, Source: "path"},
		},
	}
	manager := testRemovalManager(t, home, nil)
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	trashFiles, err := ensurePrivateDirectories(home, ".local", "share", "Trash", "files")
	if err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(trashFiles, plan.ID+"-01-"+filepath.Base(second))
	if err := os.WriteFile(conflict, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); !errors.Is(err, ErrRemovalFailed) {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("rollback did not restore %s: %v", path, err)
		}
	}
}

func TestUnsafeTrashSymlinkNeverMovesSource(t *testing.T) {
	home := t.TempDir()
	command := writeRemovalFile(t, filepath.Join(home, ".local", "bin", "opencode"), 0o700)
	component := removalComponent("opencode-cli", "Core CLI", domain.Installation{Path: command, ResolvedPath: command, Source: "path"})
	manager := testRemovalManager(t, home, nil)
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	share := filepath.Join(home, ".local", "share")
	if err := os.MkdirAll(share, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(share, "Trash")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); !errors.Is(err, ErrRemovalFailed) {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := os.Stat(command); err != nil {
		t.Fatalf("source moved through unsafe trash: %v", err)
	}
}

func testRemovalManager(t *testing.T, home string, system systemRemover) *Manager {
	t.Helper()
	manager, err := newManager(home, system, func() time.Time { return time.Unix(1000, 0).UTC() }, func() (string, error) { return "remove-plan", nil })
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func removalComponent(id, category string, installation domain.Installation) domain.Component {
	return domain.Component{ID: id, Name: id, Category: category, Status: domain.StatusInstalled, Installations: []domain.Installation{installation}}
}

func writeRemovalFile(t *testing.T, path string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("component"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

type fakeSystemRemover struct{ componentID string }

func (remover *fakeSystemRemover) Remove(_ context.Context, componentID string) error {
	remover.componentID = componentID
	return nil
}
