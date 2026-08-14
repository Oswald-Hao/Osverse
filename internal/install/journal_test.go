package install

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverTransactionsRestoresInterruptedCommit(t *testing.T) {
	manager, item := transactionManager(t, testArchive(t, []tarEntry{{name: "package/bin/tool", body: []byte("binary")}}))
	manager.profiles = []string{filepath.Join(manager.home, ".profile")}
	root, err := manager.ensureStateRoot()
	if err != nil {
		t.Fatal(err)
	}
	toolRoot, err := ensureDirectories(root, 0o700, "tools", item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(toolRoot, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(toolRoot, "new"), 0o700); err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(toolRoot, "current")
	if err := os.Symlink("old", currentPath); err != nil {
		t.Fatal(err)
	}
	binRoot, err := ensureDirectories(manager.home, 0o755, ".local", "bin")
	if err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(binRoot, item.Command)
	oldShim := filepath.Join(toolRoot, "old", item.BinaryPath)
	if err := os.Symlink(oldShim, shimPath); err != nil {
		t.Fatal(err)
	}
	profilePath := manager.profiles[0]
	if err := os.WriteFile(profilePath, []byte("export EDITOR=vim\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	profile, err := inspectProfileState(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	stored := storedPlan{public: Plan{ID: "interrupted-plan", ComponentID: item.ID}, artifact: item}
	journalPath, err := manager.writeTransactionJournal(
		root, stored,
		linkState{exists: true, target: "old"},
		linkState{exists: true, target: oldShim},
		[]profileState{profile},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := replaceSymlink(currentPath, "new"); err != nil {
		t.Fatal(err)
	}
	if err := replaceSymlink(shimPath, filepath.Join(toolRoot, "new", item.BinaryPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, []byte("partially committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.recoverTransactions(); err != nil {
		t.Fatalf("recoverTransactions() error = %v", err)
	}
	if _, err := os.Lstat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("journal remains after recovery: %v", err)
	}
	current, _ := os.Readlink(currentPath)
	shim, _ := os.Readlink(shimPath)
	content, _ := os.ReadFile(profilePath)
	info, _ := os.Stat(profilePath)
	if current != "old" || shim != oldShim || string(content) != "export EDITOR=vim\n" || info.Mode().Perm() != 0o640 {
		t.Fatalf("recovery = current %q, shim %q, profile %q, mode %o", current, shim, content, info.Mode().Perm())
	}
}

func TestRecoverTransactionsRejectsEscapingJournal(t *testing.T) {
	manager, item := transactionManager(t, testArchive(t, []tarEntry{{name: "package/bin/tool", body: []byte("binary")}}))
	manager.profiles = []string{filepath.Join(manager.home, ".profile")}
	root, err := manager.ensureStateRoot()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := ensureDirectories(root, 0o700, "state", "transactions")
	if err != nil {
		t.Fatal(err)
	}
	journal := transactionJournal{
		SchemaVersion: transactionJournalVersion,
		PlanID:        "hostile-plan",
		ComponentID:   item.ID,
		Current:       journalLinkState{Exists: true, Target: "../../../../outside"},
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(directory, transactionJournalName(journal.PlanID))
	if err := atomicWriteJournal(journalPath, raw); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(manager.home, "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.recoverTransactions(); err == nil {
		t.Fatal("recoverTransactions() accepted an escaping target")
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "untouched" {
		t.Fatalf("outside file changed = (%q, %v)", content, err)
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("rejected journal should remain for diagnosis: %v", err)
	}
}

func TestSuccessfulTransactionRemovesCrashJournal(t *testing.T) {
	archive := testArchive(t, []tarEntry{{name: "package/bin/tool", body: []byte("binary")}})
	manager, item := transactionManager(t, archive)
	manager.profiles = nil
	plan, err := manager.CreatePlan(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := manager.Start(context.Background(), plan.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if finished := awaitTask(t, manager, task.ID); finished.Phase != "completed" {
		t.Fatalf("task = %#v", finished)
	}
	directory := filepath.Join(manager.home, ".local", "share", "osverse", "state", "transactions")
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("transaction journals = (%v, %v)", entries, err)
	}
}
