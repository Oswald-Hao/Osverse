//go:build windows

package windowsapps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/detect"
)

type fakeInstallPackageQuery struct {
	evidence detect.WindowsPackageEvidence
	err      error
	calls    int
	specID   string
}

func (query *fakeInstallPackageQuery) Evidence(_ context.Context, spec detect.WindowsDesktopSpec) (detect.WindowsPackageEvidence, error) {
	query.calls++
	query.specID = spec.ID
	return query.evidence, query.err
}

func TestInstallEvidenceAcceptsRegisteredCustomExecutable(t *testing.T) {
	t.Parallel()

	custom := filepath.Join(t.TempDir(), "Custom OpenCode", "OpenCode.exe")
	if err := os.MkdirAll(filepath.Dir(custom), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(custom, []byte("MZ installed"), 0o600); err != nil {
		t.Fatal(err)
	}
	packages := &fakeInstallPackageQuery{evidence: detect.WindowsPackageEvidence{
		Installed: true, Source: "registry", ExecutablePaths: []string{custom},
	}}
	err := waitForInstallEvidence(context.Background(), t.TempDir(), "opencode-desktop",
		[]string{`AppData\Local\Programs\OpenCode\OpenCode.exe`}, packages)
	if err != nil {
		t.Fatalf("registered custom installation rejected: %v", err)
	}
	if packages.calls != 1 || packages.specID != "opencode-desktop" {
		t.Fatalf("package evidence calls = %d, spec = %q", packages.calls, packages.specID)
	}
}

func TestInstallEvidenceRejectsStaleRegistryEntryWithoutExecutable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	packages := &fakeInstallPackageQuery{evidence: detect.WindowsPackageEvidence{Installed: true, Source: "registry"}}
	err := waitForInstallEvidence(ctx, t.TempDir(), "opencode-desktop", nil, packages)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("stale registry evidence error = %v, want cancellation after evidence rejection", err)
	}
}

func TestInstallEvidenceAcceptsMSIXRegistrationWithoutAlias(t *testing.T) {
	t.Parallel()

	packages := &fakeInstallPackageQuery{evidence: detect.WindowsPackageEvidence{Installed: true, Source: "msix"}}
	if err := waitForInstallEvidence(context.Background(), t.TempDir(), "codex-desktop", nil, packages); err != nil {
		t.Fatalf("MSIX registration rejected: %v", err)
	}
}

func TestInstallEvidencePrefersFixedExecutableWhenRegistryFails(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	relative := `AppData\Local\Programs\Claude\Claude.exe`
	executable := filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(relative, `\`, `/`)))
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("MZ installed"), 0o600); err != nil {
		t.Fatal(err)
	}
	packages := &fakeInstallPackageQuery{err: errors.New("registry unavailable")}
	if err := waitForInstallEvidence(context.Background(), home, "claude-desktop", []string{relative}, packages); err != nil {
		t.Fatalf("fixed executable rejected: %v", err)
	}
	if packages.calls != 0 {
		t.Fatalf("registry queried despite fixed evidence: %d calls", packages.calls)
	}
}

func TestInstallerCatalogPathsStayAlignedWithDetectionSpecs(t *testing.T) {
	t.Parallel()

	catalog, err := builtInCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for id, item := range catalog {
		spec, ok := windowsDesktopSpec(id)
		if !ok {
			t.Errorf("installer component %q has no detector specification", id)
			continue
		}
		for _, expected := range item.ExpectedPaths {
			matched := false
			for _, detected := range spec.RelativeExecutables {
				matched = matched || strings.EqualFold(expected, detected)
			}
			if !matched {
				t.Errorf("installer path %q for %s is absent from detector", expected, id)
			}
		}
	}
}
