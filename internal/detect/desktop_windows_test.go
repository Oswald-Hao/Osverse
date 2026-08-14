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

func TestDetectWindowsStoreDesktopAcceptsPackageWhenAliasIsDisabled(t *testing.T) {
	spec := WindowsDesktopSpecs()[2]
	component, err := DetectWindowsDesktop(context.Background(), spec, domain.SystemInfo{Supported: true}, nil,
		fakeWindowsPackageQuery{evidence: WindowsPackageEvidence{Installed: true, Version: "26.721.3996.0", Source: "msix"}}, t.TempDir())
	if err != nil || component.Status != domain.StatusInstalled || component.Message != "已安装（应用执行别名未启用）" {
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

func TestChatGPTAndCodexDesktopSpecsDoNotCrossMatch(t *testing.T) {
	specs := WindowsDesktopSpecs()
	chatGPT, codex := specs[1], specs[2]
	if len(chatGPT.ExecutableNames) != 1 || chatGPT.ExecutableNames[0] != "ChatGPT.exe" ||
		len(chatGPT.AppModelPrefixes) != 1 || chatGPT.AppModelPrefixes[0] != "OpenAI.ChatGPT" {
		t.Fatalf("ChatGPT spec crosses product identity: %#v", chatGPT)
	}
	if len(codex.ExecutableNames) != 1 || codex.ExecutableNames[0] != "Codex.exe" ||
		len(codex.AppModelPrefixes) != 1 || codex.AppModelPrefixes[0] != "OpenAI.Codex" {
		t.Fatalf("Codex spec crosses product identity: %#v", codex)
	}
}

func TestDetectWindowsOpenCodeDesktopUsesRegistryInstallLocation(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(t.TempDir(), "Custom OpenCode", "OpenCode.exe")
	if err := os.MkdirAll(filepath.Dir(custom), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(custom, []byte("MZ fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := WindowsDesktopSpecs()[3]
	component, err := DetectWindowsDesktop(context.Background(), spec, domain.SystemInfo{Supported: true}, nil,
		fakeWindowsPackageQuery{evidence: WindowsPackageEvidence{Installed: true, Version: "1.14.48", Source: "registry", ExecutablePaths: []string{custom}}}, home)
	if err != nil || component.Status != domain.StatusInstalled || len(component.Installations) != 1 || component.Installations[0].Path != custom {
		t.Fatalf("DetectWindowsDesktop() = (%#v, %v)", component, err)
	}
}

func TestRegistryExecutablePathsAcceptsInstallLocationAndDisplayIcon(t *testing.T) {
	spec := WindowsDesktopSpecs()[3]
	paths := registryExecutablePaths(spec, `C:\Program Files\OpenCode`, `"D:\Apps\OpenCode\OpenCode.exe",0`)
	want := []string{`C:\Program Files\OpenCode\OpenCode.exe`, `C:\Program Files\OpenCode\opencode-desktop.exe`, `D:\Apps\OpenCode\OpenCode.exe`}
	if len(paths) != len(want) {
		t.Fatalf("paths = %#v", paths)
	}
	for index := range want {
		if paths[index] != want[index] {
			t.Fatalf("paths = %#v", paths)
		}
	}
}

func TestRegistryDisplayNameAcceptsOnlyVersionedProductSuffix(t *testing.T) {
	if !matchesRegistryDisplayName([]string{"OpenCode"}, "OpenCode 1.14.48") {
		t.Fatal("versioned OpenCode name rejected")
	}
	for _, value := range []string{"OpenCode Helper", "OpenCode malicious", "Other OpenCode 1.0"} {
		if matchesRegistryDisplayName([]string{"OpenCode"}, value) {
			t.Fatalf("unrelated display name accepted: %q", value)
		}
	}
}

func TestKnownDesktopVersionOnlyOffersNewerPinnedRelease(t *testing.T) {
	for _, test := range []struct {
		known, installed string
		want             bool
	}{
		{"3.19.2", "3.18.9", true}, {"3.19.2", "3.19.2", false}, {"3.19.2", "3.20.0", false}, {"3.19.2", "unknown", false},
	} {
		if got := knownVersionIsNewer(test.known, test.installed); got != test.want {
			t.Errorf("knownVersionIsNewer(%q,%q)=%v", test.known, test.installed, got)
		}
	}
}
