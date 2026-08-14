//go:build windows

package detect

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/domain"
)

type fakeWindowsPackageQuery struct {
	evidence WindowsPackageEvidence
	err      error
}

func (query fakeWindowsPackageQuery) Evidence(context.Context, WindowsDesktopSpec) (WindowsPackageEvidence, error) {
	return query.evidence, query.err
}

func TestDetectWindowsDesktopFindsFixedExecutable(t *testing.T) {
	home := t.TempDir()
	spec := WindowsDesktopSpecs()[4]
	path := filepath.Join(home, filepath.FromSlash("AppData/Local/Programs/CC Switch/CC Switch.exe"))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("MZ fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	component, err := DetectWindowsDesktop(context.Background(), spec,
		domain.SystemInfo{Supported: true}, nil,
		fakeWindowsPackageQuery{evidence: WindowsPackageEvidence{Installed: true, Version: "3.19.2", Source: "registry"}}, home)
	if err != nil || component.Status != domain.StatusInstalled || len(component.Installations) != 1 || component.Installations[0].Path != path {
		t.Fatalf("DetectWindowsDesktop() = (%#v, %v)", component, err)
	}
}

func TestDetectWindowsDesktopReportsBrokenPackageWithoutExecutable(t *testing.T) {
	spec := WindowsDesktopSpecs()[0]
	component, err := DetectWindowsDesktop(context.Background(), spec,
		domain.SystemInfo{Supported: true}, nil,
		fakeWindowsPackageQuery{evidence: WindowsPackageEvidence{Installed: true, Version: "1.0", Source: "registry"}}, t.TempDir())
	if err != nil || component.Status != domain.StatusBroken || len(component.Installations) != 0 {
		t.Fatalf("DetectWindowsDesktop() = (%#v, %v)", component, err)
	}
}

func TestWindowsDesktopSpecsIncludeCodexAndFixedCategories(t *testing.T) {
	specs := WindowsDesktopSpecs()
	if len(specs) != 6 {
		t.Fatalf("spec count = %d", len(specs))
	}
	seenCodex := false
	for _, spec := range specs {
		if !validWindowsDesktopSpec(spec) {
			t.Fatalf("invalid spec = %#v", spec)
		}
		seenCodex = seenCodex || spec.ID == "codex-desktop"
	}
	if !seenCodex {
		t.Fatal("Codex Desktop missing from Windows catalog")
	}
}
