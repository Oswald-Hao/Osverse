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
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	"github.com/Oswald-Hao/Osverse/internal/removal"
	xwindows "golang.org/x/sys/windows"
)

type commandRunnerFunc func(context.Context, platform.CommandRequest) (platform.CommandResult, error)

func (run commandRunnerFunc) Run(ctx context.Context, request platform.CommandRequest) (platform.CommandResult, error) {
	return run(ctx, request)
}

func TestRetryTransientMoveWaitsOnlyForInUseErrors(t *testing.T) {
	attempts, waits := 0, 0
	err := retryTransientMove(context.Background(), 4, 200*time.Millisecond, func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != 200*time.Millisecond {
			t.Fatalf("retry delay = %v", delay)
		}
		return nil
	}, func() error {
		attempts++
		if attempts < 3 {
			return platformwindows.ErrMoveInUse
		}
		return nil
	})
	if err != nil || attempts != 3 || waits != 2 {
		t.Fatalf("retryTransientMove() = (attempts=%d, waits=%d, err=%v)", attempts, waits, err)
	}

	want := errors.New("permanent move failure")
	waits = 0
	err = retryTransientMove(context.Background(), 4, 0, func(context.Context, time.Duration) error {
		waits++
		return nil
	}, func() error { return want })
	if !errors.Is(err, want) || waits != 0 {
		t.Fatalf("permanent retry = (waits=%d, err=%v)", waits, err)
	}
}

func TestRetryTransientMoveStopsAtBoundAndCancellation(t *testing.T) {
	attempts, waits := 0, 0
	err := retryTransientMove(context.Background(), 3, 0, func(context.Context, time.Duration) error {
		waits++
		return nil
	}, func() error {
		attempts++
		return platformwindows.ErrMoveInUse
	})
	if !errors.Is(err, platformwindows.ErrMoveInUse) || attempts != 3 || waits != 2 {
		t.Fatalf("bounded retry = (attempts=%d, waits=%d, err=%v)", attempts, waits, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts = 0
	err = retryTransientMove(ctx, 3, time.Second, waitForRemovalRetry, func() error {
		attempts++
		return platformwindows.ErrMoveInUse
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("canceled retry = (attempts=%d, err=%v)", attempts, err)
	}
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

func TestBrokenLegacyHarnessCanBeRemovedWhenRuntimeTargetIsMissing(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-broken-legacy-harness", nil }

	toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", "deepseek-harness")
	if err := os.MkdirAll(toolRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	// Beta 2 through Beta 10 all wrote this exact v1 marker and target layout.
	// Model the real half-install: the owned shim remains, but its wrapper was
	// never committed (or was damaged), so detection reports "broken".
	target := filepath.Join(toolRoot, "0.1.0-rc.6", "bin", "dsh.cmd")
	shim := filepath.Join(home, ".local", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "@rem Osverse managed shim v1: deepseek-harness\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + target + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusBroken}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatalf("CreatePlan() for legacy broken Harness = %v", err)
	}
	if len(plan.Effects) != 3 || plan.Effects[0].Path != shim || plan.Effects[1].Path != toolRoot {
		t.Fatalf("legacy recovery effects = %#v", plan.Effects)
	}
	result, err := manager.Execute(context.Background(), plan.ID, component)
	if err != nil || !result.Removed {
		t.Fatalf("Execute() for legacy broken Harness = (%#v, %v)", result, err)
	}
	for _, removed := range []string{shim, toolRoot} {
		if _, err := os.Lstat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy broken path remains: %s: %v", removed, err)
		}
	}
}

func TestBrokenLegacyHarnessShimOnlyRemovalPreservesExternalTarget(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-shim-only-harness", nil }
	shim := filepath.Join(home, ".local", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(home, "Documents", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(external), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("external runtime"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := "@rem Osverse managed shim v1: deepseek-harness\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + external + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusBroken}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatalf("external legacy target CreatePlan() = %v", err)
	}
	if len(plan.Effects) != 2 || plan.Effects[0].Path != shim {
		t.Fatalf("external target recovery effects = %#v", plan.Effects)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(shim); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned shim remains: %v", err)
	}
	if raw, err := os.ReadFile(external); err != nil || string(raw) != "external runtime" {
		t.Fatalf("external target changed = (%q, %v)", raw, err)
	}
}

func TestExternalPerUserHarnessEntryMovesToRecoveryWithoutDeletingTarget(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-external-user-harness", nil }
	target := filepath.Join(home, "AppData", "Roaming", "npm", "node_modules", "@deepseek-ai", "dsh", "bin.js")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("external runtime"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(home, "AppData", "Roaming", "npm", "dsh.cmd")
	content := "@echo off\r\nnode \"" + target + "\" %*\r\n"
	if err := os.WriteFile(entry, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: entry, ResolvedPath: entry, Source: "path", Managed: false, Version: "0.1.0-rc.6"}}}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatalf("CreatePlan() for external per-user entry = %v", err)
	}
	if len(plan.Effects) != 2 || plan.Effects[0].Path != entry || plan.Effects[0].Action != "recover" || !plan.Effects[0].Recoverable {
		t.Fatalf("external entry effects = %#v", plan.Effects)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(entry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external command entry remains: %v", err)
	}
	if raw, err := os.ReadFile(target); err != nil || string(raw) != "external runtime" {
		t.Fatalf("external runtime changed = (%q, %v)", raw, err)
	}
}

func TestEveryManagedCLIRecoversResidualToolRootWithoutScanInstallation(t *testing.T) {
	for _, tc := range []struct{ id, name string }{
		{"claude-code", "Claude Code"}, {"codex-cli", "Codex CLI"}, {"opencode-cli", "OpenCode CLI"},
		{"deepseek-harness", "DeepSeek Harness"}, {"qwen-code", "Qwen Code"}, {"kimi-code", "Kimi Code"},
		{"github-copilot-cli", "GitHub Copilot CLI"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			manager, err := NewManager(home)
			if err != nil {
				t.Fatal(err)
			}
			manager.randomID = func() (string, error) { return "remove-residual-" + tc.id, nil }
			toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", tc.id)
			if err := os.MkdirAll(toolRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(toolRoot, "partial.download"), []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}
			component := domain.Component{ID: tc.id, Name: tc.name, Category: "Core CLI", Status: domain.StatusBroken}
			plan, err := manager.CreatePlan(context.Background(), component)
			if err != nil {
				t.Fatalf("CreatePlan() = %v", err)
			}
			if len(plan.Effects) != 2 || plan.Effects[0].Path != toolRoot {
				t.Fatalf("residual effects = %#v", plan.Effects)
			}
			if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(toolRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("tool root remains: %v", err)
			}
		})
	}
}

func TestManagedCLIRemovalRejectsScannedCommandOutsideUserProfile(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outsideRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(outsideRoot, "system-bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(outside), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: outside, ResolvedPath: outside, Source: "path"}}}
	if _, err := manager.CreatePlan(context.Background(), component); !errors.Is(err, removal.ErrRemovalUnsupported) {
		t.Fatalf("outside-user CreatePlan() = %v, want ErrRemovalUnsupported", err)
	}
	if _, err := os.Lstat(outside); err != nil {
		t.Fatalf("outside-user command changed: %v", err)
	}
}

func TestBrokenHarnessResidualToolRootCanBeRemovedWithoutShim(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-residual-tool-root", nil }
	toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", "deepseek-harness")
	if err := os.MkdirAll(filepath.Join(toolRoot, "incomplete"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolRoot, "incomplete", "download.part"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusBroken}

	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatalf("CreatePlan() for residual tool root = %v", err)
	}
	if len(plan.Effects) != 2 || plan.Effects[0].Path != toolRoot || plan.Effects[1].Action != "manifest" {
		t.Fatalf("residual tool-root effects = %#v", plan.Effects)
	}
	result, err := manager.Execute(context.Background(), plan.ID, component)
	if err != nil || !result.Removed {
		t.Fatalf("Execute() for residual tool root = (%#v, %v)", result, err)
	}
	if _, err := os.Lstat(toolRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("residual tool root remains: %v", err)
	}
}

func TestBrokenHarnessIncompleteOwnedShimCanBeRemovedWithoutToolRoot(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-incomplete-shim", nil }
	shim := filepath.Join(home, ".local", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("@rem Osverse managed shim v1: deepseek-harness\r\n@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusBroken}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Effects) != 2 || plan.Effects[0].Path != shim {
		t.Fatalf("incomplete shim effects = %#v", plan.Effects)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(shim); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete owned shim remains: %v", err)
	}
}

func TestBrokenHarnessRecoversEditedOwnedShimButPreservesExternalShim(t *testing.T) {
	for _, tc := range []struct {
		name        string
		shim        func(string) string
		status      domain.ComponentStatus
		wantMoved   bool
		wantEffects int
	}{
		{
			name: "owned marker with explicit profile arguments",
			shim: func(target string) string {
				return "@rem Osverse managed shim v1: deepseek-harness\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + target + "\" --profile web %*\r\n"
			},
			status:    domain.StatusInstalled,
			wantMoved: true, wantEffects: 3,
		},
		{
			name: "freshly scanned external user shim",
			shim: func(target string) string {
				return "@echo off\r\n\"" + target + "\" %*\r\n"
			},
			status:    domain.StatusBroken,
			wantMoved: true, wantEffects: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			manager, err := NewManager(home)
			if err != nil {
				t.Fatal(err)
			}
			manager.randomID = func() (string, error) { return "remove-edited-owned-shim", nil }
			toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", "deepseek-harness")
			if err := os.MkdirAll(toolRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			shim := filepath.Join(home, ".local", "bin", "dsh.cmd")
			if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(toolRoot, "0.1.0-rc.6", "bin", "dsh.cmd")
			if err := os.WriteFile(shim, []byte(tc.shim(target)), 0o600); err != nil {
				t.Fatal(err)
			}
			component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: tc.status,
				Installations: []domain.Installation{{Path: shim, ResolvedPath: target}}}
			plan, err := manager.CreatePlan(context.Background(), component)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Effects) != tc.wantEffects {
				t.Fatalf("effects = %#v", plan.Effects)
			}
			if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
				t.Fatal(err)
			}
			_, shimErr := os.Lstat(shim)
			if tc.wantMoved && !errors.Is(shimErr, os.ErrNotExist) {
				t.Fatalf("owned shim remains: %v", shimErr)
			}
		})
	}
}

func TestHarnessRemovalRecognizesShortTargetInsideLongManagedRoot(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.randomID = func() (string, error) { return "remove-short-harness-target", nil }
	toolRoot := filepath.Join(home, "AppData", "Local", "Osverse", "tools", "deepseek-harness")
	target := filepath.Join(toolRoot, "0.1.0-rc.6", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shortTarget := removalWindowsShortPath(t, target)
	if strings.EqualFold(shortTarget, target) {
		t.Skip("8.3 short path names are disabled on this volume")
	}
	shim := filepath.Join(home, ".local", "bin", "dsh.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "@rem Osverse managed shim v1: deepseek-harness\r\n@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" + shortTarget + "\" %*\r\n"
	if err := os.WriteFile(shim, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	component := domain.Component{ID: "deepseek-harness", Name: "DeepSeek Harness", Category: "Core CLI", Status: domain.StatusInstalled,
		Installations: []domain.Installation{{Path: shim, ResolvedPath: shim}}}
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Effects) != 3 || plan.Effects[0].Path != shim || plan.Effects[1].Path != toolRoot {
		t.Fatalf("short-target effects = %#v", plan.Effects)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{shim, toolRoot} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed path remains after short-target removal: %s: %v", path, err)
		}
	}
}

func removalWindowsShortPath(t *testing.T, path string) string {
	t.Helper()
	longPath, err := xwindows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	required, err := xwindows.GetShortPathName(longPath, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if required == 0 {
		t.Fatal("GetShortPathName returned an empty path")
	}
	buffer := make([]uint16, required)
	written, err := xwindows.GetShortPathName(longPath, &buffer[0], uint32(len(buffer)))
	if err != nil {
		t.Fatal(err)
	}
	return xwindows.UTF16ToString(buffer[:written])
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
	manager.moveAttempts = 1
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

func TestManagedCommandWrapperRemovalRecoversUnexpectedWrapperName(t *testing.T) {
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
	plan, err := manager.CreatePlan(context.Background(), component)
	if err != nil {
		t.Fatalf("CreatePlan() = %v", err)
	}
	if _, err := manager.Execute(context.Background(), plan.ID, component); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{shim, toolRoot} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("corrupt managed path remains: %s: %v", path, err)
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
