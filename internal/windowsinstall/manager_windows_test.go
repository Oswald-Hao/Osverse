//go:build windows

package windowsinstall

import (
	"context"
	"errors"
	"os"
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

func TestWindowsActivationFailureRollsBackNewPayload(t *testing.T) {
	home := t.TempDir()
	payload := filepath.Join(home, "payload")
	destination := filepath.Join(home, "tools", "codex-cli", "0.147.0")
	marker := []byte("verified artifact\r\n")
	if err := os.MkdirAll(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, ".osverse-artifact"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	want := errors.New("activate shim")
	err := commitAndActivateWindowsPayload(home, payload, destination, marker, func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("new payload remains after failed activation: %v", statErr)
	}
}

func TestWindowsActivationFailurePreservesExistingPayload(t *testing.T) {
	home := t.TempDir()
	destination := filepath.Join(home, "tools", "codex-cli", "0.147.0")
	marker := []byte("verified artifact\r\n")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, ".osverse-artifact"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(home, "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, ".osverse-artifact"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("activate path")
	err := commitAndActivateWindowsPayload(home, payload, destination, marker, func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	if content, readErr := os.ReadFile(filepath.Join(destination, ".osverse-artifact")); readErr != nil || string(content) != string(marker) {
		t.Fatalf("existing payload changed: content=%q error=%v", content, readErr)
	}
}

func TestWindowsRollbackFailureIsReportedWithoutFalseNoInstallClaim(t *testing.T) {
	home := t.TempDir()
	payload := filepath.Join(home, "payload")
	destination := filepath.Join(home, "tools", "codex-cli", "0.147.0")
	marker := []byte("verified artifact\r\n")
	if err := os.MkdirAll(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(payload, ".osverse-artifact"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	err := commitAndActivateWindowsPayload(home, payload, destination, marker, func() error {
		if writeErr := os.WriteFile(filepath.Join(destination, ".osverse-artifact"), []byte("tampered"), 0o600); writeErr != nil {
			return writeErr
		}
		return errVersion
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("error = %v", err)
	}
	message := publicFailure(err)
	if !strings.Contains(message, "回滚失败") || strings.Contains(message, "未安装") {
		t.Fatalf("public failure = %q", message)
	}
}

func TestWindowsExistingPayloadMustMatchVerifiedStagingTree(t *testing.T) {
	home := t.TempDir()
	payload := filepath.Join(home, "payload")
	destination := filepath.Join(home, "tools", "codex-cli", "0.147.0")
	marker := []byte("verified artifact\r\n")
	for _, root := range []string{payload, destination} {
		if err := os.MkdirAll(filepath.Join(root, "package"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".osverse-artifact"), marker, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(payload, "package", "codex.exe"), []byte("verified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "package", "codex.exe"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	activated := false
	err := commitAndActivateWindowsPayload(home, payload, destination, marker, func() error {
		activated = true
		return nil
	})
	if err == nil || activated {
		t.Fatalf("error = %v, activated = %t", err, activated)
	}
}
