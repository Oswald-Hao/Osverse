package detect

import "regexp"

// CoreCLISpecs returns the fixed Phase-1 catalog in dashboard display order.
func CoreCLISpecs() []CommandSpec {
	return []CommandSpec{
		{
			ID:              "claude-code",
			Name:            "Claude Code",
			ExecutableNames: platformExecutableNames("claude"),
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:claude(?:[[:space:]-]+code)?[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)(?:[[:space:]]+\(Claude Code\))?$`,
			),
			MinimumOS: platformMinimumOS(),
		},
		{
			ID:              "codex-cli",
			Name:            "Codex CLI",
			ExecutableNames: platformExecutableNames("codex"),
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:codex(?:-cli)?[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$`,
			),
			MinimumOS: platformMinimumOS(),
		},
		{
			ID:              "opencode-cli",
			Name:            "OpenCode CLI",
			ExecutableNames: platformExecutableNames("opencode"),
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:opencode(?:[[:space:]-]+cli)?[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$`,
			),
			MinimumOS: platformMinimumOS(),
		},
	}
}
