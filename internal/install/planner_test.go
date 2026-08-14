package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestManagerRejectsUnsafeHomesAndUnsupportedArchitecture(t *testing.T) {
	value, err := builtInManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, home := range []string{"", ".", "relative/home", "/"} {
		if _, err := newManager(home, "amd64", artifactCatalog(value), time.Now, func() (string, error) { return "id", nil }); !errors.Is(err, ErrInvalidHome) {
			t.Errorf("home %q error = %v, want ErrInvalidHome", home, err)
		}
	}
	manager := testManager(t, "/home/test", "arm64")
	if _, err := manager.CreatePlan(context.Background(), "codex-cli"); !errors.Is(err, ErrUnsupportedTarget) {
		t.Fatalf("arm64 CreatePlan() error = %v", err)
	}
}

func TestNewManagerResolvesTheExistingHome(t *testing.T) {
	realHome := t.TempDir()
	parent := t.TempDir()
	alias := filepath.Join(parent, "home-link")
	if err := os.Symlink(realHome, alias); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(alias)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	resolved, _ := filepath.EvalSymlinks(realHome)
	if manager.home != resolved {
		t.Fatalf("manager home = %q, want %q", manager.home, resolved)
	}
	if _, err := NewManager(filepath.Join(parent, "missing")); !errors.Is(err, ErrInvalidHome) {
		t.Fatalf("missing home error = %v", err)
	}
}

func TestCreatePlanDerivesAllEffectsFromCatalogAndHome(t *testing.T) {
	now := time.Date(2026, time.August, 14, 3, 4, 5, 0, time.UTC)
	manager := testManager(t, "/home/test", "amd64")
	manager.now = func() time.Time { return now }
	manager.randomID = func() (string, error) { return "server-plan-id", nil }

	plan, err := manager.CreatePlan(context.Background(), "claude-code")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if plan.ID != "server-plan-id" || plan.ComponentID != "claude-code" || plan.Version != "2.1.232" || plan.Command != "claude" {
		t.Fatalf("plan identity = %#v", plan)
	}
	if plan.CreatedAt != now || plan.ExpiresAt != now.Add(planLifetime) || plan.DownloadBytes != 96700641 {
		t.Fatalf("plan metadata = %#v", plan)
	}
	wantPaths := []string{
		"registry.npmjs.org",
		filepath.Join("/home/test", ".local", "share", "osverse", "tools", "claude-code", "2.1.232"),
		filepath.Join("/home/test", ".local", "share", "osverse", "tools", "claude-code", "current"),
		filepath.Join("/home/test", ".local", "bin", "claude"),
	}
	gotPaths := make([]string, len(plan.Changes))
	for index, change := range plan.Changes {
		gotPaths[index] = change.Path
		if change.Description == "" {
			t.Errorf("change %d has empty description", index)
		}
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("change paths = %#v, want %#v", gotPaths, wantPaths)
	}

	plan.Changes[0].Path = "tampered"
	stored := manager.plans["server-plan-id"].public
	if stored.Changes[0].Path != "registry.npmjs.org" {
		t.Fatal("caller mutated stored plan")
	}
}

func TestPlanIsSingleUseAndExpires(t *testing.T) {
	now := time.Date(2026, time.August, 14, 3, 4, 5, 0, time.UTC)
	manager := testManager(t, "/home/test", "amd64")
	manager.now = func() time.Time { return now }
	manager.randomID = func() (string, error) { return "one-use", nil }
	if _, err := manager.CreatePlan(context.Background(), "codex-cli"); err != nil {
		t.Fatal(err)
	}
	stored, err := manager.consumePlan("one-use")
	if err != nil || stored.artifact.ID != "codex-cli" {
		t.Fatalf("consumePlan() = (%#v, %v)", stored, err)
	}
	if _, err := manager.consumePlan("one-use"); !errors.Is(err, ErrPlanUnavailable) {
		t.Fatalf("second consume error = %v", err)
	}

	manager.randomID = func() (string, error) { return "expired", nil }
	if _, err := manager.CreatePlan(context.Background(), "opencode-cli"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(planLifetime)
	if _, err := manager.consumePlan("expired"); !errors.Is(err, ErrPlanUnavailable) {
		t.Fatalf("expired consume error = %v", err)
	}
}

func TestCreatePlanRejectsUnknownAndCanceledRequests(t *testing.T) {
	manager := testManager(t, "/home/test", "amd64")
	if _, err := manager.CreatePlan(context.Background(), "../../codex"); !errors.Is(err, ErrUnknownComponent) {
		t.Fatalf("unknown error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.CreatePlan(ctx, "codex-cli"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

func testManager(t *testing.T, home, arch string) *Manager {
	t.Helper()
	value, err := builtInManifest()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := newManager(home, arch, artifactCatalog(value), time.Now, func() (string, error) { return "id", nil })
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
