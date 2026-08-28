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
		{
			ID:              "deepseek-harness",
			Name:            "DeepSeek Harness",
			ExecutableNames: platformExecutableNames("dsh"),
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$`,
			),
			MinimumOS: platformMinimumOS(),
		},
		{
			ID:              "qwen-code",
			Name:            "Qwen Code",
			ExecutableNames: platformExecutableNames("qwen"),
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:(?i:qwen(?:[[:space:]-]+code)?)[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$`,
			),
			MinimumOS: platformMinimumOS(),
		},
		{
			ID:              "kimi-code",
			Name:            "Kimi Code",
			ExecutableNames: platformExecutableNames("kimi"),
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$`,
			),
			MinimumOS: platformMinimumOS(),
		},
		{
			ID:              "github-copilot-cli",
			Name:            "GitHub Copilot CLI",
			ExecutableNames: platformExecutableNames("copilot"),
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:(?i:GitHub[[:space:]]+Copilot[[:space:]]+CLI)[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)\.?(?:\r?\nRun 'copilot update' to check for updates\.)?$`,
			),
			MinimumOS: platformMinimumOS(),
		},
		{
			ID:              "gemini-cli",
			Name:            "Gemini CLI",
			ExecutableNames: platformExecutableNames("gemini"),
			VersionArgs:     []string{"--version"},
			VersionPattern: regexp.MustCompile(
				`^(?:(?i:gemini(?:[[:space:]-]+cli)?)[[:space:]]+)?v?([0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$`,
			),
			MinimumOS: platformGeminiMinimumOS(),
		},
	}
}
