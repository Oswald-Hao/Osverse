//go:build windows

package windowsapps

import (
	"context"
	"strings"
	"testing"
)

func TestWindowsDesktopCatalogUsesExactPinnedSources(t *testing.T) {
	catalog, err := builtInCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 6 {
		t.Fatalf("catalog size = %d", len(catalog))
	}
	for id, item := range catalog {
		if item.Kind == "store" {
			want := map[string]string{"chatgpt-desktop": "9NT1R1C2HH7J", "codex-desktop": "9PLM9XGG6VKS"}[id]
			if item.StoreID != want {
				t.Fatalf("store artifact %s = %#v", id, item)
			}
			continue
		}
		if len(item.SHA256) != 64 || item.DownloadBytes <= 0 || !strings.HasPrefix(item.URL, "https://") {
			t.Fatalf("download artifact %s = %#v", id, item)
		}
	}
}

func TestWindowsDesktopPlanDisclosesStoreProductID(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager.arch = "amd64"
	plan, err := manager.CreatePlan(context.Background(), "codex-desktop")
	if err != nil {
		t.Fatal(err)
	}
	if plan.DownloadBytes != 0 || len(plan.Changes) != 1 || plan.Changes[0].Kind != "store" || plan.Changes[0].Path != "9PLM9XGG6VKS" {
		t.Fatalf("store plan = %#v", plan)
	}
}

func TestValidateInstallerRecognizesPEAndMSIHeaders(t *testing.T) {
	exe := t.TempDir() + `\installer.exe`
	if err := writeTestFile(exe, append([]byte{'M', 'Z'}, make([]byte, 6)...)); err != nil {
		t.Fatal(err)
	}
	if err := validateInstaller(exe, "exe"); err != nil {
		t.Fatal(err)
	}
	msi := t.TempDir() + `\installer.msi`
	if err := writeTestFile(msi, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}); err != nil {
		t.Fatal(err)
	}
	if err := validateInstaller(msi, "msi"); err != nil {
		t.Fatal(err)
	}
}
