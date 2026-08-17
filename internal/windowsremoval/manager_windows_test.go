//go:build windows

package windowsremoval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
)

func TestManagedCLIRemovalMovesFilesToRecovery(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC) }
	manager.randomID = func() (string, error) { return "remove-test", nil }
	toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", "claude-code")
	target := filepath.Join(toolRoot, "2.1.232", "package", "claude.exe")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("exe"), 0o600); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(home, ".local", "bin", "claude.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "@rem Osverse managed shim v1: claude-code\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + target + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "claude-code", Name: "Claude Code", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: shim, ResolvedPath: shim, Source: "osverse", Managed: true}}}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Execute(context.Background(), plan.ID, component)
	if err != nil || !result.Removed {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	if _, err := os.Stat(shim); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shim remains: %v", err)
	}
	recovery := filepath.Join(home, "AppData", "Local", "Osverse", "recovery", plan.ID)
	for _, name := range []string{"0-claude.cmd", "1-claude-code", "recovery.json"} {
		if _, err := os.Stat(filepath.Join(recovery, name)); err != nil {
			t.Errorf("recovery %s: %v", name, err)
		}
	}
}

func TestCodexStoreRemovalPlanUsesExactProductID(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "codex-desktop", Name: "Codex Desktop", Category: "Desktop Applications", Status: domain.StatusInstalled,
		Installations: nil}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Effects) != 1 || plan.Effects[0].Path != "9PLM9XGG6VKS" || plan.Effects[0].Recoverable || plan.Warning == "" {
		t.Fatalf("store removal plan = %#v", plan)
	}
}
