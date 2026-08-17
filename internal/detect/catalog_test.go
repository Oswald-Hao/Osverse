//go:build linux

package detect

import (
	"reflect"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/platform"
)

func TestCoreCLISpecsProvidesOrderedFixedCLICommands(t *testing.T) {
	specs := CoreCLISpecs()

	want := []struct {
		id         string
		name       string
		executable string
	}{
		{id: "claude-code", name: "Claude Code", executable: "claude"},
		{id: "codex-cli", name: "Codex CLI", executable: "codex"},
		{id: "opencode-cli", name: "OpenCode CLI", executable: "opencode"},
		{id: "deepseek-harness", name: "DeepSeek Harness", executable: "dsh"},
		{id: "qwen-code", name: "Qwen Code", executable: "qwen"},
		{id: "github-copilot-cli", name: "GitHub Copilot CLI", executable: "copilot"},
	}
	if len(specs) != len(want) {
		t.Fatalf("catalog entries = %d, want %d", len(specs), len(want))
	}

	seenIDs := make(map[string]struct{}, len(specs))
	for index, spec := range specs {
		if spec.ID != want[index].id || spec.Name != want[index].name {
			t.Errorf("spec %d identity = (%q, %q), want (%q, %q)",
				index, spec.ID, spec.Name, want[index].id, want[index].name)
		}
		if spec.ID == "" {
			t.Errorf("spec %d has an empty ID", index)
		}
		if _, exists := seenIDs[spec.ID]; exists {
			t.Errorf("duplicate catalog ID %q", spec.ID)
		}
		seenIDs[spec.ID] = struct{}{}
		if spec.Name == "" {
			t.Errorf("spec %q has an empty name", spec.ID)
		}
		if !reflect.DeepEqual(spec.ExecutableNames, []string{want[index].executable}) {
			t.Errorf("spec %q executables = %#v, want [%q]", spec.ID, spec.ExecutableNames, want[index].executable)
		}
		if !reflect.DeepEqual(spec.VersionArgs, []string{"--version"}) {
			t.Errorf("spec %q version args = %#v, want [\"--version\"]", spec.ID, spec.VersionArgs)
		}
		if spec.MinimumOS == "" {
			t.Errorf("spec %q has an empty minimum OS", spec.ID)
		}
	}
}

func TestCoreCLISpecsVersionPatternsParseUpstreamShapedOutput(t *testing.T) {
	tests := []struct {
		id      string
		output  string
		version string
	}{
		// Claude Code prints its version followed by this product suffix.
		{id: "claude-code", output: "2.1.28 (Claude Code)", version: "2.1.28"},
		{id: "claude-code", output: "claude v2.1.28 (Claude Code)", version: "2.1.28"},
		{id: "claude-code", output: "claude v2.1.28-rc.1+build.7 (Claude Code)", version: "2.1.28-rc.1+build.7"},
		// Codex CLI identifies itself with the codex-cli product name.
		{id: "codex-cli", output: "codex-cli 0.91.0", version: "0.91.0"},
		{id: "codex-cli", output: "codex v0.91.0", version: "0.91.0"},
		{id: "codex-cli", output: "codex-cli 0.91.0-rc.1+build.7", version: "0.91.0-rc.1+build.7"},
		// OpenCode has emitted both a bare version and a product-prefixed version.
		{id: "opencode-cli", output: "1.0.159", version: "1.0.159"},
		{id: "opencode-cli", output: "opencode v1.0.159", version: "1.0.159"},
		{id: "opencode-cli", output: "opencode v1.0.159-rc.1+build.7", version: "1.0.159-rc.1+build.7"},
		// DeepSeek Harness exposes the package version through dsh --version.
		{id: "deepseek-harness", output: "0.1.0-rc.6", version: "0.1.0-rc.6"},
		{id: "qwen-code", output: "0.21.13", version: "0.21.13"},
		{id: "qwen-code", output: "Qwen Code v0.21.13", version: "0.21.13"},
		{id: "github-copilot-cli", output: "GitHub Copilot CLI 1.0.80.\nRun 'copilot update' to check for updates.", version: "1.0.80"},
	}

	specs := specsByID(CoreCLISpecs())
	for _, tt := range tests {
		t.Run(tt.id+"/"+tt.output, func(t *testing.T) {
			spec, ok := specs[tt.id]
			if !ok {
				t.Fatalf("catalog has no %q spec", tt.id)
			}
			if spec.VersionPattern == nil {
				t.Fatal("version pattern is nil")
			}
			pattern := spec.VersionPattern.String()
			if pattern == "" || pattern[0] != '^' || pattern[len(pattern)-1] != '$' {
				t.Fatalf("version pattern = %q, want non-empty anchored expression", pattern)
			}
			matches := spec.VersionPattern.FindStringSubmatch(tt.output)
			if len(matches) < 2 || matches[1] == "" {
				t.Fatalf("pattern %q did not provide a first version capture for %q", pattern, tt.output)
			}
			if matches[1] != tt.version {
				t.Errorf("first capture = %q, want %q", matches[1], tt.version)
			}
			if spec.VersionPattern.MatchString(tt.output + " unrelated trailing junk") {
				t.Errorf("pattern %q accepted unrelated trailing output", pattern)
			}
		})
	}
}

func TestCoreCLISpecsVersionPatternsRequireNonEmptySemVerSuffixIdentifiers(t *testing.T) {
	invalidOutputs := []string{
		"codex 1.2.3-",
		"codex 1.2.3+",
		"codex 1.2.3-rc.1+",
		"codex 1.2.3-rc..1",
		"codex 1.2.3+build..7",
		"codex 1.2.3-rc.1+build 7",
	}
	pattern := specsByID(CoreCLISpecs())["codex-cli"].VersionPattern
	for _, output := range invalidOutputs {
		t.Run(output, func(t *testing.T) {
			if pattern.MatchString(output) {
				t.Errorf("pattern %q accepted malformed version output %q", pattern, output)
			}
		})
	}
}

func TestCoreCLISpecsVersionPatternsParseCRLFTrimmedByDetector(t *testing.T) {
	for _, spec := range CoreCLISpecs() {
		t.Run(spec.ID, func(t *testing.T) {
			version, ok := parseCommandVersion(spec.VersionPattern, platform.CommandResult{
				Stdout: "v1.2.3-rc.1+build.7\r\n",
			})
			if !ok || version != "1.2.3-rc.1+build.7" {
				t.Fatalf("parseCommandVersion() = (%q, %t), want (%q, true)", version, ok, "1.2.3-rc.1+build.7")
			}
		})
	}
}

func specsByID(specs []CommandSpec) map[string]CommandSpec {
	byID := make(map[string]CommandSpec, len(specs))
	for _, spec := range specs {
		byID[spec.ID] = spec
	}
	return byID
}
