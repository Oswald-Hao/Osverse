//go:build windows

package windowsremoval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/removal"
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

func TestManagedCommandWrapperRemovalAcceptsGeneratedWrappers(t *testing.T) {
	cases := []struct {
		componentID string
		name        string
		command     string
		version     string
	}{
		{componentID: "deepseek-harness", name: "DeepSeek Harness", command: "dsh", version: "0.1.0-rc.6"},
		{componentID: "qwen-code", name: "Qwen Code", command: "qwen", version: "0.21.13"},
		{componentID: "kimi-code", name: "Kimi Code", command: "kimi", version: "0.36.1"},
		{componentID: "github-copilot-cli", name: "GitHub Copilot CLI", command: "copilot", version: "1.0.80"},
	}
	for _, tc := range cases {
		t.Run(tc.componentID, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			manager, err := NewManager(home)
			if err != nil {
				t.Fatal(err)
			}
			manager.now = func() time.Time { return time.Date(2026, time.August, 20, 11, 0, 0, 0, time.UTC) }
			manager.randomID = func() (string, error) { return "remove-" + tc.command + "-test", nil }
			toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", tc.componentID)
			target := filepath.Join(toolRoot, tc.version, "bin", tc.command+".cmd")
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("@echo off\r\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			shim := filepath.Join(home, ".local", "bin", tc.command+".cmd")
			if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
				t.Fatal(err)
			}
			content := "@rem Osverse managed shim v1: " + tc.componentID + "\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + target + "\" %*\r\n"
			if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			component := domain.Component{ID: tc.componentID, Name: tc.name, Category: "Core CLI", Status: domain.StatusInstalled,
				Installations: []domain.Installation{{Path: shim, ResolvedPath: shim, Source: "osverse", Managed: true, Version: tc.version}}}
			plan, err := manager.CreatePlan(context.Background(), component)
			if err != nil {
				t.Fatal(err)
			}
			defer manager.expire(plan.ID)
			if len(plan.Effects) != 3 || !strings.Contains(plan.Effects[1].Path, tc.componentID) {
				t.Fatalf("wrapper removal plan = %#v", plan)
			}
		})
	}
}

func TestManagedCommandWrapperRemovalRejectsUnexpectedWrapperName(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-wrong-wrapper-test", nil }
	toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", "kimi-code")
	target := filepath.Join(toolRoot, "0.36.1", "bin", "not-kimi.cmd")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(home, ".local", "bin", "kimi.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "@rem Osverse managed shim v1: kimi-code\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + target + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "kimi-code", Name: "Kimi Code", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: shim, ResolvedPath: shim, Source: "osverse", Managed: true, Version: "0.36.1"}}}
	if _, err := manager.CreatePlan(context.Background(), component); !errors.Is(err, removal.ErrRemovalUnsupported) {
		t.Fatalf("CreatePlan() err = %v, want ErrRemovalUnsupported", err)
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

func TestOpenCodeBetaRemovalUsesOfficialPerUserUninstaller(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	uninstaller := filepath.Join(home, "AppData", "Local", "Programs", "OpenCode Beta", "Uninstall OpenCode Beta.exe")
	if err := os.MkdirAll(filepath.Dir(uninstaller), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uninstaller, []byte("MZ fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-opencode-beta", nil }
	component := domain.Component{ID: "opencode-desktop", Name: "OpenCode Desktop", Category: "Desktop Applications", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: filepath.Join(filepath.Dir(uninstaller), "OpenCode Beta.exe"), Version: "1.18.18", Source: "registry"}}}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.expire(plan.ID)
	if len(plan.Effects) != 1 || plan.Effects[0].Path != uninstaller {
		t.Fatalf("OpenCode Beta removal plan = %#v", plan)
	}
}
