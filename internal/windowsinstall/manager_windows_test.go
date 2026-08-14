//go:build windows

package windowsinstall

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsCatalogPinsVerifiedOfficialArtifacts(t *testing.T) {
	catalog, err := builtInCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 3 {
		t.Fatalf("catalog size = %d", len(catalog))
	}
	for _, id := range []string{"claude-code", "codex-cli", "opencode-cli"} {
		item, ok := catalog[id]
		if !ok || len(item.SHA256) != 64 || item.DownloadBytes <= 50_000_000 ||
			!strings.HasPrefix(item.URL, "https://registry.npmjs.org/") || !strings.HasSuffix(item.BinaryPath, ".exe") {
			t.Fatalf("artifact %q = %#v", id, item)
		}
	}
}

func TestWindowsPlanContainsOnlyUserScopedEffects(t *testing.T) {
	home := t.TempDir()
	manager, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	manager.arch = "amd64"
	manager.now = func() time.Time { return time.Date(2026, time.August, 14, 8, 0, 0, 0, time.UTC) }
	manager.randomID = func() (string, error) { return "plan", nil }
	plan, err := manager.CreatePlan(context.Background(), "codex-cli")
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != "plan" || plan.Version != "0.147.0" || len(plan.Changes) != 4 {
		t.Fatalf("plan = %#v", plan)
	}
	for _, change := range plan.Changes {
		if change.Kind == "download" || change.Kind == "registry" {
			continue
		}
		relative, err := filepath.Rel(home, change.Path)
		if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("effect escapes home: %#v", change)
		}
	}
}

func TestSafeArchiveNameRejectsWindowsTraversalAndDevices(t *testing.T) {
	valid := []string{"package/", "package/bin/tool.exe", "package/codex-resources/data.json"}
	for _, name := range valid {
		if !safeArchiveName(name) {
			t.Errorf("safeArchiveName(%q) = false", name)
		}
	}
	invalid := []string{
		"../outside.exe", "/absolute.exe", `package\tool.exe`, "package/C:/tool.exe",
		"package/CON", "package/aux.txt", "package/name. ", "package//tool.exe", "package/tool.exe:stream",
	}
	for _, name := range invalid {
		if safeArchiveName(name) {
			t.Errorf("safeArchiveName(%q) = true", name)
		}
	}
}

func TestManagedShimEscapesPercentInUserPath(t *testing.T) {
	managedRoot := `C:\Users\100%\AppData\Local\Osverse\tools`
	content := string(managedShim("claude-code", managedRoot+`\claude-code\2.1.232\package\claude.exe`))
	if !strings.Contains(content, `C:\Users\100%%\AppData`) ||
		!strings.HasPrefix(content, shimMarkerPrefix+"claude-code\r\n") ||
		!validManagedShim([]byte(content), "claude-code", managedRoot) {
		t.Fatalf("managed shim = %q", content)
	}
}
