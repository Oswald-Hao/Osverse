//go:build linux

package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsNewestFirstDeduplicatedRedactedEntries(t *testing.T) {
	home := t.TempDir()
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Unix(1234, 0) }
	sequence := 0
	store.randomID = func() (string, error) { sequence++; return string(rune('a' + sequence)), nil }
	input := Input{OperationID: "task-1", ComponentID: "codex-cli", Name: "Codex CLI", Action: "install", Status: "completed", Message: "安装完成"}
	first, err := store.Append(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.Append(context.Background(), input)
	if err != nil || duplicate.ID != first.ID {
		t.Fatalf("duplicate = (%#v, %v)", duplicate, err)
	}
	if _, err := store.Append(context.Background(), Input{OperationID: "task-2", ComponentID: "api-profile", Name: "API 配置", Action: "configure", Status: "failed", Message: "部分目标失败"}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(context.Background())
	if err != nil || len(entries) != 2 || entries[0].OperationID != "task-2" {
		t.Fatalf("entries = (%#v, %v)", entries, err)
	}
	info, err := os.Stat(filepath.Join(home, ".local/share/osverse/state/history.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("history mode = %v, %v", info, err)
	}
	if err := store.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err = store.List(context.Background())
	if err != nil || len(entries) != 0 {
		t.Fatalf("cleared entries = (%#v, %v)", entries, err)
	}
}

func TestStoreRejectsInvalidAndSymlinkedHistory(t *testing.T) {
	home := t.TempDir()
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), Input{ComponentID: "../../bad", Name: "bad", Action: "install", Status: "completed", Message: "bad"}); err != ErrInvalidEntry {
		t.Fatalf("invalid = %v", err)
	}
	target := filepath.Join(home, "external")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, store.path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(context.Background()); err != ErrUnsafeStore {
		t.Fatalf("symlink list = %v", err)
	}
	if err := store.Clear(context.Background()); err != ErrUnsafeStore {
		t.Fatalf("symlink clear = %v", err)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "keep" {
		t.Fatalf("external target changed: %q", content)
	}
}
