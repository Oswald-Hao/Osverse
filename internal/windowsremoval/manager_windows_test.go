//go:build windows

package windowsremoval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	"github.com/Oswald-Hao/Osverse/internal/removal"
)

type commandRunnerFunc func(context.Context, platform.CommandRequest) (platform.CommandResult, error)

func (run commandRunnerFunc) Run(ctx context.Context, request platform.CommandRequest) (platform.CommandResult, error) {
	return run(ctx, request)
}

func TestSamePathUsesWindowsFileIdentityForAlternatePathSpellings(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "managed-shim.cmd")
	alias := filepath.Join(root, "alternate-shim.cmd")
	if err := os.WriteFile(original, []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, alias); err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(filepath.Clean(original), filepath.Clean(alias)) || !samePath(original, alias) {
		t.Fatalf("samePath(%q, %q) did not use file identity", original, alias)
	}
}

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
			profile := filepath.Join(home, "AppData", "Roaming", "Osverse-test", tc.command+".json")
			if err := os.MkdirAll(filepath.Dir(profile), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(profile, []byte("preserve-profile"), 0o600); err != nil {
				t.Fatal(err)
			}
			component := domain.Component{ID: tc.componentID, Name: tc.name, Category: "Core CLI", Status: domain.StatusInstalled,
				Installations: []domain.Installation{{Path: shim, ResolvedPath: shim, Source: "osverse", Managed: true, Version: tc.version}}}
			plan, err := manager.CreatePlan(context.Background(), component)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Effects) != 3 || !strings.Contains(plan.Effects[1].Path, tc.componentID) {
				t.Fatalf("wrapper removal plan = %#v", plan)
			}
			result, err := manager.Execute(context.Background(), plan.ID, component)
			if err != nil || !result.Removed {
				t.Fatalf("Execute() = (%#v, %v)", result, err)
			}
			for _, removed := range []string{shim, toolRoot} {
				if _, err := os.Lstat(removed); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("managed path remains after removal: %s: %v", removed, err)
				}
			}
			raw, err := os.ReadFile(profile)
			if err != nil || string(raw) != "preserve-profile" {
				t.Fatalf("profile changed = (%q, %v)", raw, err)
			}
			recovery := filepath.Join(home, "AppData", "Local", "Osverse", "recovery", plan.ID)
			manifestRaw, err := os.ReadFile(filepath.Join(recovery, "recovery.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct {
				SchemaVersion int               `json:"schemaVersion"`
				ComponentID   string            `json:"componentId"`
				Paths         map[string]string `json:"paths"`
			}
			if err := json.Unmarshal(manifestRaw, &manifest); err != nil || manifest.SchemaVersion != 1 ||
				manifest.ComponentID != tc.componentID || len(manifest.Paths) != 2 {
				t.Fatalf("recovery manifest = (%#v, %v)", manifest, err)
			}
			for destination, original := range manifest.Paths {
				if _, err := os.Lstat(destination); err != nil {
					t.Fatalf("recovery destination missing: %s: %v", destination, err)
				}
				if original != shim && original != toolRoot {
					t.Fatalf("unexpected recovery source: %s", original)
				}
			}
		})
	}
}

func TestManagedHarnessRemovalRevalidatesOwnershipWhenScanProvenanceIsStale(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return time.Date(2026, time.August, 20, 19, 0, 0, 0, time.UTC) }
	manager.randomID = func() (string, error) { return "remove-stale-harness", nil }

	toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", "deepseek-harness")
	target := filepath.Join(toolRoot, "0.1.0-rc.6", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(home, ".local", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "@rem Osverse managed shim v1: deepseek-harness\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + target + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Detection metadata is advisory. The removal manager must independently
	// revalidate the fixed shim and managed root before it captures either path.
	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: shim, ResolvedPath: shim, Source: "path", Managed: false, Version: "unknown"}}}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.expire(plan.ID)
	if len(plan.Effects) != 3 || plan.Effects[0].Path != shim || plan.Effects[1].Path != toolRoot {
		t.Fatalf("stale-provenance removal plan = %#v", plan)
	}

	// Version probing is an advisory display operation and can cross the
	// three-second timeout boundary between the preview scan and confirmation
	// scan. The already-pinned paths remain the removal trust boundary.
	current := component
	current.Status = domain.StatusBroken
	current.Message = "版本检测失败"
	current.Installations = append([]domain.Installation(nil), component.Installations...)
	current.Installations[0].Version = "0.1.0-rc.6"
	current.Installations[0].Source = "osverse"
	current.Installations[0].Managed = true
	result, err := manager.Execute(context.Background(), plan.ID, current)
	if err != nil || !result.Removed {
		t.Fatalf("Execute() with refreshed display metadata = (%#v, %v)", result, err)
	}
	for _, removed := range []string{shim, toolRoot} {
		if _, err := os.Lstat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed path remains after metadata drift: %s: %v", removed, err)
		}
	}
}

func TestManagedWrapperRemovalRollsBackWhenRuntimeIsLocked(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-locked-harness", nil }
	toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", "deepseek-harness")
	target := filepath.Join(toolRoot, "0.1.0-rc.6", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(home, ".local", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "@rem Osverse managed shim v1: deepseek-harness\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + target + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: shim, ResolvedPath: shim, Source: "osverse", Managed: true, Version: "0.1.0-rc.6"}}}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Execute(context.Background(), plan.ID, component); !errors.Is(err, removal.ErrComponentInUse) || result.Removed {
		_ = locked.Close()
		t.Fatalf("locked Execute() = (%#v, %v)", result, err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{shim, toolRoot, target} {
		if _, err := os.Lstat(preserved); err != nil {
			t.Fatalf("rollback did not preserve %s: %v", preserved, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, "AppData", "Local", "Osverse", "recovery", plan.ID, "recovery.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial recovery manifest exists: %v", err)
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

func TestOpenCodeRemovalAllowsTheValidatedUninstallerToDeleteItself(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	uninstaller := filepath.Join(home, "AppData", "Local", "Programs", "OpenCode", "Uninstall OpenCode.exe")
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
	manager.randomID = func() (string, error) { return "remove-self-deleting-opencode", nil }
	manager.runner = commandRunnerFunc(func(_ context.Context, request platform.CommandRequest) (platform.CommandResult, error) {
		if request.Path != uninstaller || request.PinnedExecutable == nil || !request.ReleasePinnedAfterStart {
			t.Fatalf("uninstaller request = %#v", request)
		}
		if err := os.Rename(uninstaller, uninstaller+".replaced"); err == nil {
			t.Fatal("uninstaller could be replaced before process start")
		}
		if err := request.PinnedExecutable.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(uninstaller); err != nil {
			t.Fatalf("self-delete after process start failed: %v", err)
		}
		return platform.CommandResult{ExitCode: 0}, nil
	})
	component := domain.Component{ID: "opencode-desktop", Name: "OpenCode Desktop", Category: "Desktop Applications", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: filepath.Join(filepath.Dir(uninstaller), "OpenCode.exe"), Version: "1.18.18", Source: "registry"}}}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Execute(context.Background(), plan.ID, component)
	if err != nil || !result.Removed {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	if _, err := os.Lstat(uninstaller); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uninstaller remains: %v", err)
	}
}
