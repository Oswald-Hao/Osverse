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
	want := []string{`C:\Program Files\OpenCode\OpenCode.exe`, `C:\Program Files\OpenCode\OpenCode Beta.exe`, `C:\Program Files\OpenCode\opencode-desktop.exe`, `D:\Apps\OpenCode\OpenCode.exe`}
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

func TestWindowsOpenCodeDesktopSpecIncludesOfficialBetaChannel(t *testing.T) {
	spec := WindowsDesktopSpecs()[3]
	if !containsFold(spec.ExecutableNames, "OpenCode Beta.exe") ||
		!containsFold(spec.RegistryNames, "OpenCode Beta") {
		t.Fatalf("OpenCode Beta identity missing from spec: %#v", spec)
	}
	want := `AppData\Local\Programs\OpenCode Beta\OpenCode Beta.exe`
	if !containsFold(spec.RelativeExecutables, want) {
		t.Fatalf("OpenCode Beta path missing from spec: %#v", spec.RelativeExecutables)
	}
}

func TestWindowsDesktopPathFallbackExcludesCLICollisions(t *testing.T) {
	tests := []struct {
		id   string
		want []string
	}{
		{id: "claude-desktop", want: nil},
		{id: "codex-desktop", want: nil},
		{id: "opencode-desktop", want: []string{"OpenCode Beta.exe", "opencode-desktop.exe"}},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			var spec WindowsDesktopSpec
			for _, candidate := range WindowsDesktopSpecs() {
				if candidate.ID == test.id {
					spec = candidate
					break
				}
			}
			got := windowsDesktopPathExecutableNames(spec)
			if len(got) != len(test.want) {
				t.Fatalf("PATH fallback names = %#v, want %#v", got, test.want)
			}
			for index := range test.want {
				if got[index] != test.want[index] {
					t.Fatalf("PATH fallback names = %#v, want %#v", got, test.want)
				}
			}
		})
	}
}

func TestDetectWindowsDesktopDoesNotTreatCLIOnPathAsDesktop(t *testing.T) {
	tests := []struct {
		id      string
		cliName string
	}{
		{id: "claude-desktop", cliName: "claude.exe"},
		{id: "codex-desktop", cliName: "codex.exe"},
		{id: "opencode-desktop", cliName: "opencode.exe"},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, test.cliName), []byte("MZ CLI fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			var spec WindowsDesktopSpec
			for _, candidate := range WindowsDesktopSpecs() {
				if candidate.ID == test.id {
					spec = candidate
					break
				}
			}
			component, err := DetectWindowsDesktop(context.Background(), spec, domain.SystemInfo{Supported: true},
				[]string{directory}, fakeWindowsPackageQuery{}, t.TempDir())
			if err != nil || component.Status != domain.StatusMissing || len(component.Installations) != 0 {
				t.Fatalf("CLI PATH collision = (%#v, %v), want missing desktop", component, err)
			}
		})
	}
}

func TestDetectWindowsOpenCodeBetaAtOfficialPath(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(home, "AppData", "Local", "Programs", "OpenCode Beta", "OpenCode Beta.exe")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("MZ OpenCode Beta fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	component, err := DetectWindowsDesktop(context.Background(), WindowsDesktopSpecs()[3],
		domain.SystemInfo{Supported: true}, nil, fakeWindowsPackageQuery{}, home)
	if err != nil || component.Status != domain.StatusInstalled || len(component.Installations) != 1 || component.Installations[0].Path != executable {
		t.Fatalf("OpenCode Beta detection = (%#v, %v)", component, err)
	}
}

func TestAppModelPackageIdentityRequiresExactBoundary(t *testing.T) {
	tests := []struct {
		name      string
		prefixes  []string
		candidate string
		want      bool
	}{
		{name: "Codex exact", prefixes: []string{"OpenAI.Codex"}, candidate: "OpenAI.Codex_26.721.3996.0_x64__8wekyb3d8bbwe", want: true},
		{name: "Codex case insensitive", prefixes: []string{"OpenAI.Codex"}, candidate: "openai.codex_26.721.3996.0_x64__8wekyb3d8bbwe", want: true},
		{name: "Claude configured boundary", prefixes: []string{"Claude_"}, candidate: "Claude_1.2.3.4_x64__publisher", want: true},
		{name: "sibling preview", prefixes: []string{"OpenAI.Codex"}, candidate: "OpenAI.CodexPreview_26.721.3996.0_x64__publisher", want: false},
		{name: "identity without version boundary", prefixes: []string{"OpenAI.Codex"}, candidate: "OpenAI.Codex", want: false},
		{name: "hyphenated sibling", prefixes: []string{"OpenAI.Codex"}, candidate: "OpenAI.Codex-Beta_26.721.3996.0_x64__publisher", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesAppModelPackageName(test.prefixes, test.candidate); got != test.want {
				t.Fatalf("matchesAppModelPackageName(%#v, %q) = %t, want %t", test.prefixes, test.candidate, got, test.want)
			}
		})
	}
}

func TestAppModelEvidenceSelectsNewestVersionIndependentlyOfEnumerationOrder(t *testing.T) {
	spec := WindowsDesktopSpecs()[2]
	names := []string{
		"OpenAI.Codex_26.721.9.0_x64__publisher",
		"OpenAI.CodexPreview_99.0.0.0_x64__publisher",
		"OpenAI.Codex_26.721.3996.0_x64__publisher",
		"OpenAI.Codex_26.800.1.0_x64__publisher",
	}
	first, ok := appModelEvidenceFromNames(spec, names)
	if !ok || !first.Installed || first.Source != "msix" || first.Version != "26.800.1.0" {
		t.Fatalf("AppModel evidence = (%#v, %t)", first, ok)
	}
	for left, right := 0, len(names)-1; left < right; left, right = left+1, right-1 {
		names[left], names[right] = names[right], names[left]
	}
	second, ok := appModelEvidenceFromNames(spec, names)
	if !ok || second.Installed != first.Installed || second.Version != first.Version || second.Source != first.Source || len(second.ExecutablePaths) != 0 {
		t.Fatalf("reversed AppModel evidence = (%#v, %t), want %#v", second, ok, first)
	}
}

func TestAppModelEvidenceUsesStableUnknownFallback(t *testing.T) {
	spec := WindowsDesktopSpecs()[2]
	evidence, ok := appModelEvidenceFromNames(spec, []string{
		"OpenAI.Codex_preview_x64__publisher",
		"OpenAI.Codex_unversioned_x64__publisher",
	})
	if !ok || !evidence.Installed || evidence.Version != "unknown" || evidence.Source != "msix" {
		t.Fatalf("unknown AppModel evidence = (%#v, %t)", evidence, ok)
	}
}
