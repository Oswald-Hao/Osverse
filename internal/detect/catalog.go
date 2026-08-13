package detect

import "regexp"

const coreCLIMinimumOS = "Ubuntu 20.04"

// CoreCLISpecs returns the fixed Phase-1 catalog in dashboard display order.
func CoreCLISpecs() []CommandSpec {
	return []CommandSpec{
		{
			ID:              "claude-code",
			Name:            "Claude Code",
			ExecutableNames: []string{"claude"},
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:claude(?:[[:space:]-]+code)?[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)(?:[[:space:]]+\(Claude Code\))?$`,
			),
			MinimumOS: coreCLIMinimumOS,
		},
		{
			ID:              "codex-cli",
			Name:            "Codex CLI",
			ExecutableNames: []string{"codex"},
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:codex(?:-cli)?[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$`,
			),
			MinimumOS: coreCLIMinimumOS,
		},
		{
			ID:              "opencode-cli",
			Name:            "OpenCode CLI",
			ExecutableNames: []string{"opencode"},
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:opencode(?:[[:space:]-]+cli)?[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$`,
			),
			MinimumOS: coreCLIMinimumOS,
		},
	}
}
